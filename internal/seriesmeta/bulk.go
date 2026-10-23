package seriesmeta

import (
	"context"
	"manga-manager/internal/database"
	"strings"
)

// SelectionQueries supplies transaction-bound set SQL without transaction ownership.
type SelectionQueries interface {
	SetSeriesStatusBatch(context.Context, []int64, string) error
	SetSeriesPublisherBatch(context.Context, []int64, string) error
	UpsertTag(context.Context, string) (database.Tag, error)
	LinkSeriesTagBatch(context.Context, []int64, int64) error
	UnlinkSeriesTagByNameBatch(context.Context, []int64, string) error
}

// SelectionInput changes supported scalars when non-nil and applies tag deltas.
// Scalars preserve raw values; tag names are trimmed. Locks and provenance are untouched.
type SelectionInput struct {
	Status     *string
	Publisher  *string
	AddTags    []string
	RemoveTags []string
}

// EditSelection applies scalars, adds tags, then removes tags in the caller's transaction.
// Missing IDs are tolerated. Any error requires rollback of the entire selection.
func EditSelection(ctx context.Context, q SelectionQueries, ids []int64, input SelectionInput) error {
	if len(ids) == 0 {
		return nil
	}
	if input.Status != nil {
		if err := q.SetSeriesStatusBatch(ctx, ids, *input.Status); err != nil {
			return err
		}
	}
	if input.Publisher != nil {
		if err := q.SetSeriesPublisherBatch(ctx, ids, *input.Publisher); err != nil {
			return err
		}
	}
	for _, name := range input.AddTags {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tag, err := q.UpsertTag(ctx, name)
		if err != nil {
			return err
		}
		if err := q.LinkSeriesTagBatch(ctx, ids, tag.ID); err != nil {
			return err
		}
	}
	for _, name := range input.RemoveTags {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if err := q.UnlinkSeriesTagByNameBatch(ctx, ids, name); err != nil {
			return err
		}
	}
	return nil
}
