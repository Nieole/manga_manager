package organize

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"manga-manager/internal/database"
)

var (
	ErrRelationSelf          = errors.New("A series cannot relate to itself")
	ErrRelationTargetMissing = errors.New("Target series not found")
	ErrRelationMissing       = errors.New("Relation not found")
	ErrRelationTypeRequired  = errors.New("relation_type cannot be empty")
)

type RelationStore interface {
	ExecTx(context.Context, func(*database.Queries) error) error
	ListForwardSeriesRelations(context.Context, int64) ([]database.ListForwardSeriesRelationsRow, error)
	ListReverseSeriesRelations(context.Context, int64) ([]database.ListReverseSeriesRelationsRow, error)
	GetConnectedSeriesRelations(context.Context, int64) ([]database.GetConnectedSeriesRelationsRow, error)
	GetAllSeriesRelationsForLibrary(context.Context, database.GetAllSeriesRelationsForLibraryParams) ([]database.GetAllSeriesRelationsForLibraryRow, error)
}

// Relations owns relation mutations and the coalesced rebuilding of derived collection identities.
// runBackground registers work with the application's lifecycle and must eventually execute it.
type Relations struct {
	store     RelationStore
	rebuilder *franchiseRebuilder
}

func NewRelations(store RelationStore, runBackground func(func())) *Relations {
	s := &Relations{store: store}
	s.rebuilder = newFranchiseRebuilder(s.Rebuild, runBackground)
	return s
}

type CreatedRelation struct {
	Existing bool
	ID       int64
}

func (s *Relations) Create(ctx context.Context, source, target int64, kind string) (CreatedRelation, error) {
	if source == target {
		return CreatedRelation{}, ErrRelationSelf
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "sequel"
	}
	var result CreatedRelation
	err := s.store.ExecTx(ctx, func(q *database.Queries) error {
		if _, err := q.SeriesExistsByID(ctx, target); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrRelationTargetMissing
			}
			return err
		}
		id, err := q.FindExistingSeriesRelation(ctx, database.FindExistingSeriesRelationParams{LeftID: source, RightID: target})
		if err == nil {
			result = CreatedRelation{Existing: true, ID: id}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return q.CreateSeriesRelation(ctx, database.CreateSeriesRelationParams{SourceSeriesID: source, TargetSeriesID: target, RelationType: kind})
	})
	if err != nil {
		return CreatedRelation{}, err
	}
	if !result.Existing {
		s.rebuilder.schedule()
	}
	return result, nil
}
func (s *Relations) Update(ctx context.Context, id int64, kind string) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return ErrRelationTypeRequired
	}
	err := s.store.ExecTx(ctx, func(q *database.Queries) error {
		return q.UpdateSeriesRelation(ctx, database.UpdateSeriesRelationParams{ID: id, RelationType: kind})
	})
	if err != nil {
		return err
	}
	s.rebuilder.schedule()
	return nil
}
func (s *Relations) Delete(ctx context.Context, id int64) error {
	err := s.store.ExecTx(ctx, func(q *database.Queries) error {
		n, err := q.DeleteSeriesRelation(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrRelationMissing
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.rebuilder.schedule()
	return nil
}
func (s *Relations) Graph(ctx context.Context, id int64) ([]database.GetConnectedSeriesRelationsRow, error) {
	return s.store.GetConnectedSeriesRelations(ctx, id)
}
func (s *Relations) LibraryGraph(ctx context.Context, id int64) ([]database.GetAllSeriesRelationsForLibraryRow, error) {
	return s.store.GetAllSeriesRelationsForLibrary(ctx, database.GetAllSeriesRelationsForLibraryParams{LibraryID: id, LibraryID_2: id})
}

type SeriesRelation struct {
	ID               int64  `json:"id"`
	TargetSeriesID   int64  `json:"target_series_id"`
	TargetSeriesName string `json:"target_series_name"`
	RelationType     string `json:"relation_type"`
}

// inverseRelationType returns the inverse relation type for display
// when viewing a relation from the target's perspective.
// e.g. A → B (spinoff) means B sees A as "parent_story"
func inverseRelationType(rt string) string {
	switch rt {
	case "sequel":
		return "prequel"
	case "prequel":
		return "sequel"
	case "spinoff":
		return "parent_story"
	case "side_story":
		return "parent_story"
	case "adaptation":
		return "source"
	case "remake":
		return "original"
	case "same_universe":
		return "same_universe"
	case "parent_story":
		return "spinoff"
	case "alternative_version":
		return "alternative_version"
	case "alternate_story":
		return "alternate_story"
	case "crossover":
		return "crossover"
	case "one_shot":
		return "serialization"
	case "anthology":
		return "original"
	case "doujinshi":
		return "original"
	default:
		return rt
	}
}

func (s *Relations) List(ctx context.Context, seriesID int64) ([]SeriesRelation, error) {
	forward, err := s.store.ListForwardSeriesRelations(ctx, seriesID)
	if err != nil {
		return nil, err
	}

	items := make([]SeriesRelation, 0, len(forward))
	for _, row := range forward {
		items = append(items, SeriesRelation{
			ID:               row.ID,
			TargetSeriesID:   row.TargetSeriesID,
			TargetSeriesName: row.TargetSeriesName,
			RelationType:     row.RelationType,
		})
	}

	reverse, err := s.store.ListReverseSeriesRelations(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	for _, row := range reverse {
		items = append(items, SeriesRelation{
			ID:               row.ID,
			TargetSeriesID:   row.TargetSeriesID,
			TargetSeriesName: row.TargetSeriesName,
			RelationType:     inverseRelationType(row.RelationType),
		})
	}

	return items, nil
}
