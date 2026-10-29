package condition

import (
	"strconv"
	"strings"
)

// tri 是记录过滤的三值逻辑结果：属性缺失时原子谓词为 unknown。
type tri uint8

const (
	triUnknown tri = iota
	triFalse
	triTrue
)

// filterExpr 是记录过滤的表达式节点，只能由 Parse 产出。
type filterExpr interface{ filterNode() }

type predExpr struct {
	attr   Attr
	cmp    cmpOp
	values []int64
}

func (*predExpr) filterNode() {}

type andExpr struct{ kids []filterExpr }

func (*andExpr) filterNode() {}

type orExpr struct{ kids []filterExpr }

func (*orExpr) filterNode() {}

type notExpr struct{ kid filterExpr }

func (*notExpr) filterNode() {}

// evalFilter 按具体类型分派求值：用普通函数而不是接口方法，
// 让 get 这个函数值不逃逸，避免每次求值多一次分配。
func evalFilter(n filterExpr, get func(Attr) (int64, bool)) tri {
	switch v := n.(type) {
	case *predExpr:
		value, ok := get(v.attr)
		if !ok {
			return triUnknown
		}
		if predicate(v.cmp, v.values, value, true) {
			return triTrue
		}
		return triFalse
	case *andExpr:
		res := triTrue
		for _, kid := range v.kids {
			switch evalFilter(kid, get) {
			case triFalse:
				return triFalse
			case triUnknown:
				res = triUnknown
			}
		}
		return res
	case *orExpr:
		res := triFalse
		for _, kid := range v.kids {
			switch evalFilter(kid, get) {
			case triTrue:
				return triTrue
			case triUnknown:
				res = triUnknown
			}
		}
		return res
	case *notExpr:
		switch evalFilter(v.kid, get) {
		case triTrue:
			return triFalse
		case triFalse:
			return triTrue
		}
		return triUnknown
	}
	return triUnknown
}

// Filter 描述一个记录集：数据域 + 布尔筛选条件。
// 谓词可用 !、&、| 与括号任意组合；一条记录独立求值。
type Filter struct {
	src  Source
	root filterExpr // nil 表示恒真
}

func newFilter(src Source, root filterExpr) Filter { return Filter{src: src, root: root} }

// Match 判断一条记录是否命中：只有 true 算命中，unknown 不算。
func (f Filter) Match(get func(Attr) (int64, bool)) bool {
	if f.root == nil {
		return true
	}
	return evalFilter(f.root, get) == triTrue
}

func (f Filter) empty() bool { return f.root == nil }

// text 返回稳定文本，用于 source 指纹；同构过滤条件必须得到同一文本。
func (f Filter) text() string {
	if f.root == nil {
		return ""
	}
	var b strings.Builder
	writeFilterText(&b, f.root)
	return b.String()
}

func writeFilterText(b *strings.Builder, n filterExpr) {
	switch v := n.(type) {
	case *predExpr:
		b.WriteByte('#')
		b.WriteString(strconv.Itoa(int(v.attr)))
		b.WriteString(cmpText(v.cmp, v.values))
	case *andExpr:
		b.WriteString("(A")
		for _, kid := range v.kids {
			writeFilterText(b, kid)
			b.WriteByte('&')
		}
		b.WriteByte(')')
	case *orExpr:
		b.WriteString("(O")
		for _, kid := range v.kids {
			writeFilterText(b, kid)
			b.WriteByte('|')
		}
		b.WriteByte(')')
	case *notExpr:
		b.WriteString("(N")
		writeFilterText(b, v.kid)
		b.WriteByte(')')
	}
}

// keys 由过滤条件推导依赖点，只求“不漏触发”：
//   - 纯 AND 且同一属性全是等值：该属性出精确点；
//   - 含 OR/NOT，或属性上有非等值：该属性出通配点。
func (f Filter) keys() []ChangeKey {
	if f.root == nil {
		return nil
	}
	var preds []*predExpr
	pure := collectPreds(f.root, &preds)
	out := make([]ChangeKey, 0, len(preds))
	var seen []Attr
	for _, p := range preds {
		if hasAttr(seen, p.attr) {
			continue
		}
		seen = append(seen, p.attr)
		if !pure || !allEqual(preds, p.attr) {
			out = append(out, WildcardKey(f.src, p.attr))
			continue
		}
		for _, q := range preds {
			if q.attr != p.attr {
				continue
			}
			for _, v := range q.values {
				out = append(out, ExactKey(f.src, p.attr, v))
			}
		}
	}
	return out
}

// exactIndex 返回纯 AND 等值谓词推导出的精确候选：属性 → 候选值。
// 含 OR/NOT 或非等值时返回空（业务退回全量遍历），不会漏选。
func (f Filter) exactIndex() map[Attr][]int64 {
	keys := f.keys()
	var idx map[Attr][]int64
	for _, k := range keys {
		if k.wildcard {
			continue
		}
		if idx == nil {
			idx = make(map[Attr][]int64, 2)
		}
		idx[k.attr] = append(idx[k.attr], k.id)
	}
	return idx
}

func collectPreds(n filterExpr, out *[]*predExpr) bool {
	switch v := n.(type) {
	case *predExpr:
		*out = append(*out, v)
		return true
	case *andExpr:
		pure := true
		for _, kid := range v.kids {
			if !collectPreds(kid, out) {
				pure = false
			}
		}
		return pure
	case *orExpr:
		for _, kid := range v.kids {
			collectPreds(kid, out)
		}
	case *notExpr:
		collectPreds(v.kid, out)
	}
	return false
}

func allEqual(preds []*predExpr, attr Attr) bool {
	for _, p := range preds {
		if p.attr == attr && p.cmp != cmpEqual {
			return false
		}
	}
	return true
}

func hasAttr(list []Attr, a Attr) bool {
	for _, v := range list {
		if v == a {
			return true
		}
	}
	return false
}

// mayMatchEvent 只用纯 AND 中的等值谓词排除明确不匹配的事件；
// 其余情况保守放行，最终仍由实时过滤决定。
func (f Filter) mayMatchEvent(cs *ChangeSet) bool {
	return f.root == nil || cs == nil || mayMatchEventNode(f.root, cs)
}

func mayMatchEventNode(n filterExpr, cs *ChangeSet) bool {
	switch v := n.(type) {
	case *predExpr:
		if v.cmp != cmpEqual {
			return true
		}
		hasExact := false
		for _, k := range cs.keys {
			if k.src != SourceEvent || k.attr != v.attr {
				continue
			}
			if k.wildcard {
				return true
			}
			hasExact = true
			for _, value := range v.values {
				if k.id == value {
					return true
				}
			}
		}
		return !hasExact
	case *andExpr:
		for _, kid := range v.kids {
			if !mayMatchEventNode(kid, cs) {
				return false
			}
		}
	}
	return true
}
