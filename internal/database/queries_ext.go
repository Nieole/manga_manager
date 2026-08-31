package database

// DBTX exposes the query executor for narrowly scoped custom statements that
// are intentionally kept outside sqlc generation.
func (q *Queries) DBTX() DBTX {
	return q.db
}
