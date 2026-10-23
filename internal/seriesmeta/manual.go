// Package seriesmeta owns series metadata rules for each writing source.
// Callers retain transaction ownership and derived-data refresh scheduling.
package seriesmeta

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"manga-manager/internal/database"
)

// ManualQueries is the transaction-bound persistence needed for manual replacement.
// database.Queries satisfies it without exposing transaction or raw SQL operations.
type ManualQueries interface {
	VersionQueries
	UpdateSeriesMetadata(context.Context, database.UpdateSeriesMetadataParams) (database.Series, error)
	ClearSeriesTags(context.Context, int64) error
	UpsertTag(context.Context, string) (database.Tag, error)
	LinkSeriesTag(context.Context, database.LinkSeriesTagParams) error
	ClearSeriesAuthors(context.Context, int64) error
	UpsertAuthor(context.Context, database.UpsertAuthorParams) (database.Author, error)
	LinkSeriesAuthor(context.Context, database.LinkSeriesAuthorParams) error
	ClearSeriesLinks(context.Context, int64) error
	LinkSeriesLink(context.Context, database.LinkSeriesLinkParams) (database.SeriesLink, error)
}

// Author is an author's name and role supplied by a manual editor.
type Author struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// Link is a named address supplied by a manual editor.
type Link struct {
	Name string `json:"name"`
	Url  string `json:"url"`
}

// ManualInput replaces scalars and each non-nil collection; empty collections clear.
// Manual edits can change locked fields and the locks themselves.
type ManualInput struct {
	Title           string
	Summary         string
	Publisher       string
	Status          string
	Rating          float64
	Language        string
	LockedFields    string
	Tags            []string
	Authors         []Author
	Links           []Link
	ExpectedVersion *string
}

// ErrConflict means the content version changed; SaveManual returns the current version.
var ErrConflict = errors.New("series metadata version conflict")

// SaveManual checks the version and replaces metadata in the caller's transaction.
// current is the caller's existing snapshot, preserving the title fallback timing.
// On failure the caller must roll back; stats refresh remains in that transaction.
func SaveManual(ctx context.Context, q ManualQueries, current database.Series, input ManualInput) (string, error) {
	if input.ExpectedVersion != nil {
		version, err := LoadVersion(ctx, q, current.ID)
		if err != nil {
			return "", err
		}
		if version != *input.ExpectedVersion {
			return version, ErrConflict
		}
	}
	_, err := q.UpdateSeriesMetadata(ctx, database.UpdateSeriesMetadataParams{
		Title:        sql.NullString{String: input.Title, Valid: input.Title != ""},
		Summary:      sql.NullString{String: input.Summary, Valid: input.Summary != ""},
		Publisher:    sql.NullString{String: input.Publisher, Valid: input.Publisher != ""},
		Status:       sql.NullString{String: input.Status, Valid: input.Status != ""},
		Rating:       sql.NullFloat64{Float64: input.Rating, Valid: input.Rating > 0},
		Language:     sql.NullString{String: input.Language, Valid: input.Language != ""},
		LockedFields: sql.NullString{String: input.LockedFields, Valid: true},
		NameInitial:  database.SeriesInitial(input.Title, current.Name),
		ID:           current.ID,
	})
	if err != nil {
		return "", err
	}

	// Replacement errors reach the transaction owner so clearing and rebuilding roll back together.
	if input.Tags != nil {
		if err := q.ClearSeriesTags(ctx, current.ID); err != nil {
			return "", err
		}
		for _, t := range input.Tags {
			if strings.TrimSpace(t) == "" {
				continue
			}
			inserted, err := q.UpsertTag(ctx, t)
			if err != nil {
				return "", err
			}
			if err := q.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: current.ID, TagID: inserted.ID}); err != nil {
				return "", err
			}
		}
	}

	if input.Authors != nil {
		if err := q.ClearSeriesAuthors(ctx, current.ID); err != nil {
			return "", err
		}
		for _, a := range input.Authors {
			if strings.TrimSpace(a.Name) == "" {
				continue
			}
			inserted, err := q.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: a.Name, Role: a.Role})
			if err != nil {
				return "", err
			}
			if err := q.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: current.ID, AuthorID: inserted.ID}); err != nil {
				return "", err
			}
		}
	}

	if input.Links != nil {
		if err := q.ClearSeriesLinks(ctx, current.ID); err != nil {
			return "", err
		}
		for _, link := range input.Links {
			if strings.TrimSpace(link.Name) == "" || strings.TrimSpace(link.Url) == "" {
				continue
			}
			if _, err := q.LinkSeriesLink(ctx, database.LinkSeriesLinkParams{
				SeriesID: current.ID,
				Name:     link.Name,
				Url:      link.Url,
			}); err != nil {
				return "", err
			}
		}
	}

	return "", nil
}
