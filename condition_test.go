package condition

import (
	"strings"
	"testing"
)

func TestStateLeafEvaluation(t *testing.T) {
	b := &bag{items: []rec{item(2, 5, 5, 1)}}
	m, _, plans := newTestManager(t, b)
	r := &recorder{}
	e := mustParse(t, "Item(item_class=2, sum(item_count)) >= 10")
	register(t, m, plans, 1, e, r.handler)

	last := r.last()
	if last.Match || !last.Progress[0].Has || last.Progress[0].Value != 5 {
		t.Fatalf("初始结果 = %+v", last)
	}

	b.items = append(b.items, item(2, 4, 5, 2))
	cs := NewChangeSet()
	cs.Mark(ExactKey(domainItem, attrClass, 2))
	m.Trigger(cs)

	last = r.last()
	if !last.Match || last.Progress[0].Value != 10 {
		t.Fatalf("加道具后结果 = %+v", last)
	}

	b.items = nil
	cs.Reset()
	cs.Mark(ExactKey(domainItem, attrClass, 2))
	m.Trigger(cs)
	last = r.last()
	if last.Match || last.Progress[0].Value != 0 {
		t.Fatalf("清空后结果 = %+v", last)
	}
}

func TestFilterThreeValue(t *testing.T) {
	b := &bag{items: []rec{{attrClass: 2}}} // 没有 quality 属性
	m, _, plans := newTestManager(t, b)
	r1, r2 := &recorder{}, &recorder{}

	register(t, m, plans, 1, mustParse(t, "Item(item_quality != 3) >= 1"), r1.handler)
	register(t, m, plans, 2, mustParse(t, "Item(!(item_quality = 3)) >= 1"), r2.handler)

	if r1.last().Match || r2.last().Match {
		t.Fatalf("属性缺失应为 unknown，不等于命中: %+v %+v", r1.last(), r2.last())
	}

	b.items = []rec{item(2, 4, 1, 1)}
	cs := NewChangeSet()
	cs.Mark(ExactKey(domainItem, attrQuality, 4))
	m.Trigger(cs)

	if !r1.last().Match || !r2.last().Match {
		t.Fatalf("属性存在后应命中: %+v %+v", r1.last(), r2.last())
	}
}

func TestFilterLogic(t *testing.T) {
	b := &bag{items: []rec{
		item(1, 5, 5, 1),
		item(2, 1, 10, 2),
		item(3, 5, 100, 3),
	}}
	m, _, plans := newTestManager(t, b)
	r := &recorder{}
	e := mustParse(t, "Item((item_class=1 | item_class=2) & !(item_quality<3), sum(item_count)) >= 5")
	register(t, m, plans, 1, e, r.handler)

	if !r.last().Match || r.last().Progress[0].Value != 5 {
		t.Fatalf("(class=1|class=2) & !(quality<3) 结果 = %+v", r.last())
	}
}

func TestPredicateForms(t *testing.T) {
	b := &bag{items: []rec{
		item(1, 2, 3, 1),
		item(2, 3, 1, 2),
		item(0, 9, 1, 3),
	}}
	m, _, plans := newTestManager(t, b)
	r := &recorder{}

	register(t, m, plans, 1, mustParse(t, "Item(item_class=1,2) >= 2"), r.handler)
	if r.last().Progress[0].Value != 2 {
		t.Fatalf("多值等值 = %+v", r.last())
	}
	register(t, m, plans, 2, mustParse(t, "Item(item_quality 2-3) >= 2"), r.handler)
	if r.last().Progress[0].Value != 2 {
		t.Fatalf("闭区间 = %+v", r.last())
	}
	register(t, m, plans, 3, mustParse(t, "Item(item_class) >= 2"), r.handler)
	if r.last().Progress[0].Value != 2 {
		t.Fatalf("裸属性应等价 class != 0: %+v", r.last())
	}
}

func TestEventRecordsAndAggregations(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want int64
	}{
		{"sum", "Event(event_id=1001 & item_class=10, sum(item_count)) >= 6", 6},
		{"count", "Event(event_id=1001 & item_class=10, count()) >= 1", 1},
		{"max", "Event(event_id=1001 & item_class=10, max(item_quality)) >= 5", 5},
		{"min", "Event(event_id=1001 & item_class=10, min(item_quality)) >= 3", 3},
		{"avg", "Event(event_id=1001 & item_class=10, avg(item_count)) >= 3", 3},
		{"first", "Event(event_id=1001 & item_class=10, first(item_count)) >= 2", 2},
		{"value", "Event(event_id=1001 & item_class=10, value(item_count)) >= 4", 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := &bag{}
			m, c, plans := newTestManager(t, b)
			r := &recorder{}
			register(t, m, plans, 1, mustParse(t, tc.expr), r.handler)

			cs := NewChangeSet()
			if err := cs.Event(1001); err != nil {
				t.Fatal(err)
			}
			if err := cs.EventValue(map[Attr]int64{attrClass: 10, attrQuality: 3, attrCount: 2}); err != nil {
				t.Fatal(err)
			}
			if err := cs.EventValue(map[Attr]int64{attrClass: 10, attrQuality: 5, attrCount: 4}); err != nil {
				t.Fatal(err)
			}
			m.Trigger(cs)

			if got := r.last().Progress[0].Value; got != tc.want {
				t.Fatalf("%s 进度 = %d, 期望 %d", tc.name, got, tc.want)
			}
			if !r.last().Match {
				t.Fatalf("%s 未命中: %+v", tc.name, r.last())
			}
			if acc := c.m[1][0].acc; acc != accOf(tc.name) {
				t.Fatalf("%s 计数方式 = %v", tc.name, acc)
			}
		})
	}
}

func accOf(name string) Acc {
	switch name {
	case "sum":
		return AccSum
	case "count":
		return AccCount
	case "max":
		return AccMax
	case "min":
		return AccMin
	case "avg":
		return AccAvg
	case "value":
		return AccValue
	default:
		return AccFirst
	}
}

// 一次事件两条记录：count() 计一次，sum 累加两条。
func TestEventCountIsPerEvent(t *testing.T) {
	b := &bag{}
	m, c, plans := newTestManager(t, b)
	r := &recorder{}
	register(t, m, plans, 1, mustParse(t, "Event(event_id=1001 & item_class=10, count()) >= 2"), r.handler)

	for i := 0; i < 2; i++ {
		cs := NewChangeSet()
		if err := cs.Event(1001); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 3; j++ {
			if err := cs.EventValue(map[Attr]int64{attrClass: 10, attrCount: 1}); err != nil {
				t.Fatal(err)
			}
		}
		m.Trigger(cs)
	}
	if got, _ := c.Read(1, 0, AccCount); got != 2 {
		t.Fatalf("两次事件（每次三条记录）计次 = %d, 期望 2", got)
	}
	if !r.last().Match {
		t.Fatalf("应命中: %+v", r.last())
	}
}

func TestEventOwn(t *testing.T) {
	b := &bag{}
	m, _, plans := newTestManager(t, b)
	r := &recorder{}
	e := mustParse(t, "Event(event_id=1001, count(), own(Item(item_class=2001 & item_quality>3))) >= 1")
	register(t, m, plans, 1, e, r.handler)

	cs := NewChangeSet()
	if err := cs.Event(1001); err != nil {
		t.Fatal(err)
	}
	m.Trigger(cs)
	if r.last().Match {
		t.Fatalf("own 不满足时不应计数: %+v", r.last())
	}

	b.items = []rec{item(2001, 5, 1, 1)}
	cs2 := NewChangeSet()
	if err := cs2.Event(1001); err != nil {
		t.Fatal(err)
	}
	cs2.State(domainItem, attrClass, 0, 2001)
	m.Trigger(cs2)
	if !r.last().Match || r.last().Progress[0].Value != 1 {
		t.Fatalf("own 满足后应计数: %+v", r.last())
	}
}

func TestOwnSharesStateQueryWithOuterLeaf(t *testing.T) {
	b := &bag{items: []rec{item(2001, 5, 1, 1)}}
	m, counter, plans := newTestManager(t, b)
	e := mustParse(t, "Item(item_class=2001 & item_quality>3) >= 1 & Event(event_id=1001, count(), own(Item(item_class=2001 & item_quality>3))) >= 1")
	if err := plans.Add(e); err != nil {
		t.Fatal(err)
	}
	for _, key := range []uint64{1, 2} {
		if err := m.Register(key, e, nil); err != nil {
			t.Fatal(err)
		}
	}

	before := b.evals
	cs := NewChangeSet()
	if err := cs.Event(1001); err != nil {
		t.Fatal(err)
	}
	cs.State(domainItem, attrClass, 2001, 2001)
	m.Trigger(cs)
	if got := b.evals - before; got != 1 {
		t.Fatalf("普通状态叶与 own 在一轮内查询 %d 次，期望 1 次", got)
	}
	for _, key := range []uint64{1, 2} {
		if got, has := counter.Read(key, 1, AccCount); !has || got != 1 {
			t.Fatalf("实例 %d 事件计数 = %d (has=%v)，期望 1", key, got, has)
		}
	}
}

func TestOwnStateChangeDirtiesWithoutNotify(t *testing.T) {
	b := &bag{}
	m, counter, plans := newTestManager(t, b)
	r := &recorder{}
	register(t, m, plans, 1, mustParse(t, "Event(event_id=1001, count(), own(Item(item_class=2001 & item_quality>3))) >= 1"), r.handler)

	fireEvent := func() {
		t.Helper()
		cs := NewChangeSet()
		if err := cs.Event(1001); err != nil {
			t.Fatal(err)
		}
		m.Trigger(cs)
	}
	fireEvent() // own=false，填充共享缓存。
	if len(r.results) != 2 {
		t.Fatalf("首次事件后回调次数 = %d，期望 2", len(r.results))
	}

	b.items = []rec{item(2001, 5, 1, 1)}
	stateChange := NewChangeSet()
	stateChange.State(domainItem, attrClass, 0, 2001)
	m.Trigger(stateChange)
	if len(r.results) != 2 {
		t.Fatalf("own 状态变化不应唤醒计划，回调次数 = %d", len(r.results))
	}
	if _, has := counter.Read(1, 0, AccCount); has {
		t.Fatal("own 状态变化不应更新 Counter")
	}

	fireEvent()
	if got, has := counter.Read(1, 0, AccCount); !has || got != 1 {
		t.Fatalf("标脏后事件计数 = %d (has=%v)，期望 1", got, has)
	}
}

func TestOwnUsesRoundSharedState(t *testing.T) {
	b := &bag{items: []rec{item(2001, 5, 1, 1)}}
	m, counter, plans := newTestManager(t, b)
	e := mustParse(t, "Event(event_id=1001, count(), own(Item(item_class=2001 & item_quality>3))) >= 1")
	if err := plans.Add(e); err != nil {
		t.Fatal(err)
	}
	mutated := false
	h := func(res Result) bool {
		if res.Round == 0 || mutated {
			return false
		}
		mutated = true
		b.items = nil
		cs := NewChangeSet()
		cs.State(domainItem, attrClass, 2001, 0)
		m.Trigger(cs)
		return false
	}
	if err := m.Register(1, e, h); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(2, e, nil); err != nil {
		t.Fatal(err)
	}
	register(t, m, plans, 3, mustParse(t, "false"), nil)

	fireEvent := func() {
		t.Helper()
		cs := NewChangeSet()
		if err := cs.Event(1001); err != nil {
			t.Fatal(err)
		}
		m.Trigger(cs)
	}
	fireEvent()
	for _, key := range []uint64{1, 2} {
		if got, has := counter.Read(key, 0, AccCount); !has || got != 1 {
			t.Fatalf("同一轮实例 %d 未共享 own 状态: value=%d has=%v", key, got, has)
		}
	}

	fireEvent()
	for _, key := range []uint64{1, 2} {
		if got, has := counter.Read(key, 0, AccCount); !has || got != 1 {
			t.Fatalf("后续轮未使用标脏后的 own 状态: key=%d value=%d has=%v", key, got, has)
		}
	}
}

func TestEventPrefilter(t *testing.T) {
	b := &bag{}
	m, c, plans := newTestManager(t, b)
	r := &recorder{}
	register(t, m, plans, 1, mustParse(t, "Event(event_id=1001 & item_class=10, count()) >= 1"), r.handler)

	// 不同事件 id：不进入候选。
	cs := NewChangeSet()
	if err := cs.Event(1002); err != nil {
		t.Fatal(err)
	}
	m.Trigger(cs)
	if _, ok := c.Read(1, 0, AccCount); ok {
		t.Fatal("事件 id 不匹配时不应计数")
	}

	// 同一事件 id，但记录属性不满足过滤器。
	cs2 := NewChangeSet()
	if err := cs2.Event(1001); err != nil {
		t.Fatal(err)
	}
	if err := cs2.EventValue(map[Attr]int64{attrClass: 99}); err != nil {
		t.Fatal(err)
	}
	m.Trigger(cs2)
	if _, ok := c.Read(1, 0, AccCount); ok {
		t.Fatal("过滤条件不满足时不应计数")
	}
}

// Handler 内再次 Trigger 应排队，并且排队的是深拷贝，Reset 原对象不影响。
func TestHandlerQueueAndClone(t *testing.T) {
	b := &bag{}
	m, c, plans := newTestManager(t, b)
	queued := false
	h := func(Result) bool {
		if !queued {
			queued = true
			inner := NewChangeSet()
			if err := inner.Event(1001); err != nil {
				t.Fatal(err)
			}
			if err := inner.EventValue(map[Attr]int64{attrClass: 10}); err != nil {
				t.Fatal(err)
			}
			m.Trigger(inner)
			inner.Reset() // 排队的是深拷贝，Reset 不应影响已排队的触发
		}
		return false
	}
	register(t, m, plans, 1, mustParse(t, "Event(event_id=1001 & item_class=10, count()) >= 1"), h)

	if got, _ := c.Read(1, 0, AccCount); got != 1 {
		t.Fatalf("排队触发应计 1 次，得到 %d", got)
	}
}

func TestResultProgressLifetime(t *testing.T) {
	m, _, plans := newTestManager(t, &bag{})
	var results []Result
	h := func(res Result) bool {
		results = append(results, res)
		return false
	}
	register(t, m, plans, 1, mustParse(t, "Event(event_id=1001) >= 2"), h)

	for i := 0; i < 2; i++ {
		cs := NewChangeSet()
		if err := cs.Event(1001); err != nil {
			t.Fatal(err)
		}
		m.Trigger(cs)
	}
	if len(results) != 3 {
		t.Fatalf("回调次数 = %d，期望 3", len(results))
	}
	want := []struct {
		has   bool
		value int64
	}{{false, 0}, {true, 1}, {true, 2}}
	for i, w := range want {
		got := results[i].Progress[0]
		if got.Has != w.has || got.Value != w.value {
			t.Errorf("结果 %d 被后续回调改写: %+v，期望 has=%v value=%d", i, got, w.has, w.value)
		}
	}
	if &results[0].Progress[0] == &results[1].Progress[0] {
		t.Fatal("不同 Result 仍共享 Progress 切片")
	}

	clone := results[0].Clone()
	results[0].Progress[0].Value = 99
	if clone.Progress[0].Value != 0 {
		t.Fatalf("Clone 不再返回独立副本: %+v", clone.Progress[0])
	}
}

func TestLeafNodeIndexAndCounterDel(t *testing.T) {
	b := &bag{items: []rec{item(2, 5, 1, 1)}}
	m, c, plans := newTestManager(t, b)
	done := false
	var lastEventValue int64
	h := func(res Result) bool {
		if len(res.Progress) != 2 {
			t.Fatalf("叶数量 = %d", len(res.Progress))
		}
		if res.Progress[1].Node != 1 {
			t.Fatalf("事件叶编号 = %d, 期望 1", res.Progress[1].Node)
		}
		lastEventValue = res.Progress[1].Value
		if res.Match && !done {
			done = true
			return true
		}
		return false
	}
	register(t, m, plans, 7, mustParse(t, "Item(item_class=2) >= 1 & Event(event_id=1001) >= 3"), h)

	for i := 0; i < 3; i++ {
		cs := NewChangeSet()
		if err := cs.Event(1001); err != nil {
			t.Fatal(err)
		}
		m.Trigger(cs)
	}
	if lastEventValue != 3 {
		t.Fatalf("事件叶累计 = %d, 期望 3", lastEventValue)
	}
	if c.dels != 1 {
		t.Fatalf("条件完成应清理计数，Del 次数 = %d", c.dels)
	}
	if m.Len() != 0 {
		t.Fatalf("条件完成后应注销，Len = %d", m.Len())
	}
}

// 同模板多实例共享一次状态查询，避免不同计划重复读取相同状态。
func TestSharedStateEvaluation(t *testing.T) {
	b := &bag{items: []rec{item(2, 5, 1, 1)}}
	m, _, plans := newTestManager(t, b)
	e := mustParse(t, "Item(item_class=2, sum(item_count)) >= 1")
	if err := plans.Add(e); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if err := m.Register(uint64(i+1), e, nil); err != nil {
			t.Fatal(err)
		}
	}
	if b.evals != 1 {
		t.Fatalf("1000 个实例注册应只求值 1 次，实际 %d", b.evals)
	}

	cs := NewChangeSet()
	cs.Mark(ExactKey(domainItem, attrClass, 2))
	m.Trigger(cs)
	if b.evals != 2 {
		t.Fatalf("一次触发应只求值 1 次，累计 %d", b.evals)
	}
}

// 含 OR 的过滤器使用通配依赖：任意 class 变化都要唤醒，不能漏选。
func TestWildcardDependency(t *testing.T) {
	b := &bag{items: []rec{item(1, 5, 1, 1)}}
	m, _, plans := newTestManager(t, b)
	r := &recorder{}
	register(t, m, plans, 1, mustParse(t, "Item(item_class=1 | item_class=2, sum(item_count)) >= 1"), r.handler)
	if !r.last().Match {
		t.Fatalf("初始应命中: %+v", r.last())
	}

	b.items = []rec{item(3, 5, 1, 1)}
	cs := NewChangeSet()
	cs.Mark(ExactKey(domainItem, attrClass, 3))
	m.Trigger(cs)
	if r.last().Match {
		t.Fatalf("class=3 时不应命中: %+v", r.last())
	}
}

// 同一个 ChangeSet 既改状态又带事件时：纯状态计划不能被事件粗筛跳过，
// 纯事件计划也不能被状态 Key 唤醒。
func TestMixedStateAndEventTrigger(t *testing.T) {
	b := &bag{items: []rec{item(2, 5, 5, 1)}}
	m, c, plans := newTestManager(t, b)
	state, event, mixed := &recorder{}, &recorder{}, &recorder{}

	register(t, m, plans, 1, mustParse(t, "Item(item_class=2, sum(item_count)) >= 10"), state.handler)
	register(t, m, plans, 2, mustParse(t, "Event(event_id=1001, count()) >= 1"), event.handler)
	register(t, m, plans, 3, mustParse(t, "Item(item_class=2, sum(item_count)) >= 10 & Event(event_id=1001, count()) >= 1"), mixed.handler)

	cs := NewChangeSet()
	if err := cs.Event(1001); err != nil {
		t.Fatal(err)
	}
	b.items = []rec{item(1, 5, 5, 1)} // class 2 -> 1：状态计划的 sum 从 5 变 0
	cs.State(domainItem, attrClass, 2, 1)
	m.Trigger(cs)

	if got := state.last().Progress[0].Value; got != 0 {
		t.Fatalf("纯状态计划未被重算，进度 = %d, 期望 0", got)
	}
	if state.last().Match {
		t.Fatalf("纯状态计划不应命中: %+v", state.last())
	}
	if !event.last().Match {
		t.Fatalf("纯事件计划应命中: %+v", event.last())
	}
	if mixed.last().Match {
		t.Fatalf("混合计划的状态叶不满足，不应命中: %+v", mixed.last())
	}
	if got, _ := c.Read(3, 1, AccCount); got != 1 {
		t.Fatalf("混合计划事件叶累计 = %d, 期望 1", got)
	}

	// 只含无关事件 Key 的触发不应唤醒任何计划。
	before := len(state.results) + len(event.results) + len(mixed.results)
	other := NewChangeSet()
	if err := other.Event(1002); err != nil {
		t.Fatal(err)
	}
	m.Trigger(other)
	if after := len(state.results) + len(event.results) + len(mixed.results); after != before {
		t.Fatalf("无关事件唤醒了计划: %d -> %d", before, after)
	}
}

// 事件 id 是 ChangeSet 的语义字段：只 Mark 事件域 Key 不算一次事件。
func TestEventIDIsExplicit(t *testing.T) {
	b := &bag{}
	m, c, plans := newTestManager(t, b)
	register(t, m, plans, 1, mustParse(t, "Event(event_id=1001, count()) >= 1"), nil)

	cs := NewChangeSet()
	cs.Mark(ExactKey(SourceEvent, eventAttrID, 1001))
	m.Trigger(cs)
	if _, ok := c.Read(1, 0, AccCount); ok {
		t.Fatal("只 Mark 事件 Key 不应被当作一次事件")
	}

	real := NewChangeSet()
	if err := real.Event(1001); err != nil {
		t.Fatal(err)
	}
	real.Reset()
	if err := real.EventValue(map[Attr]int64{attrClass: 10}); err != ErrEventMissing {
		t.Fatalf("Reset 后应要求重新 Event，得到 %v", err)
	}
}

// 排队触发的深拷贝必须保留事件身份与记录。
func TestChangeSetCloneKeepsEvent(t *testing.T) {
	cs := NewChangeSet()
	if err := cs.Event(1001); err != nil {
		t.Fatal(err)
	}
	if err := cs.EventValue(map[Attr]int64{attrClass: 10, attrCount: 3}); err != nil {
		t.Fatal(err)
	}
	cp := cs.clone()
	cs.Reset()

	if cp.eventID() != 1001 {
		t.Fatalf("clone 后事件 id = %d, 期望 1001", cp.eventID())
	}
	if len(cp.records) != 1 || len(cp.records[0].values) != 2 {
		t.Fatalf("clone 后记录 = %+v", cp.records)
	}
	if cs.eventID() != 0 || len(cs.records) != 0 {
		t.Fatalf("Reset 未清空: event=%d records=%d", cs.eventID(), len(cs.records))
	}
}

// 触发时按注册顺序回调，而不是按实例键或 map 顺序。
func TestRegistrationOrder(t *testing.T) {
	b := &bag{items: []rec{item(2, 5, 1, 1)}}
	m, _, plans := newTestManager(t, b)
	e := mustParse(t, "Item(item_class=2) >= 1")
	if err := plans.Add(e); err != nil {
		t.Fatal(err)
	}
	var order []uint64
	for _, k := range []uint64{3, 1, 2} {
		key := k
		if err := m.Register(key, e, func(Result) bool {
			order = append(order, key)
			return false
		}); err != nil {
			t.Fatal(err)
		}
	}

	order = nil
	cs := NewChangeSet()
	cs.Mark(ExactKey(domainItem, attrClass, 2))
	m.Trigger(cs)

	if len(order) != 3 || order[0] != 3 || order[1] != 1 || order[2] != 2 {
		t.Fatalf("回调顺序 = %v, 期望 [3 1 2]", order)
	}
}

// 回调内注销其他实例：只留下空洞，不打断本轮遍历。
func TestCancelDuringDispatch(t *testing.T) {
	b := &bag{items: []rec{item(2, 5, 1, 1)}}
	m, _, plans := newTestManager(t, b)
	e := mustParse(t, "Item(item_class=2) >= 1")
	if err := plans.Add(e); err != nil {
		t.Fatal(err)
	}
	armed := false
	var order []uint64
	for _, k := range []uint64{1, 2, 3} {
		key := k
		h := func(Result) bool {
			if armed && key == 1 {
				m.Unregister(2)
			}
			order = append(order, key)
			return false
		}
		if err := m.Register(key, e, h); err != nil {
			t.Fatal(err)
		}
	}

	armed = true
	order = nil
	cs := NewChangeSet()
	cs.Mark(ExactKey(domainItem, attrClass, 2))
	m.Trigger(cs)

	if len(order) != 2 || order[0] != 1 || order[1] != 3 {
		t.Fatalf("回调顺序 = %v, 期望 [1 3]", order)
	}
	if m.Len() != 2 {
		t.Fatalf("注销后 Len = %d, 期望 2", m.Len())
	}
	p := m.plans[e.signature()]
	if len(p.active) != 2 || p.activeCount != 2 || p.dirty {
		t.Fatalf("本轮结束后墓碑应已清理: len=%d count=%d dirty=%v", len(p.active), p.activeCount, p.dirty)
	}
}

// 触发之外的注销立即回收；注册按注册顺序追加。
func TestUnregisterReclaimsSlot(t *testing.T) {
	b := &bag{items: []rec{item(2, 5, 1, 1)}}
	m, _, plans := newTestManager(t, b)
	e := mustParse(t, "Item(item_class=2) >= 1")
	if err := plans.Add(e); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if err := m.Register(uint64(i), e, nil); err != nil {
			t.Fatal(err)
		}
	}
	p := m.plans[e.signature()]
	m.Unregister(3)
	if len(p.active) != 2 || p.activeCount != 2 {
		t.Fatalf("尾部注销后 len=%d count=%d, 期望 2/2", len(p.active), p.activeCount)
	}

	if err := m.Register(4, e, nil); err != nil {
		t.Fatal(err)
	}
	if len(p.active) != 3 || p.activeCount != 3 {
		t.Fatalf("复用尾部后 len=%d count=%d, 期望 3/3", len(p.active), p.activeCount)
	}
	if p.active[0].key != 1 || p.active[1].key != 2 || p.active[2].key != 4 {
		t.Fatalf("顺序 = [%d %d %d], 期望 [1 2 4]", p.active[0].key, p.active[1].key, p.active[2].key)
	}
}

// 批量注销后 active 只保留活跃实例，注册顺序不变。
func TestBulkUnregister(t *testing.T) {
	b := &bag{items: []rec{item(2, 5, 1, 1)}}
	m, _, plans := newTestManager(t, b)
	e := mustParse(t, "Item(item_class=2) >= 1")
	if err := plans.Add(e); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 100; i++ {
		if err := m.Register(uint64(i), e, nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 90; i++ {
		m.Unregister(uint64(i))
	}

	count := 0
	if err := m.Register(101, e, func(Result) bool { count++; return false }); err != nil {
		t.Fatal(err)
	}
	count = 0
	cs := NewChangeSet()
	cs.Mark(ExactKey(domainItem, attrClass, 2))
	m.Trigger(cs)
	if count != 1 {
		t.Fatalf("压缩后回调次数 = %d, 期望 1", count)
	}
	if m.Len() != 11 {
		t.Fatalf("Len = %d, 期望 11", m.Len())
	}
}

func TestRegisterErrors(t *testing.T) {
	b := &bag{}
	m, _, plans := newTestManager(t, b)
	e := mustParse(t, "Item(item_class=2) >= 1")

	if err := m.Register(1, e, nil); err != errUnknownPlan {
		t.Fatalf("未登记计划应报错，得到 %v", err)
	}
	if err := plans.Add(e); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(1, nil, nil); err != errNilExpr {
		t.Fatalf("nil 表达式应报错，得到 %v", err)
	}
	if err := m.Register(1, e, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(1, e, nil); err != errDuplicate {
		t.Fatalf("重复实例应报错，得到 %v", err)
	}
	if _, err := NewManager[uint64](nil, b, plans); err != errNilCounter {
		t.Fatalf("nil Counter 应报错，得到 %v", err)
	}
	if _, err := NewManager(newMemCounter[uint64](), b, nil); err != errNilPlans {
		t.Fatalf("nil Plans 应报错，得到 %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"Item()", "必须带过滤条件"},
		{"Event()", "需要 event_id"},
		{"Event(event_id=0)", "不能为 0"},
		{"Event(event_id=1, own(Event(event_id=2)))", "own(...) 内不能使用 Event"},
		{"Event(event_id=1, count(), typo(true))", "期望 own(...)"},
		{"Item(item_class=1) <1,2", "只能有一个值"},
		{"Item(item_class=1,)", "期望聚合函数"},
		{"Item(unknown=1)", "未知属性"},
		{"Item(item_class=1) !", "多余的"},
		{"Item(item_class=1) 1-2-3", "多余的"},
		{"count(1)", "未知叶子"},
	}
	for _, tc := range cases {
		_, err := Parse(tc.expr, testCfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("Parse(%q) 错误 = %v, 期望包含 %q", tc.expr, err, tc.want)
		}
	}
}

func TestParseNegativeRanges(t *testing.T) {
	for _, expr := range []string{
		"Item(item_class -3--1)",
		"Item(item_class=1) -3--1",
	} {
		t.Run(expr, func(t *testing.T) {
			if _, err := Parse(expr, testCfg); err != nil {
				t.Fatalf("Parse(%q) 拒绝了负数区间: %v", expr, err)
			}
		})
	}
}

func TestParseConstants(t *testing.T) {
	b := &bag{}
	m, _, plans := newTestManager(t, b)
	r := &recorder{}
	register(t, m, plans, 1, mustParse(t, "true"), r.handler)
	if !r.last().Match {
		t.Fatalf("true 应恒真: %+v", r.last())
	}
	register(t, m, plans, 2, mustParse(t, "false | Item(item_class=2) >= 1"), r.handler)
	if r.last().Match {
		t.Fatalf("空集合不应命中: %+v", r.last())
	}
}

// Query.Index 只从纯 AND 等值谓词推导，业务可直接用来分桶。
func TestQueryIndex(t *testing.T) {
	b := &bag{items: []rec{item(2, 5, 1, 7)}}
	m, _, plans := newTestManager(t, b)
	register(t, m, plans, 1, mustParse(t, "Item(item_tid=7 & item_class=2, sum(item_count)) >= 1"), nil)

	idx := b.lastQuery.Index
	if len(idx[attrTid]) != 1 || idx[attrTid][0] != 7 {
		t.Fatalf("Index = %v", idx)
	}
	if len(idx[attrClass]) != 1 || idx[attrClass][0] != 2 {
		t.Fatalf("Index = %v", idx)
	}

	// 含 OR 时退化为通配依赖，不给精确索引。
	b2 := &bag{items: []rec{item(2, 5, 1, 7)}}
	m2, _, plans2 := newTestManager(t, b2)
	register(t, m2, plans2, 1, mustParse(t, "Item(item_tid=7 | item_tid=8, sum(item_count)) >= 1"), nil)
	if idx := b2.lastQuery.Index; idx != nil {
		t.Fatalf("含 OR 时不应给精确索引: %v", idx)
	}
}

// 跨事件的均值按“每次事件的聚合值”再平均，而不是展开所有记录。
func TestAvgAcrossEvents(t *testing.T) {
	b := &bag{}
	m, c, plans := newTestManager(t, b)
	register(t, m, plans, 1, mustParse(t, "Event(event_id=1001, avg(item_count)) >= 6"), nil)

	first := NewChangeSet()
	if err := first.Event(1001); err != nil {
		t.Fatal(err)
	}
	if err := first.EventValue(map[Attr]int64{attrCount: 2}); err != nil {
		t.Fatal(err)
	}
	if err := first.EventValue(map[Attr]int64{attrCount: 4}); err != nil {
		t.Fatal(err)
	}
	m.Trigger(first) // 本次均值为 3

	second := NewChangeSet()
	if err := second.Event(1001); err != nil {
		t.Fatal(err)
	}
	if err := second.EventValue(map[Attr]int64{attrCount: 10}); err != nil {
		t.Fatal(err)
	}
	m.Trigger(second) // 本次均值为 10

	if got, _ := c.Read(1, 0, AccAvg); got != 6 {
		t.Fatalf("跨事件均值 = %d, 期望 (3+10)/2 = 6", got)
	}
}

func TestEventAvgInt64Overflow(t *testing.T) {
	const maxInt int64 = 9223372036854775807
	const minInt int64 = -maxInt - 1
	tests := []struct {
		name   string
		values []int64
		want   int64
	}{
		{"positive_overflow", []int64{maxInt, maxInt}, maxInt},
		{"negative_overflow", []int64{minInt, minInt}, minInt},
		{"overflow_before_mixed_signs", []int64{maxInt, maxInt, minInt}, 3074457345618258602},
		{"negative_fraction_truncates_toward_zero", []int64{minInt, minInt, minInt + 1}, minInt + 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, counter, plans := newTestManager(t, &bag{})
			register(t, m, plans, 1, mustParse(t, "Event(event_id=1001, avg(item_count)) >= 1"), nil)

			cs := NewChangeSet()
			if err := cs.Event(1001); err != nil {
				t.Fatal(err)
			}
			for _, value := range tc.values {
				if err := cs.EventValue(map[Attr]int64{attrCount: value}); err != nil {
					t.Fatal(err)
				}
			}
			m.Trigger(cs)

			if got, has := counter.Read(1, 0, AccAvg); !has || got != tc.want {
				t.Fatalf("事件均值 = %d (has=%v)，期望 %d；输入 %v", got, has, tc.want, tc.values)
			}
		})
	}
}

// 同一事件内 value 取最后一条匹配记录，first 取第一条；跨事件 value 覆盖、first 保留。
func TestValueVsFirstWithinEvent(t *testing.T) {
	b := &bag{}
	m, c, plans := newTestManager(t, b)
	register(t, m, plans, 1, mustParse(t, "Event(event_id=1001 & item_class=10, value(item_count)) >= 9"), nil)
	register(t, m, plans, 2, mustParse(t, "Event(event_id=1001 & item_class=10, first(item_count)) >= 7"), nil)

	fire := func(counts ...int64) {
		t.Helper()
		cs := NewChangeSet()
		if err := cs.Event(1001); err != nil {
			t.Fatal(err)
		}
		for _, v := range counts {
			if err := cs.EventValue(map[Attr]int64{attrClass: 10, attrCount: v}); err != nil {
				t.Fatal(err)
			}
		}
		m.Trigger(cs)
	}

	fire(7, 9)
	if got, _ := c.Read(1, 0, AccValue); got != 9 {
		t.Fatalf("同事件内 value = %d, 期望最后一条 9", got)
	}
	if got, _ := c.Read(2, 0, AccFirst); got != 7 {
		t.Fatalf("同事件内 first = %d, 期望第一条 7", got)
	}

	fire(3)
	if got, _ := c.Read(1, 0, AccValue); got != 3 {
		t.Fatalf("跨事件 value = %d, 期望覆盖为 3", got)
	}
	if got, _ := c.Read(2, 0, AccFirst); got != 7 {
		t.Fatalf("跨事件 first = %d, 期望保留 7", got)
	}
}

// value 每次都覆盖为最新观测值。
func TestValueAcrossEvents(t *testing.T) {
	b := &bag{}
	m, c, plans := newTestManager(t, b)
	register(t, m, plans, 1, mustParse(t, "Event(event_id=1001, value(item_count)) >= 9"), nil)

	for _, v := range []int64{7, 9} {
		cs := NewChangeSet()
		if err := cs.Event(1001); err != nil {
			t.Fatal(err)
		}
		if err := cs.EventValue(map[Attr]int64{attrCount: v}); err != nil {
			t.Fatal(err)
		}
		m.Trigger(cs)
	}
	if got, _ := c.Read(1, 0, AccValue); got != 9 {
		t.Fatalf("value = %d, 期望 9", got)
	}
}

// 纯事件场景可以不接 StateSource。
func TestNilStateSource(t *testing.T) {
	c := newMemCounter[uint64]()
	plans := NewPlans()
	m, err := NewManager(c, nil, plans)
	if err != nil {
		t.Fatal(err)
	}
	e := mustParse(t, "Event(event_id=1001) >= 1")
	if err := plans.Add(e); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(1, e, nil); err != nil {
		t.Fatal(err)
	}
	cs := NewChangeSet()
	if err := cs.Event(1001); err != nil {
		t.Fatal(err)
	}
	m.Trigger(cs)
	if got, _ := c.Read(1, 0, AccCount); got != 1 {
		t.Fatalf("纯事件计数 = %d", got)
	}
}

// 同一份 Plans 可跨 Manager 共享，运行期绑定互相独立。
func TestPlansSharedAcrossManagers(t *testing.T) {
	e := mustParse(t, "Item(item_class=2, sum(item_count)) >= 1")
	plans := NewPlans()
	if err := plans.Add(e); err != nil {
		t.Fatal(err)
	}
	b1 := &bag{items: []rec{item(2, 5, 1, 1)}}
	c1 := newMemCounter[uint64]()
	m1, err := NewManager(c1, b1, plans)
	if err != nil {
		t.Fatal(err)
	}
	b2 := &bag{}
	c2 := newMemCounter[uint64]()
	m2, err := NewManager(c2, b2, plans)
	if err != nil {
		t.Fatal(err)
	}
	r1, r2 := &recorder{}, &recorder{}
	if err := m1.Register(1, e, r1.handler); err != nil {
		t.Fatal(err)
	}
	if err := m2.Register(1, e, r2.handler); err != nil {
		t.Fatal(err)
	}
	if !r1.last().Match || r2.last().Match {
		t.Fatalf("两个 Manager 应各自求值: %+v %+v", r1.last(), r2.last())
	}
}
