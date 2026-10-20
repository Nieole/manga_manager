// Package relocation checks deterministic library root mappings and owns their
// durable plans and paged details. It never changes library paths or book files
// while checking; task and run lifecycle remain owned by task and taskstore.
package relocation
