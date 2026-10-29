package condition

import (
	"fmt"
	"testing"
)

func newEventBenchManager(b *testing.B) (*Manager[uint64], *memCounter[uint64], *Plans) {
	b.Helper()
	counter := newMemCounter[uint64]()
	plans := NewPlans()
	m, err := NewManager(counter, nil, plans)
	if err != nil {
		b.Fatalf("NewManager: %v", err)
	}
	return m, counter, plans
}

func benchEventExpr(b *testing.B, id, class int64) Expr {
	b.Helper()
	text := fmt.Sprintf("Event(event_id=%d & item_class=%d, sum(item_count)) >= 1", id, class)
	e, err := Parse(text, testCfg)
	if err != nil {
		b.Fatalf("Parse(%q): %v", text, err)
	}
	return e
}

func benchEventChangeSet(b *testing.B, id, class int64) *ChangeSet {
	b.Helper()
	cs := NewChangeSet()
	if err := cs.Event(id); err != nil {
		b.Fatalf("Event: %v", err)
	}
	if err := cs.EventValue(map[Attr]int64{attrClass: class, attrCount: 1}); err != nil {
		b.Fatalf("EventValue: %v", err)
	}
	return cs
}

// warmUpEventBench 预分配 steady-state 所需缓冲，并确认基准事件确实命中。
func warmUpEventBench(b *testing.B, m *Manager[uint64], counter *memCounter[uint64], cs *ChangeSet, key uint64) {
	b.Helper()
	m.Trigger(cs)
	if value, has := counter.Read(key, 0, AccSum); !has || value != 1 {
		b.Fatalf("事件预热未命中: key=%d value=%d has=%v", key, value, has)
	}
}

// 单实例、单记录：每次迭代只有一次真实事件 Trigger。
func BenchmarkEventSingleTrigger(b *testing.B) {
	m, counter, plans := newEventBenchManager(b)
	e := benchEventExpr(b, 1001, 10)
	if err := plans.Add(e); err != nil {
		b.Fatal(err)
	}
	if err := m.Register(1, e, nil); err != nil {
		b.Fatal(err)
	}
	cs := benchEventChangeSet(b, 1001, 10)
	warmUpEventBench(b, m, counter, cs, 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Trigger(cs)
	}
}

// 同模板 1000 实例共享一次事件增量，每次迭代只有一次 Trigger。
func BenchmarkEventSameExpr_1000Regs_SingleTrigger(b *testing.B) {
	m, counter, plans := newEventBenchManager(b)
	e := benchEventExpr(b, 1001, 10)
	if err := plans.Add(e); err != nil {
		b.Fatal(err)
	}
	for i := 1; i <= 1000; i++ {
		if err := m.Register(uint64(i), e, nil); err != nil {
			b.Fatal(err)
		}
	}
	cs := benchEventChangeSet(b, 1001, 10)
	warmUpEventBench(b, m, counter, cs, 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Trigger(cs)
	}
}

// 1000 个不同事件计划，每次迭代只命中一个计划并 Trigger 一次。
func BenchmarkEventDistinctExpr_1000Regs_SingleTrigger_1Hit(b *testing.B) {
	m, counter, plans := newEventBenchManager(b)
	for i := 1; i <= 1000; i++ {
		e := benchEventExpr(b, int64(i), int64(i))
		if err := plans.Add(e); err != nil {
			b.Fatal(err)
		}
		if err := m.Register(uint64(i), e, nil); err != nil {
			b.Fatal(err)
		}
	}
	cs := benchEventChangeSet(b, 1000, 1000)
	warmUpEventBench(b, m, counter, cs, 1000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Trigger(cs)
	}
}
