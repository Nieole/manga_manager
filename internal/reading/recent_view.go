package reading

import (
	"context"

	"manga-manager/internal/database"
)

// RecentBooksView binds a batch of recent book projections to its reader.
type RecentBooksView struct {
	Subject Subject
	Items   []database.GetRecentReadAllRow
}

// RecentBooks keeps the subject selection inside the complete recent-reading query.
func RecentBooks(ctx context.Context, store RecentStore, subject Subject, limit int64) (RecentBooksView, error) {
	if err := subject.validate(); err != nil {
		return RecentBooksView{}, err
	}
	items, err := RecentReads(ctx, store, subject.userID, limit)
	if err != nil {
		return RecentBooksView{}, err
	}
	return RecentBooksView{Subject: subject, Items: items}, nil
}
