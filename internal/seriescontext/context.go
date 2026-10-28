// Package seriescontext owns the complete reader-bound series detail query.
package seriescontext

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"manga-manager/internal/proposal"
	"manga-manager/internal/reading"
	"manga-manager/internal/seriesmeta"
	"manga-manager/internal/task"
	"time"
)

type Store interface {
	reading.BookQueryStore
	GetSeries(context.Context, int64) (database.Series, error)
	GetTagsForSeries(context.Context, int64) ([]database.Tag, error)
	GetAuthorsForSeries(context.Context, int64) ([]database.Author, error)
	GetLinksForSeries(context.Context, int64) ([]database.SeriesLink, error)
}
type Relations interface {
	List(context.Context, int64) ([]organize.SeriesRelation, error)
}
type Proposals interface {
	ListBySeries(context.Context, int64) (proposal.SeriesProposals, error)
}
type Runs interface {
	ListSnapshots(context.Context, task.RunFilter) ([]task.Snapshot, error)
}

var ErrMissing = errors.New("series not found")

type Result struct {
	PendingReviews, ProvenanceCount int
	LatestFailure                   *time.Time
	Series                          database.Series
	Books                           reading.BookSeriesView
	Tags                            []database.Tag
	Authors                         []database.Author
	Links                           []database.SeriesLink
	Relations                       []organize.SeriesRelation
	Proposals                       proposal.SeriesProposals
	FailedRuns                      []task.Snapshot
	MetadataVersion                 string
}
type AuxiliaryError struct {
	Part string
	Err  error
}

func (e *AuxiliaryError) Error() string {
	return fmt.Sprintf("series context %s unavailable: %v", e.Part, e.Err)
}
func (e *AuxiliaryError) Unwrap() error { return e.Err }

type Service struct {
	store     Store
	relations Relations
	proposals Proposals
	runs      Runs
}

func New(store Store, relations Relations, proposals Proposals, runs Runs) *Service {
	return &Service{store, relations, proposals, runs}
}
func (s *Service) Load(ctx context.Context, subject reading.Subject, id int64) (Result, error) {
	var out Result
	var err error
	if _, err = subject.UserID(); err != nil {
		return out, err
	}
	out.Series, err = s.store.GetSeries(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrMissing
	}
	if err != nil {
		return out, err
	}
	out.Books, err = reading.NewQueries(s.store).SeriesBooks(ctx, subject, id)
	if err != nil {
		return out, err
	}
	out.Tags, err = s.store.GetTagsForSeries(ctx, id)
	if err != nil {
		return out, &AuxiliaryError{"tags", err}
	}
	if out.Tags == nil {
		out.Tags = []database.Tag{}
	}
	out.Authors, err = s.store.GetAuthorsForSeries(ctx, id)
	if err != nil {
		return out, &AuxiliaryError{"authors", err}
	}
	if out.Authors == nil {
		out.Authors = []database.Author{}
	}
	out.Links, err = s.store.GetLinksForSeries(ctx, id)
	if err != nil {
		return out, &AuxiliaryError{"links", err}
	}
	if out.Links == nil {
		out.Links = []database.SeriesLink{}
	}
	out.Relations, err = s.relations.List(ctx, id)
	if err != nil {
		return out, &AuxiliaryError{"relations", err}
	}
	if out.Relations == nil {
		out.Relations = []organize.SeriesRelation{}
	}
	out.Proposals, err = s.proposals.ListBySeries(ctx, id)
	if err != nil {
		return out, &AuxiliaryError{"metadata_review", err}
	}
	out.FailedRuns, err = s.runs.ListSnapshots(ctx, task.RunFilter{Statuses: []task.RunStatus{task.StatusFailed}, Scope: task.ScopeSeries, ScopeID: &id, Limit: 5, Order: task.OrderLiveFirst})
	if err != nil {
		return out, &AuxiliaryError{"failed_tasks", err}
	}
	out.PendingReviews = len(out.Proposals.Proposals)
	out.ProvenanceCount = len(out.Proposals.Provenance)
	for _, snap := range out.FailedRuns {
		if out.LatestFailure == nil || snap.Run.UpdatedAt.After(*out.LatestFailure) {
			at := snap.Run.UpdatedAt
			out.LatestFailure = &at
		}
	}
	out.MetadataVersion = seriesmeta.Version(out.Series, out.Tags, out.Authors, out.Links)
	return out, nil
}
