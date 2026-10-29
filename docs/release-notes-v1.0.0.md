# game-condition v1.0.0: Generic Instance Keys

**Tag:** `v1.0.0`

**Target:** `main`

**Upgrade impact:** First tagged release; breaking source API change for users of earlier untagged revisions.

## Highlights

- `Manager` and `Counter` now support application-selected comparable instance keys.
- Use integers, strings, or comparable structs directly as condition instance keys.
- Struct keys can naturally carry both owner identity and condition instance ID for shared storage.
- The key type propagates through registration, dependency indexing, evaluation contexts, and queued commands with compile-time type safety.

The new constructor signature is:

```go
func NewManager[InstanceKey comparable](
	counter Counter[InstanceKey],
	state StateSource,
	plans *Plans,
) (*Manager[InstanceKey], error)
```

`NewManager` can usually infer `InstanceKey` from its counter argument.

## Breaking changes

- The exported `condition.InstanceKey = uint64` alias has been removed.
- `Counter` is now `Counter[InstanceKey comparable]`.
- `Manager` is now `Manager[InstanceKey comparable]`.
- `Register`, `Unregister`, and all counter methods use the selected `InstanceKey`.
- Explicit manager and counter types must include their key type, such as `*condition.Manager[uint64]` and `condition.Counter[uint64]`.
- Calls to `NewManager` with an untyped nil counter need an explicit type argument, such as `condition.NewManager[uint64](nil, state, plans)`.

Existing applications can retain `uint64` keys by replacing `condition.InstanceKey` with `uint64` and updating explicit generic types. Counter implementations do not need to become generic unless the application wants them to be.

## Using other key types

A string-keyed manager can register business identifiers directly:

```go
manager, err := condition.NewManager[string](counter, state, plans)
if err != nil {
	return err
}
err = manager.Register("quest-42", expr, handler)
```

A comparable struct can replace manual key packing:

```go
type questKey struct {
	OwnerID     uint64
	ConditionID uint64
}
```

Applications using Redis, databases, or other encoded storage remain responsible for serializing their selected key type and migrating persisted keys when that representation changes.

See the [full migration guide](https://github.com/punchio/game-condition/blob/main/docs/migration-generic-instance-key.md).

## Unchanged behavior

`Plans`, `Expr`, `ChangeSet`, `StateSource`, `Result`, and `Handler` remain non-generic. Expression syntax, state caching, event aggregation, independent progress slices, handler completion, and reentrant command queue behavior are unchanged.

Existing tests and benchmarks now instantiate the manager with `uint64`. New tests exercise string and comparable-struct keys through registration, counter updates and reads, callbacks, unregistration, and deletion. Public examples cover both string and `uint64` keys.

## Validation

The following local checks passed for this change:

```bash
go test -count=1 ./...
go vet ./...
go test -run '^Example' -count=1 -v .
go test -run '^$' -bench . -benchtime 1x -count=1 .
```

Formatting and `git diff --check` also passed. Benchmark execution was a one-iteration smoke test, not a performance comparison. Race-enabled tests were not run in the local Windows environment because gcc was unavailable. Before tagging, the GitHub CI workflow must pass on both Go 1.22.x and stable; each job includes `go test -race -count=1 ./...`.
