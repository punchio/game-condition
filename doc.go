// Package condition evaluates declarative game conditions and progress.
//
// State leaves read current business state through StateSource. Event leaves
// aggregate records from the current ChangeSet and fold the resulting delta
// into an application-provided Counter. Dependency indexes only select
// candidates; real-time filtering and expression evaluation decide matches.
//
// Expressions parsed by Parse are immutable and can be shared by managers.
// A Manager serves one owner and is not safe for concurrent use. Plans can be
// shared after configuration loading. Each evaluation returns an independent
// Result.Progress slice that callers may retain.
//
// Manager and Counter use the same caller-selected comparable InstanceKey type.
// Integer and string keys work directly; comparable structs can carry multiple
// fields, such as an owner identity and a condition instance ID.
//
// The package does not include business models or storage implementations.
// Applications define state systems, attributes, StateSource, and Counter.
package condition
