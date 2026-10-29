package condition

import (
	"fmt"
	"testing"
)

type benchItem struct {
	class   int64
	quality int64
	count   int64
	tid     int64
}

// benchState 是数据层实现：q.Index 有精确候选时只遍历对应桶；
// fullScan 打开后忽略 Index，用于与不支持索引的接入方式做同口径对比。
type benchState struct {
	items    []*benchItem
	byTid    map[int64][]*benchItem
	fullScan bool
}

func newBenchState(n int) *benchState {
	s := &benchState{
		items: make([]*benchItem, 0, n),
		byTid: make(map[int64][]*benchItem, n),
	}
	for i := 0; i < n; i++ {
		it := &benchItem{class: int64(i % 5), quality: int64(i % 6), count: 1, tid: int64(i)}
		s.items = append(s.items, it)
		s.byTid[it.tid] = append(s.byTid[it.tid], it)
	}
	return s
}

func (s *benchState) get(it *benchItem, a Attr) (int64, bool) {
	switch a {
	case attrClass:
		return it.class, true
	case attrQuality:
		return it.quality, true
	case attrCount:
		return it.count, true
	case attrTid:
		return it.tid, true
	}
	return 0, false
}

func (s *benchState) Eval(q Query) (int64, bool) {
	list := s.items
	if !s.fullScan {
		if vals := q.Index[attrTid]; len(vals) == 1 {
			if bucket, ok := s.byTid[vals[0]]; ok {
				list = bucket
			} else {
				list = nil
			}
		}
	}
	var sum, cnt, best int64
	has := false
	for _, it := range list {
		if !q.Filter.Match(func(a Attr) (int64, bool) { return s.get(it, a) }) {
			continue
		}
		if q.Agg == AccCount {
			cnt++
			has = true
			continue
		}
		v, _ := s.get(it, q.Attr)
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
		default:
			if !has {
				best = v
			}
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

func benchExpr(b *testing.B, tid int64) Expr {
	b.Helper()
	e, err := Parse(fmt.Sprintf("Item(item_tid=%d, sum(item_count)) >= 1", tid), testCfg)
	if err != nil {
		b.Fatal(err)
	}
	return e
}

func newBenchManager(b *testing.B, n int) (*Manager, *benchState, *Plans) {
	b.Helper()
	state := newBenchState(n)
	counter := newMemCounter()
	plans := NewPlans()
	m, err := NewManager(counter, state, plans)
	if err != nil {
		b.Fatal(err)
	}
	return m, state, plans
}

// 1000 个注册共享同一表达式：一次命中只读一次状态。
func BenchmarkSameExpr_1000Regs_50Trigger_1Hit(b *testing.B) {
	m, state, plans := newBenchManager(b, 1000)
	e := benchExpr(b, 0)
	if err := plans.Add(e); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if err := m.Register(InstanceKey(i+1), e, nil); err != nil {
			b.Fatal(err)
		}
	}
	unrelated := NewChangeSet()
	unrelated.Mark(ExactKey(domainItem, attrTid, 999999))
	related := NewChangeSet()
	related.Mark(ExactKey(domainItem, attrTid, 0))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 49; j++ {
			m.Trigger(unrelated)
		}
		state.items[0].count++
		m.Trigger(related)
	}
}

// 同模板 1000 实例且全部回调：每次回调独立分配进度切片。
func BenchmarkSameExpr_1000Regs_Notify(b *testing.B) {
	m, state, plans := newBenchManager(b, 1000)
	e := benchExpr(b, 0)
	if err := plans.Add(e); err != nil {
		b.Fatal(err)
	}
	h := func(Result) bool { return false }
	for i := 0; i < 1000; i++ {
		if err := m.Register(InstanceKey(i+1), e, h); err != nil {
			b.Fatal(err)
		}
	}
	unrelated := NewChangeSet()
	unrelated.Mark(ExactKey(domainItem, attrTid, 999999))
	related := NewChangeSet()
	related.Mark(ExactKey(domainItem, attrTid, 0))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 49; j++ {
			m.Trigger(unrelated)
		}
		state.items[0].count++
		m.Trigger(related)
	}
}

// 同模板 1000 实例 + 全量扫描数据层：与不支持索引的接入方式同口径。
func BenchmarkSameExpr_1000Regs_FullScan(b *testing.B) {
	state := newBenchState(1000)
	state.fullScan = true
	counter := newMemCounter()
	plans := NewPlans()
	m, err := NewManager(counter, state, plans)
	if err != nil {
		b.Fatal(err)
	}
	e := benchExpr(b, 0)
	if err := plans.Add(e); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if err := m.Register(InstanceKey(i+1), e, nil); err != nil {
			b.Fatal(err)
		}
	}
	unrelated := NewChangeSet()
	unrelated.Mark(ExactKey(domainItem, attrTid, 999999))
	related := NewChangeSet()
	related.Mark(ExactKey(domainItem, attrTid, 0))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 49; j++ {
			m.Trigger(unrelated)
		}
		state.items[0].count++
		m.Trigger(related)
	}
}

// 1000 个不同表达式 + 全量扫描数据层：同口径对比。
func BenchmarkDistinctExpr_1000Regs_FullScan(b *testing.B) {
	state := newBenchState(1000)
	state.fullScan = true
	counter := newMemCounter()
	plans := NewPlans()
	m, err := NewManager(counter, state, plans)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		e := benchExpr(b, int64(i))
		if err := plans.Add(e); err != nil {
			b.Fatal(err)
		}
		if err := m.Register(InstanceKey(i+1), e, nil); err != nil {
			b.Fatal(err)
		}
	}
	unrelated := NewChangeSet()
	unrelated.Mark(ExactKey(domainItem, attrTid, 999999))
	related := NewChangeSet()
	related.Mark(ExactKey(domainItem, attrTid, 0))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 49; j++ {
			m.Trigger(unrelated)
		}
		state.items[0].count++
		m.Trigger(related)
	}
}

// 1000 个注册各用不同表达式：一次命中只有一个计划被求值。
func BenchmarkDistinctExpr_1000Regs_50Trigger_1Hit(b *testing.B) {
	m, state, plans := newBenchManager(b, 1000)
	for i := 0; i < 1000; i++ {
		e := benchExpr(b, int64(i))
		if err := plans.Add(e); err != nil {
			b.Fatal(err)
		}
		if err := m.Register(InstanceKey(i+1), e, nil); err != nil {
			b.Fatal(err)
		}
	}
	unrelated := NewChangeSet()
	unrelated.Mark(ExactKey(domainItem, attrTid, 999999))
	related := NewChangeSet()
	related.Mark(ExactKey(domainItem, attrTid, 0))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 49; j++ {
			m.Trigger(unrelated)
		}
		state.items[0].count++
		m.Trigger(related)
	}
}
