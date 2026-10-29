package condition

import "testing"

const (
	attrClass   Attr   = 1
	attrQuality Attr   = 2
	attrCount   Attr   = 3
	attrTid     Attr   = 4
	domainItem  Source = 2
)

var testCfg = &ParseConfig{
	StateSystems: map[string]Source{"Item": domainItem},
	Attrs: map[string]Attr{
		"item_class":   attrClass,
		"item_quality": attrQuality,
		"item_count":   attrCount,
		"item_tid":     attrTid,
	},
}

type rec map[Attr]int64

func (r rec) get(a Attr) (int64, bool) {
	v, ok := r[a]
	return v, ok
}

func item(class, quality, count, tid int64) rec {
	return rec{attrClass: class, attrQuality: quality, attrCount: count, attrTid: tid}
}

// bag 是 StateSource 的测试实现：全量扫描 + 过滤 + 聚合。
type bag struct {
	items     []rec
	evals     int
	lastQuery Query
}

func (b *bag) Eval(q Query) (int64, bool) {
	b.evals++
	b.lastQuery = q
	var sum, cnt, best int64
	has := false
	for _, r := range b.items {
		if !q.Filter.Match(r.get) {
			continue
		}
		if q.Agg == AccCount {
			cnt++
			has = true
			continue
		}
		v, ok := r[q.Attr]
		if !ok {
			continue
		}
		switch q.Agg {
		case AccSum, AccAvg:
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
			best = v // 当前值取最新一条匹配记录
		}
		has = true
	}
	switch q.Agg {
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

type cell struct {
	acc Acc
	sum int64
	n   int64
	val int64
	has bool
}

// memCounter 是 Counter 的测试实现。
type memCounter struct {
	m       map[InstanceKey]map[uint16]*cell
	updates int
	dels    int
}

func newMemCounter() *memCounter {
	return &memCounter{m: make(map[InstanceKey]map[uint16]*cell)}
}

func (c *memCounter) Update(k InstanceKey, index uint16, acc Acc, val int64) {
	c.updates++
	slots := c.m[k]
	if slots == nil {
		slots = make(map[uint16]*cell, 2)
		c.m[k] = slots
	}
	cl := slots[index]
	if cl == nil {
		cl = &cell{acc: acc}
		slots[index] = cl
	}
	if cl.has && cl.acc != acc {
		*cl = cell{acc: acc}
	}
	switch acc {
	case AccCount:
		cl.val++
	case AccSum:
		cl.val += val
	case AccAvg:
		cl.sum += val
		cl.n++
		cl.val = cl.sum / cl.n
	case AccMax:
		if !cl.has || val > cl.val {
			cl.val = val
		}
	case AccMin:
		if !cl.has || val < cl.val {
			cl.val = val
		}
	case AccFirst:
		if !cl.has {
			cl.val = val
		}
	case AccValue:
		cl.val = val
	}
	cl.has = true
}

func (c *memCounter) Read(k InstanceKey, index uint16, acc Acc) (int64, bool) {
	slots := c.m[k]
	if slots == nil {
		return 0, false
	}
	cl := slots[index]
	if cl == nil || !cl.has || cl.acc != acc {
		return 0, false
	}
	return cl.val, true
}

func (c *memCounter) Del(k InstanceKey) {
	c.dels++
	delete(c.m, k)
}

func mustParse(t *testing.T, text string) Expr {
	t.Helper()
	e, err := Parse(text, testCfg)
	if err != nil {
		t.Fatalf("解析 %q 失败: %v", text, err)
	}
	return e
}

func newTestManager(t *testing.T, state StateSource) (*Manager, *memCounter, *Plans) {
	t.Helper()
	c := newMemCounter()
	plans := NewPlans()
	m, err := NewManager(c, state, plans)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m, c, plans
}

func register(t *testing.T, m *Manager, plans *Plans, key InstanceKey, e Expr, h Handler) {
	t.Helper()
	if err := plans.Add(e); err != nil {
		t.Fatalf("Plans.Add: %v", err)
	}
	if err := m.Register(key, e, h); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

// recorder 保存每次回调结果的副本。
type recorder struct {
	results []Result
}

func (r *recorder) handler(res Result) bool {
	r.results = append(r.results, res.Clone())
	return false
}

func (r *recorder) last() Result { return r.results[len(r.results)-1] }
