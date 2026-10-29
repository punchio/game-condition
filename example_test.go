package condition_test

import (
	"fmt"

	"github.com/punchio/game-condition"
)

// exampleCounter is a minimal AccCount-only example, not production storage.
type exampleCounter struct {
	values map[condition.InstanceKey]map[uint16]int64
}

func newExampleCounter() *exampleCounter {
	return &exampleCounter{values: make(map[condition.InstanceKey]map[uint16]int64)}
}

func (c *exampleCounter) Update(k condition.InstanceKey, index uint16, acc condition.Acc, val int64) {
	if acc != condition.AccCount {
		panic("exampleCounter only supports AccCount")
	}
	slots := c.values[k]
	if slots == nil {
		slots = make(map[uint16]int64)
		c.values[k] = slots
	}
	slots[index] += val
}

func (c *exampleCounter) Read(k condition.InstanceKey, index uint16, acc condition.Acc) (int64, bool) {
	if acc != condition.AccCount {
		return 0, false
	}
	value, ok := c.values[k][index]
	return value, ok
}

func (c *exampleCounter) Del(k condition.InstanceKey) {
	delete(c.values, k)
}

// exampleState evaluates one in-memory record for AccSum queries.
type exampleState struct {
	values map[condition.Attr]int64
}

func (s *exampleState) Eval(q condition.Query) (int64, bool) {
	if q.Agg != condition.AccSum || !q.Filter.Match(s.get) {
		return 0, false
	}
	value, ok := s.values[q.Attr]
	return value, ok
}

func (s *exampleState) get(attr condition.Attr) (int64, bool) {
	value, ok := s.values[attr]
	return value, ok
}

func mustExample(err error) {
	if err != nil {
		panic(err)
	}
}

func Example() {
	expr, err := condition.Parse("Event(1001) >= 2", nil)
	mustExample(err)
	plans := condition.NewPlans()
	mustExample(plans.Add(expr))
	manager, err := condition.NewManager(newExampleCounter(), nil, plans)
	mustExample(err)

	err = manager.Register(42, expr, func(result condition.Result) bool {
		fmt.Printf("round=%d match=%t progress=%d\n",
			result.Round, result.Match, result.Progress[0].Value)
		return result.Match
	})
	mustExample(err)

	for i := 0; i < 2; i++ {
		changes := condition.NewChangeSet()
		mustExample(changes.Event(1001))
		manager.Trigger(changes)
	}
	fmt.Printf("registered=%d\n", manager.Len())

	// Output:
	// round=0 match=false progress=0
	// round=1 match=false progress=1
	// round=2 match=true progress=2
	// registered=0
}

func ExampleManager() {
	const (
		attrClass  condition.Attr   = 1
		attrCount  condition.Attr   = 2
		itemSource condition.Source = 2
	)
	config := &condition.ParseConfig{
		Attrs: map[string]condition.Attr{
			"item_class": attrClass,
			"item_count": attrCount,
		},
		StateSystems: map[string]condition.Source{"Item": itemSource},
	}
	state := &exampleState{values: map[condition.Attr]int64{
		attrClass: 2,
		attrCount: 5,
	}}
	expr, err := condition.Parse("Item(item_class=2, sum(item_count)) >= 10", config)
	mustExample(err)
	plans := condition.NewPlans()
	mustExample(plans.Add(expr))
	manager, err := condition.NewManager(newExampleCounter(), state, plans)
	mustExample(err)

	err = manager.Register(7, expr, func(result condition.Result) bool {
		fmt.Printf("round=%d match=%t value=%d\n",
			result.Round, result.Match, result.Progress[0].Value)
		return result.Match
	})
	mustExample(err)

	state.values[attrCount] = 12
	changes := condition.NewChangeSet()
	changes.State(itemSource, attrCount, 5, 12)
	manager.Trigger(changes)
	fmt.Printf("registered=%d\n", manager.Len())

	// Output:
	// round=0 match=false value=5
	// round=1 match=true value=12
	// registered=0
}
