# Migrating to generic `InstanceKey`

This guide applies to code written against earlier revisions where `condition.InstanceKey` was an alias for `uint64` and `condition.Counter` / `condition.Manager` were non-generic.

The key type is now selected by the application:

```go
type Counter[InstanceKey comparable] interface {
	Update(k InstanceKey, index uint16, acc Acc, val int64)
	Read(k InstanceKey, index uint16, acc Acc) (val int64, has bool)
	Del(k InstanceKey)
}

func NewManager[InstanceKey comparable](
	counter Counter[InstanceKey],
	state StateSource,
	plans *Plans,
) (*Manager[InstanceKey], error)
```

`InstanceKey` is a type parameter, not an exported concrete type. A counter implementation can be generic, but it does not have to be: an existing concrete store whose methods use `uint64` satisfies `condition.Counter[uint64]`.

## 1. Keep `uint64` keys

This is the smallest migration and requires no counter-data conversion.

Replace the removed declarations as follows:

| Earlier API | Migrated API |
| --- | --- |
| `condition.InstanceKey` | `uint64` |
| `condition.Counter` | `condition.Counter[uint64]` |
| `*condition.Manager` | `*condition.Manager[uint64]` |
| `map[condition.InstanceKey]V` | `map[uint64]V` |
| `Update(k condition.InstanceKey, ...)` | `Update(k uint64, ...)` |
| `Read(k condition.InstanceKey, ...)` | `Read(k uint64, ...)` |
| `Del(k condition.InstanceKey)` | `Del(k uint64)` |

Apply the same `uint64` replacement to `Register`, `Unregister`, helper functions, and test fixtures that previously used the alias. Counter method bodies and persisted values can remain unchanged.

For an existing application-defined `store`, a compile-time assertion can confirm the migrated implementation:

```go
var _ condition.Counter[uint64] = (*store)(nil)
```

`NewManager` normally infers the key type from a non-nil counter:

```go
func newManager(
	counter *store,
	state condition.StateSource,
	plans *condition.Plans,
) (*condition.Manager[uint64], error) {
	return condition.NewManager(counter, state, plans)
}
```

Explicit instantiation is also accepted:

```go
manager, err := condition.NewManager[uint64](counter, state, plans)
```

When an untyped `nil` counter is passed to test constructor validation, type inference has no counter from which to determine the key type. Add the type argument explicitly:

```go
_, err := condition.NewManager[uint64](nil, state, plans)
```

## 2. Use string keys

Provide a counter satisfying `condition.Counter[string]`, then register strings directly:

```go
manager, err := condition.NewManager[string](counter, state, plans)
if err != nil {
	return err
}

if err := manager.Register("quest-42", expr, handler); err != nil {
	return err
}
```

The same key must be used for unregistering:

```go
manager.Unregister("quest-42")
```

No numeric conversion or framework-side string encoding is involved.

## 3. Use a comparable struct key

A comparable struct can carry identity fields that previously had to be packed into an integer:

```go
type questKey struct {
	OwnerID     uint64
	ConditionID uint64
}
```

Given an application-defined constructor for a counter satisfying `condition.Counter[questKey]`:

```go
var counter condition.Counter[questKey] = newQuestCounter()

manager, err := condition.NewManager(counter, state, plans)
if err != nil {
	return err
}

key := questKey{OwnerID: 7001, ConditionID: 42}
if err := manager.Register(key, expr, handler); err != nil {
	return err
}

manager.Unregister(key)
```

All fields of the struct must be comparable. Structs containing slices, maps, or functions cannot be used. Arrays and nested structs are supported when all their elements or fields are also comparable.

## 4. Generic counter implementations

A storage implementation may itself accept the same key type parameter:

```go
type memoryCounter[InstanceKey comparable] struct {
	values map[InstanceKey]map[uint16]int64
}
```

Its `Update`, `Read`, and `Del` receivers and key parameters must use that same `InstanceKey`. See the complete `AccCount`-only implementation in [example_test.go](../example_test.go); production stores still need to implement the aggregation semantics required by their expressions.

Using a generic store is optional. Separate concrete implementations for `uint64`, `string`, or a struct are equally valid.

## 5. Shared storage and persisted data

- A manager still serves one owner. If multiple owners share a counter backend, keys must be unique across that backend.
- A struct containing owner and condition identifiers can express this uniqueness directly.
- The framework does not serialize generic keys. Redis, database, or network encoding remains the responsibility of the application's `Counter` implementation.
- Keeping `uint64` keys does not require persisted-key migration.
- Changing from `uint64` to another key representation requires an application-specific storage migration. Do not reinterpret existing encoded keys without one.
- Leaf indexes and aggregation semantics are unchanged. Existing rules for migrating counters after expression leaf changes still apply.

## 6. APIs that remain non-generic

The key type parameter is intentionally limited to the manager, counter, and their internal runtime dependencies. These public types remain non-generic:

- `Plans` and `Expr`
- `ChangeSet` and `StateSource`
- `Result`, `LeafState`, and `Handler`

Expression syntax, state caching, event aggregation, progress ownership, handler behavior, and reentrant queue semantics are unchanged.

## Migration checklist

1. Replace every use of the removed `condition.InstanceKey` alias.
2. Add `[uint64]` or the selected key type to explicit `Counter` and `Manager` types.
3. Update counter maps, method receivers, method parameters, and registration helpers consistently.
4. Add an explicit key type to `NewManager` calls that pass an untyped nil counter.
5. Review key uniqueness and serialization when sharing storage or changing key representations.
6. Run the application's tests and static checks:

```bash
go test ./...
go vet ./...
```
