// Package relocation owns deterministic root mappings, durable checks and
// confirmations, and explicit restoration of protected libraries. Read-only
// checks preserve records and files; committed changes retain exclusive access
// through derived effects and recovery. Task lifecycle belongs to taskstore.
package relocation
