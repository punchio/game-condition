package condition

import (
	"fmt"
	"math"
	"math/big"
)

// readerKind 区分常驻状态与事件累计。
type readerKind uint8

const (
	readerState readerKind = iota + 1
	readerEvent
)

// dataReader 是叶的取值器。
// 状态叶保存预计算的 Query（含精确候选），事件叶保存事件 id、过滤与 own。
type dataReader struct {
	kind readerKind
	flt  Filter
	attr Attr
	agg  Acc
	id   int64
	own  exprNode
	deps []ChangeKey
	fp   string // 状态叶的查询指纹，用于 Manager 内共享求值
	q    Query
}

// newStateReader 构造状态型取值器。
// 聚合需要属性时，额外声明该属性的通配依赖（实体实例可能被增删/替换）。
func newStateReader(src Source, flt Filter, attr Attr, agg Acc) dataReader {
	if !agg.Valid() {
		agg = AccCount
	}
	keys := flt.keys()
	if agg.needsAttr() {
		keys = append(keys, WildcardKey(src, attr))
	}
	q := Query{Source: src, Filter: flt, Agg: agg, Attr: attr, Index: flt.exactIndex()}
	fp := fmt.Sprintf("%d|%s|%d|%d", src, flt.text(), agg, attr)
	return dataReader{
		kind: readerState,
		flt:  flt,
		attr: attr,
		agg:  agg,
		deps: dedupeKeys(keys),
		fp:   fp,
		q:    q,
	}
}

// newEventReader 构造事件型取值器。
func newEventReader(id int64, flt Filter, attr Attr, own exprNode) dataReader {
	keys := append([]ChangeKey{ExactKey(SourceEvent, eventAttrID, id)}, flt.keys()...)
	return dataReader{
		kind: readerEvent,
		id:   id,
		flt:  flt,
		attr: attr,
		own:  own,
		deps: dedupeKeys(keys),
	}
}

func (r dataReader) keys() []ChangeKey { return r.deps }

// mayMatch 判断事件元数据是否足以排除该叶；不能确定时放行。
func (r dataReader) mayMatch(event int64, cs *ChangeSet) bool {
	return r.kind == readerEvent && r.id == event && r.flt.mayMatchEvent(cs)
}

// delta 计算本次事件带给该叶的增量/样本。
// 只依赖事件记录与过滤器，与实例、实时状态无关，因此每个计划每轮只算一次。
func (r dataReader) delta(acc Acc, records []eventRecord) (int64, bool) {
	if acc == AccCount && r.flt.empty() {
		return 1, true
	}
	value, has := aggregateEvent(records, r.flt, acc, r.attr)
	if !has {
		return 0, false
	}
	if acc == AccCount {
		return 1, true // 事件计次：一次事件最多 +1，不按记录数计
	}
	return value, true
}

// allow 按实例检查 own 前置条件，状态查询使用轮内共享缓存。
func allow[InstanceKey comparable](r dataReader, ctx *evalContext[InstanceKey]) bool {
	return r.own == nil || evalOwn(ctx, r.own)
}

// aggregateEvent 逐条记录过滤后聚合，绝不跨记录组合过滤属性。
func aggregateEvent(records []eventRecord, flt Filter, agg Acc, attr Attr) (int64, bool) {
	var sum, cnt, best int64
	var has bool
	for _, rec := range records {
		if !flt.Match(rec.get) {
			continue
		}
		if agg == AccCount {
			cnt++
			has = true
			continue
		}
		v, ok := rec.get(attr)
		if !ok {
			continue
		}
		switch agg {
		case AccSum:
			sum += v
			cnt++
		case AccAvg:
			if (v > 0 && sum > math.MaxInt64-v) || (v < 0 && sum < math.MinInt64-v) {
				return aggregateEventAvgBig(records, flt, attr)
			}
			sum += v
			cnt++
		case AccMax:
			if !has || v > best {
				best = v
			}
		case AccMin:
			if !has || v < best {
				best = v
			}
		case AccFirst:
			if !has {
				best = v
			}
		case AccValue:
			// 当前值：同一事件内取最后一条匹配记录，跨事件由 Counter 覆盖。
			best = v
		}
		has = true
	}
	switch agg {
	case AccCount:
		return cnt, has
	case AccAvg:
		if cnt == 0 {
			return 0, false
		}
		return sum / cnt, true
	case AccSum:
		return sum, has
	}
	return best, has
}

// aggregateEventAvgBig 只在 int64 求和溢出时调用；Quo 保持向零取整。
func aggregateEventAvgBig(records []eventRecord, flt Filter, attr Attr) (int64, bool) {
	var sum, value big.Int
	var cnt int64
	for _, rec := range records {
		if !flt.Match(rec.get) {
			continue
		}
		v, ok := rec.get(attr)
		if !ok {
			continue
		}
		value.SetInt64(v)
		sum.Add(&sum, &value)
		cnt++
	}
	if cnt == 0 {
		return 0, false
	}
	var avg, divisor big.Int
	divisor.SetInt64(cnt)
	return avg.Quo(&sum, &divisor).Int64(), true
}

// evalOwn 按实例检查前置条件，状态查询使用轮内共享缓存。
func evalOwn[InstanceKey comparable](ctx *evalContext[InstanceKey], e exprNode) bool {
	switch n := e.(type) {
	case *constExpr:
		return n.val
	case *logicExpr:
		switch n.op {
		case logicNot:
			if len(n.children) != 1 {
				return false
			}
			return !evalOwn(ctx, n.children[0])
		case logicOr:
			for _, kid := range n.children {
				if evalOwn(ctx, kid) {
					return true
				}
			}
			return false
		}
		for _, kid := range n.children {
			if !evalOwn(ctx, kid) {
				return false
			}
		}
		return true
	case *leafExpr:
		value, has := readSourceState(ctx.plan.ownSlots[n], ctx.state)
		return predicate(n.cmp, n.values, value, has)
	}
	return false
}

// dedupeKeys 就地线性去重：依赖点通常只有 1~3 个。
func dedupeKeys(keys []ChangeKey) []ChangeKey {
	out := keys[:0]
	for _, k := range keys {
		dup := false
		for _, seen := range out {
			if seen == k {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, k)
		}
	}
	return out
}
