// Package organize owns manual and smart collections, snapshots, names and source ownership.
package organize

import (
	"context"
	"database/sql"
	"errors"
	"manga-manager/internal/database"
	"strings"
)

var (
	ErrCollectionNameTaken       = errors.New("collection name already exists")
	ErrCollectionNameRequired    = errors.New("Name is required")
	ErrCollectionNotFound        = errors.New("Collection not found")
	ErrCollectionReadOnly        = errors.New("Franchise collections are derived from series relations and cannot be edited")
	ErrCollectionMembersRequired = errors.New("series_ids is required")
	ErrCollectionTooManyMembers  = errors.New("Too many series in one request")
	ErrCollectionMemberNotFound  = errors.New("Series is not in this collection")
)

const FranchiseCollectionSourceType = "system_franchise"
const MaxCollectionBatchSize = 1000

// DerivedCollectionSourceType identifies collections owned by continuous derivation.
func DerivedCollectionSourceType(source string) bool { return source == FranchiseCollectionSourceType }

type CollectionNameReader interface {
	CollectionNameExists(context.Context, string) (int64, error)
}

// CollectionNameExists uses SQLite's name comparison for all creation sources.
func CollectionNameExists(ctx context.Context, q CollectionNameReader, name string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, nil
	}
	_, err := q.CollectionNameExists(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// CheckCollectionName checks uniqueness inside the caller's write transaction.
func CheckCollectionName(ctx context.Context, q CollectionNameReader, name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrCollectionNameRequired
	}
	exists, err := CollectionNameExists(ctx, q, name)
	if err != nil {
		return err
	}
	if exists {
		return ErrCollectionNameTaken
	}
	return nil
}

type CollectionStore interface {
	ExecTx(context.Context, func(*database.Queries) error) error
	GetStaticCollectionView(context.Context, int64) (database.GetStaticCollectionViewRow, error)
	ListCollectionsWithSeriesCount(context.Context) ([]database.ListCollectionsWithSeriesCountRow, error)
	ListCollectionSeries(context.Context, int64) ([]database.ListCollectionSeriesRow, error)
}
type Collections struct{ store CollectionStore }

func NewCollections(store CollectionStore) *Collections { return &Collections{store: store} }
func (s *Collections) Get(ctx context.Context, id int64) (database.GetStaticCollectionViewRow, error) {
	row, err := s.store.GetStaticCollectionView(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrCollectionNotFound
	}
	return row, err
}
func (s *Collections) List(ctx context.Context) ([]database.ListCollectionsWithSeriesCountRow, error) {
	return s.store.ListCollectionsWithSeriesCount(ctx)
}
func (s *Collections) Members(ctx context.Context, id int64) ([]database.ListCollectionSeriesRow, error) {
	return s.store.ListCollectionSeries(ctx, id)
}
func description(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}
func (s *Collections) Create(ctx context.Context, name, desc string) (int64, error) {
	name = strings.TrimSpace(name)
	var id int64
	err := s.store.ExecTx(ctx, func(q *database.Queries) error {
		if err := CheckCollectionName(ctx, q, name); err != nil {
			return err
		}
		var err error
		id, err = q.CreateSimpleCollection(ctx, database.CreateSimpleCollectionParams{Name: name, Description: description(desc)})
		return err
	})
	return id, err
}
func writable(ctx context.Context, q *database.Queries, id int64) (database.GetStaticCollectionViewRow, error) {
	row, err := q.GetStaticCollectionView(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return row, ErrCollectionNotFound
	}
	if err != nil {
		return row, err
	}
	if DerivedCollectionSourceType(row.SourceType) {
		return row, ErrCollectionReadOnly
	}
	return row, nil
}
func (s *Collections) Update(ctx context.Context, id int64, name, desc string) error {
	name = strings.TrimSpace(name)
	return s.store.ExecTx(ctx, func(q *database.Queries) error {
		row, err := writable(ctx, q, id)
		if err != nil {
			return err
		}
		if name == "" {
			return ErrCollectionNameRequired
		}
		if !sameCollectionName(strings.TrimSpace(row.Name), name) {
			if err := CheckCollectionName(ctx, q, name); err != nil {
				return err
			}
		}
		return q.UpdateCollectionDetails(ctx, database.UpdateCollectionDetailsParams{ID: id, Name: name, Description: description(desc)})
	})
}
func (s *Collections) Delete(ctx context.Context, id int64) error {
	return s.store.ExecTx(ctx, func(q *database.Queries) error {
		if _, err := writable(ctx, q, id); err != nil {
			return err
		}
		_, err := q.DeleteCollection(ctx, id)
		return err
	})
}
func (s *Collections) AddMembers(ctx context.Context, id int64, ids []int64) (int, error) {
	added := 0
	err := s.store.ExecTx(ctx, func(q *database.Queries) error {
		if _, err := writable(ctx, q, id); err != nil {
			return err
		}
		if len(ids) == 0 {
			return ErrCollectionMembersRequired
		}
		if len(ids) > MaxCollectionBatchSize {
			return ErrCollectionTooManyMembers
		}
		for _, sid := range ids {
			n, err := q.AddSeriesToCollection(ctx, database.AddSeriesToCollectionParams{CollectionID: id, SeriesID: sid})
			if err != nil {
				return err
			}
			added += int(n)
		}
		return q.TouchCollection(ctx, id)
	})
	if err != nil {
		return 0, err
	}
	return added, nil
}
func (s *Collections) RemoveMember(ctx context.Context, id, sid int64) error {
	return s.store.ExecTx(ctx, func(q *database.Queries) error {
		if _, err := writable(ctx, q, id); err != nil {
			return err
		}
		n, err := q.RemoveSeriesFromCollection(ctx, database.RemoveSeriesFromCollectionParams{CollectionID: id, SeriesID: sid})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrCollectionMemberNotFound
		}
		return q.TouchCollection(ctx, id)
	})
}

// SQLite NOCASE folds ASCII only; Unicode folding can incorrectly bypass a collision.
func sameCollectionName(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := 0; i < len(left); i++ {
		a, b := left[i], right[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
