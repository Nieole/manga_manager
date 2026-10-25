package organize

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"manga-manager/internal/database"
)

var (
	ErrAIGroupingNotPending = errors.New("AI grouping review collection is not pending")
	ErrAIGroupingSeriesGone = errors.New("AI grouping review collection has no surviving series")
	ErrAIGroupingNotFound   = errors.New("AI grouping review collection not found")
)

type InvalidAIGroupingEdit struct{ Message string }

func (e *InvalidAIGroupingEdit) Error() string { return e.Message }

type AIGroupingStore interface {
	GetSeriesWithoutCollection(context.Context, int64) ([]database.GetSeriesWithoutCollectionRow, error)
	ExecTx(context.Context, func(*database.Queries) error) error
	GetAIGroupingReview(context.Context, int64) (database.AiGroupingReview, error)
	ListAIGroupingReviewCollections(context.Context, int64) ([]database.AiGroupingReviewCollection, error)
}

// AIGrouping owns generated collection suggestions and their explicit adjudication, independently of series metadata proposals.
type AIGrouping struct {
	store   AIGroupingStore
	refresh func()
}

// NewAIGrouping publishes refresh only after a successful durable collection application.
func NewAIGrouping(store AIGroupingStore, refresh func()) *AIGrouping {
	return &AIGrouping{store: store, refresh: refresh}
}

type AIGroupingReview struct {
	Review      database.AiGroupingReview
	Collections []database.AiGroupingReviewCollection
}

func (s *AIGrouping) Review(ctx context.Context, id int64) (AIGroupingReview, error) {
	review, err := s.store.GetAIGroupingReview(ctx, id)
	if err != nil {
		return AIGroupingReview{}, err
	}
	rows, err := s.store.ListAIGroupingReviewCollections(ctx, id)
	return AIGroupingReview{Review: review, Collections: rows}, err
}
func pendingAIGrouping(ctx context.Context, q *database.Queries, reviewID int64) (database.AiGroupingReview, error) {
	review, err := q.GetAIGroupingReview(ctx, reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return review, ErrAIGroupingNotFound
	}
	if err != nil {
		return review, err
	}
	if review.Status != "pending" {
		return review, ErrAIGroupingNotPending
	}
	return review, nil
}
func pendingAICollection(ctx context.Context, q *database.Queries, reviewID, collectionID int64) (database.AiGroupingReview, database.AiGroupingReviewCollection, error) {
	review, err := pendingAIGrouping(ctx, q, reviewID)
	if err != nil {
		return review, database.AiGroupingReviewCollection{}, err
	}
	row, err := q.GetAIGroupingReviewCollection(ctx, collectionID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && row.ReviewID != reviewID {
		return review, row, ErrAIGroupingNotFound
	}
	if err != nil {
		return review, row, err
	}
	if row.Status != "pending" {
		return review, row, ErrAIGroupingNotPending
	}
	return review, row, nil
}
func finishAIGroupingIfReady(ctx context.Context, q *database.Queries, reviewID int64) error {
	pending, err := q.CountPendingAIGroupingReviewCollections(ctx, reviewID)
	if err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}
	return finalizeAIGroupingReviewStatus(ctx, q, reviewID)
}

// ApplyCollection creates a manually owned collection and adjudicates its candidate atomically.
func (s *AIGrouping) ApplyCollection(ctx context.Context, reviewID, collectionID int64) (int64, error) {
	var id int64
	err := s.store.ExecTx(ctx, func(q *database.Queries) error {
		review, row, err := pendingAICollection(ctx, q, reviewID, collectionID)
		if err != nil {
			return err
		}
		id, err = applyAIGroupingReviewCollectionWithQueries(ctx, q, review, row)
		if err != nil {
			return err
		}
		return finishAIGroupingIfReady(ctx, q, reviewID)
	})
	if err != nil {
		return 0, err
	}
	s.refresh()
	return id, nil
}

type AIGroupingApplyResult struct{ Applied, Skipped int }

// ApplyReview observes a pending snapshot and rejects candidates adjudicated before its write transaction.
func (s *AIGrouping) ApplyReview(ctx context.Context, reviewID int64) (AIGroupingApplyResult, error) {
	snapshot, err := s.Review(ctx, reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return AIGroupingApplyResult{}, ErrAIGroupingNotFound
	}
	if err != nil {
		return AIGroupingApplyResult{}, err
	}
	if snapshot.Review.Status != "pending" {
		return AIGroupingApplyResult{}, ErrAIGroupingNotPending
	}
	var result AIGroupingApplyResult
	err = s.store.ExecTx(ctx, func(q *database.Queries) error {
		if _, err := pendingAIGrouping(ctx, q, reviewID); err != nil {
			return err
		}
		for _, candidate := range snapshot.Collections {
			if candidate.Status != "pending" {
				continue
			}
			review, row, err := pendingAICollection(ctx, q, reviewID, candidate.ID)
			if err != nil {
				return err
			}
			_, err = applyAIGroupingReviewCollectionWithQueries(ctx, q, review, row)
			switch {
			case errors.Is(err, ErrAIGroupingSeriesGone):
				result.Skipped++
			case err != nil:
				return err
			default:
				result.Applied++
			}
		}
		return finishAIGroupingIfReady(ctx, q, reviewID)
	})
	if err != nil {
		return AIGroupingApplyResult{}, err
	}
	if result.Applied == 0 && result.Skipped > 0 {
		return result, ErrAIGroupingSeriesGone
	}
	s.refresh()
	return result, nil
}

type EditAIGroupingCollection struct {
	Name, Description string
	SeriesIDs         []int64
}

func (s *AIGrouping) EditCollection(ctx context.Context, reviewID, collectionID int64, req EditAIGroupingCollection) (database.AiGroupingReviewCollection, error) {
	var result database.AiGroupingReviewCollection
	err := s.store.ExecTx(ctx, func(q *database.Queries) error {
		_, _, err := pendingAICollection(ctx, q, reviewID, collectionID)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return &InvalidAIGroupingEdit{Message: "Name is required"}
		}
		raw, _ := json.Marshal(req.SeriesIDs)
		ids := AIGroupingSeriesIDs(string(raw))
		if len(ids) == 0 {
			return &InvalidAIGroupingEdit{Message: "series_ids is required"}
		}
		candidates, err := q.ListAIGroupingReviewCollections(ctx, reviewID)
		if err != nil {
			return err
		}
		allowed := map[int64]struct{}{}
		for _, candidate := range candidates {
			if candidate.Status != "pending" && candidate.ID != collectionID {
				continue
			}
			for _, id := range AIGroupingSeriesIDs(candidate.SeriesIds) {
				allowed[id] = struct{}{}
			}
		}
		for _, id := range ids {
			if _, ok := allowed[id]; !ok {
				return &InvalidAIGroupingEdit{Message: "series_ids must come from the same review"}
			}
		}
		raw, _ = json.Marshal(ids)
		result, err = q.UpdateAIGroupingReviewCollection(ctx, database.UpdateAIGroupingReviewCollectionParams{ID: collectionID, Name: name, Description: strings.TrimSpace(req.Description), SeriesIds: string(raw), SeriesCount: int64(len(ids))})
		return err
	})
	return result, err
}
func (s *AIGrouping) RejectCollection(ctx context.Context, reviewID, collectionID int64) error {
	return s.store.ExecTx(ctx, func(q *database.Queries) error {
		if _, _, err := pendingAICollection(ctx, q, reviewID, collectionID); err != nil {
			return err
		}
		if err := q.MarkAIGroupingReviewCollectionRejected(ctx, collectionID); err != nil {
			return err
		}
		return finishAIGroupingIfReady(ctx, q, reviewID)
	})
}
func (s *AIGrouping) RejectReview(ctx context.Context, reviewID int64) error {
	return s.store.ExecTx(ctx, func(q *database.Queries) error {
		if _, err := pendingAIGrouping(ctx, q, reviewID); err != nil {
			return err
		}
		if err := q.MarkAIGroupingReviewCollectionsRejected(ctx, reviewID); err != nil {
			return err
		}
		return finalizeAIGroupingReviewStatus(ctx, q, reviewID)
	})
}
func AIGroupingSeriesIDs(raw string) []int64 {
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return []int64{}
	}
	clean := ids[:0]
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	return clean
}

// aiGroupingLiveSeriesIDs 按给定顺序滤掉库里已不存在的系列。查询与写入必须在同一个事务里，
// 否则中间被删掉的系列仍会撞上外键。
func aiGroupingLiveSeriesIDs(ctx context.Context, q *database.Queries, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.GetSeriesNamesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	alive := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		alive[row.ID] = struct{}{}
	}
	live := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := alive[id]; ok {
			live = append(live, id)
		}
	}
	return live, nil
}

func applyAIGroupingReviewCollectionWithQueries(ctx context.Context, q *database.Queries, review database.AiGroupingReview, collection database.AiGroupingReviewCollection) (int64, error) {
	seriesIDs := AIGroupingSeriesIDs(collection.SeriesIds)
	name := strings.TrimSpace(collection.Name)
	if name == "" || len(seriesIDs) == 0 {
		return 0, sql.ErrNoRows
	}
	// 悬空 ID 一律跳过，与读侧口径一致：审核入队之后系列可能被重扫清理掉，而 series_ids
	// 是一串裸 ID、没有外键。照原样插进 collection_series 会撞 series 外键——
	// INSERT OR IGNORE 不抑制外键错误，整个事务回滚，用户只看到一句「服务器错误」。
	seriesIDs, err := aiGroupingLiveSeriesIDs(ctx, q, seriesIDs)
	if err != nil {
		return 0, err
	}
	if len(seriesIDs) == 0 {
		return 0, ErrAIGroupingSeriesGone
	}
	// 候选合集名由 AI 给出，同样受「哪里都不许出现两个同名合集」约束：查重落在事务内的 SQL 上，
	// 因此整批应用时先建的那几条也算数——同一批里两条同名候选，第二条就在这里被挡下。
	// 撞名不是能重试的瞬时状态，用户得先改名或驳回这条候选，因此整批回滚。
	if err := CheckCollectionName(ctx, q, name); err != nil {
		return 0, err
	}
	created, err := q.CreateCollection(ctx, database.CreateCollectionParams{
		Name:           name,
		Description:    description(collection.Description),
		SourceType:     "ai_grouping",
		SourceReviewID: sql.NullInt64{Int64: review.ID, Valid: true},
	})
	if err != nil {
		return 0, err
	}
	for _, seriesID := range seriesIDs {
		if _, err := q.AddSeriesToCollection(ctx, database.AddSeriesToCollectionParams{
			CollectionID: created.ID,
			SeriesID:     seriesID,
		}); err != nil {
			return 0, err
		}
	}
	if err := q.TouchCollection(ctx, created.ID); err != nil {
		return 0, err
	}
	// Collection, members and the terminal-state CAS commit together.
	// A lost CAS must roll back the newly created identity and every member.
	rows, err := q.MarkAIGroupingReviewCollectionApplied(ctx, database.MarkAIGroupingReviewCollectionAppliedParams{
		CreatedCollectionID: sql.NullInt64{Int64: created.ID, Valid: true},
		ID:                  collection.ID,
	})
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		// 刚建的合集与成员随事务整体撤销。
		return 0, ErrAIGroupingNotPending
	}
	return created.ID, nil
}

func finalizeAIGroupingReviewStatus(ctx context.Context, q *database.Queries, reviewID int64) error {
	applied, err := q.CountAppliedAIGroupingReviewCollections(ctx, reviewID)
	if err != nil {
		return err
	}
	status := "rejected"
	if applied > 0 {
		status = "applied"
	}
	_, err = q.UpdateAIGroupingReviewStatus(ctx, database.UpdateAIGroupingReviewStatusParams{
		Status: status,
		ID:     reviewID,
	})
	return err
}
