// Package relocation owns deterministic root mappings, durable checks and
// confirmations, and explicit restoration of protected libraries. Read-only
// checks preserve records and files; committed changes retain exclusive access
// through derived effects and recovery. Execute reports durable commit facts to
// application callers even when later effects fail. Private SQLite adapters own
// operation history, path transactions, restoration CAS and policy journals;
// configuration coordination retains its separate compensation semantics.
// Task lifecycle belongs to task and taskstore.
package relocation
