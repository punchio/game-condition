# game-condition

[![CI](https://github.com/punchio/game-condition/actions/workflows/ci.yml/badge.svg)](https://github.com/punchio/game-condition/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/punchio/game-condition.svg)](https://pkg.go.dev/github.com/punchio/game-condition)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

`game-condition` is a lightweight condition and progress evaluation library for game services. It evaluates declarative expressions over both current state and event deltas, while leaving business models and storage to the application. It is not a general-purpose rules engine.

The module uses only the Go standard library.

## Features

- Separate state and event semantics: current state comes from `StateSource`; event deltas come from the current `ChangeSet`.
- Boolean expressions with `!`, `&`, `|`, and parentheses.
- Record-level filters, thresholds, and seven aggregations: `value`, `count`, `sum`, `avg`, `max`, `min`, and `first`.
- Dependency indexes for candidate selection, followed by real-time filtering and evaluation.
- Shared state queries within a manager and plan-level event delta calculation.
- Optional `own(...)` state prerequisites for event counters.
- Pluggable `Counter` storage for in-memory, Redis, database, or custom backends.
- Independent `Result.Progress` slices that callers can safely retain.

## Installation

Go 1.22 or later is required.

```bash
go get github.com/punchio/game-condition
```

```go
import "github.com/punchio/game-condition"
```

## Quick start

The core event-counting flow looks like this:

```go
expr, err := condition.Parse("Event(1001) >= 2", nil)
if err != nil {
	panic(err)
}

plans := condition.NewPlans()
if err := plans.Add(expr); err != nil {
	panic(err)
}

manager, err := condition.NewManager(newExampleCounter[string](), nil, plans)
if err != nil {
	panic(err)
}

err = manager.Register("quest-42", expr, func(result condition.Result) bool {
	fmt.Printf("round=%d match=%t progress=%d\n",
		result.Round, result.Match, result.Progress[0].Value)
	return result.Match // Complete and unregister when matched.
})
if err != nil {
	panic(err)
}

for i := 0; i < 2; i++ {
	changes := condition.NewChangeSet()
	if err := changes.Event(1001); err != nil {
		panic(err)
	}
	manager.Trigger(changes)
}
```

The [complete runnable example](https://github.com/punchio/game-condition/blob/main/example_test.go) defines `newExampleCounter`, a minimal `AccCount`-only implementation used to keep the example small. Replace it with your storage-backed `Counter` in production. The same file also contains a `StateSource` example.

Registration evaluates and calls the handler immediately with `Round == 0`. In this example, the two events advance progress to 1 and then 2. Returning `true` from the handler completes and unregisters the condition.

## Expression language

A state leaf reads current business state:

```text
Item(item_class=2, sum(item_count)) >= 10
```

An event leaf aggregates records from each reported event:

```text
Event(event_id=1001 & item_class=10, sum(item_count)) >= 3
```

Key rules:

- Operator precedence is `!` > `&` > `|`; parentheses group expressions.
- Event arguments follow event match, aggregation, then optional `own(...)`.
- Aggregation defaults to `count()` and the threshold defaults to `>=1`.
- `event_id` is mandatory and must be nonzero.
- `own(...)` is allowed only as the final event argument. It checks current state before updating that event counter.
- State system names, attributes, and data sources are defined by the application through `ParseConfig`.

See the [Chinese design documentation](docs/design.md) for the complete grammar and semantics.

## State and events

State and events answer different questions:

- `StateSource` reads current, persistent business state after the application has updated it.
- Event records in a `ChangeSet` describe deltas that happened in this trigger. They are not replaced by the player's current state.
- Filters evaluate each record independently; attributes from different records are never combined into one record.
- Dependency metadata only removes impossible candidates. Real-time filters and expression evaluation determine the final result.

## Counter contract

Applications implement `Counter[InstanceKey]` to persist event progress. `InstanceKey` is a `comparable` type parameter shared with the manager, so integers, strings, and comparable structs can be used directly without conversion:

```go
type Counter[InstanceKey comparable] interface {
	Update(k InstanceKey, index uint16, acc Acc, val int64)
	Read(k InstanceKey, index uint16, acc Acc) (val int64, has bool)
	Del(k InstanceKey)
}
```

- A counter slot is identified by `(InstanceKey, leaf index)`.
- Leaf indexes follow expression order starting at zero. State leaves consume an index but do not read or write the counter.
- `Update` and `Read` must use the same key and aggregation semantics.
- `Read` returns `has=false` when no usable observation exists.
- The manager calls `Del` when a condition completes or is unregistered.

Changing leaf order or adding leaves changes their indexes. Migrate or reset persisted counters accordingly.

## Results and lifecycle

- Every evaluation creates an independent `Result.Progress` slice. Callers may retain results without cloning them; `Clone` remains available for an explicit deep copy.
- Registration performs an initial evaluation and optional handler callback with `Round == 0`.
- Returning `true` from a handler completes the condition, unregisters it, and deletes its counters.
- `Register` and `Trigger` calls made inside a handler are queued. Queued triggers deep-copy their `ChangeSet`, including event records.

## Concurrency

A `Manager` serves one owner and is not safe for concurrent use. Expressions returned by `Parse` are immutable and can be shared across managers. A `Plans` table can be shared after configuration loading is complete.

A `Counter` and `Plans` may be shared across managers. When sharing a counter across owners, make keys unique across the shared storage. A comparable struct containing both the owner identity and condition instance ID can provide this directly, preventing one owner from overwriting another's progress.

## Testing and benchmarks

```bash
go test ./...
go test -race ./...
go vet ./...
go test -bench=. -benchmem ./...
```

## Documentation

- [Chinese design documentation](docs/design.md)
- [Migrating to generic InstanceKey](docs/migration-generic-instance-key.md)
- [v1.0.0 release notes](docs/release-notes-v1.0.0.md)
- [Editable Trigger sequence diagram](docs/diagrams/trigger-sequence/trigger-sequence.kanva.pptd)

## License

Released under the [MIT License](LICENSE).
