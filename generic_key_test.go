package condition

import "testing"

func TestGenericInstanceKeyString(t *testing.T) {
	counter := newMemCounter[string]()
	plans := NewPlans()
	manager, err := NewManager(counter, nil, plans)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	expr := mustParse(t, "Event(1001) >= 2")
	if err := plans.Add(expr); err != nil {
		t.Fatalf("Plans.Add: %v", err)
	}
	results := make(map[string]Result)
	for _, key := range []string{"quest-a", "quest-b"} {
		key := key
		if err := manager.Register(key, expr, func(result Result) bool {
			results[key] = result
			return false
		}); err != nil {
			t.Fatalf("Register(%q): %v", key, err)
		}
	}
	changes := NewChangeSet()
	if err := changes.Event(1001); err != nil {
		t.Fatalf("Event: %v", err)
	}
	manager.Trigger(changes)
	manager.Trigger(changes)
	for _, key := range []string{"quest-a", "quest-b"} {
		value, has := counter.Read(key, 0, AccCount)
		if !has || value != 2 {
			t.Fatalf("Read(%q) = (%d, %v), want (2, true)", key, value, has)
		}
		if got := results[key].Progress[0].Value; got != 2 {
			t.Fatalf("Progress[%q] = %d, want 2", key, got)
		}
	}
	manager.Unregister("quest-a")
	if _, has := counter.Read("quest-a", 0, AccCount); has {
		t.Fatal("quest-a counter survived Unregister")
	}
	if value, has := counter.Read("quest-b", 0, AccCount); !has || value != 2 {
		t.Fatalf("quest-b counter = (%d, %v), want (2, true)", value, has)
	}
	if counter.dels != 1 {
		t.Fatalf("Del calls = %d, want 1", counter.dels)
	}
}

func TestGenericInstanceKeyComparableStruct(t *testing.T) {
	type instanceKey struct {
		Owner string
		ID    uint64
	}
	key := instanceKey{Owner: "player-7", ID: 42}
	counter := newMemCounter[instanceKey]()
	plans := NewPlans()
	manager, err := NewManager(counter, nil, plans)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	expr := mustParse(t, "Event(1002) >= 1")
	if err := plans.Add(expr); err != nil {
		t.Fatalf("Plans.Add: %v", err)
	}
	var last Result
	if err := manager.Register(key, expr, func(result Result) bool {
		last = result
		return false
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	changes := NewChangeSet()
	if err := changes.Event(1002); err != nil {
		t.Fatalf("Event: %v", err)
	}
	manager.Trigger(changes)
	if value, has := counter.Read(key, 0, AccCount); !has || value != 1 {
		t.Fatalf("Read = (%d, %v), want (1, true)", value, has)
	}
	if !last.Match || last.Progress[0].Value != 1 {
		t.Fatalf("callback result = %+v, want match with progress 1", last)
	}
	manager.Unregister(key)
	if _, has := counter.Read(key, 0, AccCount); has {
		t.Fatal("struct-key counter survived Unregister")
	}
	if counter.dels != 1 {
		t.Fatalf("Del calls = %d, want 1", counter.dels)
	}
}
