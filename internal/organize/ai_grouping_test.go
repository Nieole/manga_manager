package organize_test

import (
	"context"
	"errors"
	"fmt"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"testing"
)

func TestAIGroupingApplyCollectionOwnsIdentityAndAdjudication(t *testing.T) {
	store, manual := newCollections(t)
	ctx := context.Background()
	review, candidates := seedGrouping(t, store, "Picks")
	service := organize.NewAIGrouping(store, func() {})
	id, err := service.ApplyCollection(ctx, review.ID, candidates[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := manual.Get(ctx, id)
	if err != nil || got.SourceType != "ai_grouping" || got.SourceReviewID.Int64 != review.ID {
		t.Fatalf("collection=%+v err=%v", got, err)
	}
	state, err := service.Review(ctx, review.ID)
	if err != nil || state.Review.Status != "applied" || state.Collections[0].CreatedCollectionID.Int64 != id {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if err := manual.Update(ctx, id, "Curated", ""); err != nil {
		t.Fatal(err)
	}
}
func seedGrouping(t *testing.T, store database.Store, names ...string) (database.AiGroupingReview, []database.AiGroupingReviewCollection) {
	t.Helper()
	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'Library','lib');INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Alpha','a'),(2,1,'Beta','b')`); err != nil {
		t.Fatal(err)
	}
	review, err := store.CreateAIGroupingReview(ctx, database.CreateAIGroupingReviewParams{LibraryID: 1, Provider: "test", Status: "pending", CollectionCount: int64(len(names)), CandidateCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	candidates := make([]database.AiGroupingReviewCollection, 0, len(names))
	for _, name := range names {
		row, err := store.CreateAIGroupingReviewCollection(ctx, database.CreateAIGroupingReviewCollectionParams{ReviewID: review.ID, Name: name, SeriesIds: "[1,2]", SeriesCount: 2, Status: "pending"})
		if err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, row)
	}
	return review, candidates
}
func TestAIGroupingBatchNameConflictRollsBack(t *testing.T) {
	store, manual := newCollections(t)
	ctx := context.Background()
	review, _ := seedGrouping(t, store, "Same", "same")
	refresh := 0
	service := organize.NewAIGrouping(store, func() { refresh++ })
	if _, err := service.ApplyReview(ctx, review.ID); !errors.Is(err, organize.ErrCollectionNameTaken) {
		t.Fatalf("conflict=%v", err)
	}
	list, err := manual.List(ctx)
	if err != nil || len(list) != 0 || refresh != 0 {
		t.Fatalf("list=%+v refresh=%d err=%v", list, refresh, err)
	}
	state, err := service.Review(ctx, review.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range state.Collections {
		if row.Status != "pending" || row.CreatedCollectionID.Valid {
			t.Fatalf("leaked adjudication=%+v", row)
		}
	}
}
func TestAIGroupingBatchSkipsGoneSeriesAndLeavesThemForDecision(t *testing.T) {
	store, manual := newCollections(t)
	ctx := context.Background()
	review, candidates := seedGrouping(t, store, "Gone", "Alive")
	service := organize.NewAIGrouping(store, func() {})
	for i, id := range []int64{1, 2} {
		if _, err := service.EditCollection(ctx, review.ID, candidates[i].ID, organize.EditAIGroupingCollection{Name: candidates[i].Name, SeriesIDs: []int64{id}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.(*database.SqlStore).DB().Exec(`DELETE FROM series WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyCollection(ctx, review.ID, candidates[0].ID); !errors.Is(err, organize.ErrAIGroupingSeriesGone) {
		t.Fatalf("gone candidate=%v", err)
	}
	result, err := service.ApplyReview(ctx, review.ID)
	if err != nil || result.Applied != 1 || result.Skipped != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	state, err := service.Review(ctx, review.ID)
	if err != nil || state.Review.Status != "pending" || state.Collections[0].Status != "pending" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	list, err := manual.List(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "Alive" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if err := service.RejectReview(ctx, review.ID); err != nil {
		t.Fatal(err)
	}
	state, err = service.Review(ctx, review.ID)
	if err != nil || state.Review.Status != "applied" || state.Collections[0].Status != "rejected" {
		t.Fatalf("mixed state=%+v err=%v", state, err)
	}
}
func TestAIGroupingAllGoneDoesNotPublishOrAdjudicate(t *testing.T) {
	store, manual := newCollections(t)
	ctx := context.Background()
	review, _ := seedGrouping(t, store, "Gone")
	if _, err := store.(*database.SqlStore).DB().Exec(`DELETE FROM series`); err != nil {
		t.Fatal(err)
	}
	refresh := 0
	service := organize.NewAIGrouping(store, func() { refresh++ })
	result, err := service.ApplyReview(ctx, review.ID)
	if !errors.Is(err, organize.ErrAIGroupingSeriesGone) || result.Skipped != 1 || refresh != 0 {
		t.Fatalf("result=%+v refresh=%d err=%v", result, refresh, err)
	}
	list, err := manual.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	state, err := service.Review(ctx, review.ID)
	if err != nil || state.Review.Status != "pending" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}
func TestAIGroupingLateFailureRollsBackIdentityAndDecision(t *testing.T) {
	store, manual := newCollections(t)
	ctx := context.Background()
	review, rows := seedGrouping(t, store, "Picks")
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`CREATE TRIGGER reject_finalization BEFORE UPDATE ON ai_grouping_reviews BEGIN SELECT RAISE(ABORT,'finalization failed');END`); err != nil {
		t.Fatal(err)
	}
	refresh := 0
	service := organize.NewAIGrouping(store, func() { refresh++ })
	id, err := service.ApplyCollection(ctx, review.ID, rows[0].ID)
	if err == nil || id != 0 || refresh != 0 {
		t.Fatalf("id=%d refresh=%d err=%v", id, refresh, err)
	}
	list, err := manual.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	state, err := service.Review(ctx, review.ID)
	if err != nil || state.Collections[0].Status != "pending" || state.Collections[0].CreatedCollectionID.Valid {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_finalization`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyCollection(ctx, review.ID, rows[0].ID); err != nil {
		t.Fatal(err)
	}
	if refresh != 1 {
		t.Fatalf("refresh=%d", refresh)
	}
}
func TestAIGroupingConcurrentSingleAndBatchCreateEachIdentityOnce(t *testing.T) {
	for _, mode := range []string{"single", "batch"} {
		t.Run(mode, func(t *testing.T) {
			store, manual := newCollections(t)
			ctx := context.Background()
			names := []string{"First"}
			if mode == "batch" {
				names = append(names, "Second")
			}
			review, rows := seedGrouping(t, store, names...)
			service := organize.NewAIGrouping(store, func() {})
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() { <-start; _, err := service.ApplyCollection(ctx, review.ID, rows[0].ID); results <- err }()
			go func() {
				<-start
				var err error
				if mode == "batch" {
					_, err = service.ApplyReview(ctx, review.ID)
				} else {
					_, err = service.ApplyCollection(ctx, review.ID, rows[0].ID)
				}
				results <- err
			}()
			close(start)
			success := 0
			for i := 0; i < 2; i++ {
				err := <-results
				if err == nil {
					success++
				} else if !errors.Is(err, organize.ErrAIGroupingNotPending) {
					t.Fatal(err)
				}
			}
			if success < 1 {
				t.Fatal("no successful adjudication")
			}
			if mode == "single" && success != 1 {
				t.Fatalf("same candidate accepted %d times", success)
			}
			list, err := manual.List(ctx)
			if err != nil || len(list) != len(names) {
				t.Fatalf("list=%+v err=%v", list, err)
			}
			state, err := service.Review(ctx, review.ID)
			if err != nil || state.Review.Status != "applied" {
				t.Fatalf("state=%+v err=%v", state, err)
			}
			for _, row := range state.Collections {
				if row.Status != "applied" || !row.CreatedCollectionID.Valid {
					t.Fatalf("candidate=%+v", row)
				}
			}
		})
	}
}
func TestAIGroupingCASLossRollsBackNewCollection(t *testing.T) {
	store, manual := newCollections(t)
	ctx := context.Background()
	review, rows := seedGrouping(t, store, "Picks")
	db := store.(*database.SqlStore).DB()
	trigger := fmt.Sprintf(`CREATE TRIGGER preempt_candidate AFTER INSERT ON collections WHEN NEW.source_type='ai_grouping' BEGIN UPDATE ai_grouping_review_collections SET status='rejected' WHERE id=%d;END`, rows[0].ID)
	if _, err := db.Exec(trigger); err != nil {
		t.Fatal(err)
	}
	service := organize.NewAIGrouping(store, func() { t.Error("failed CAS published refresh") })
	id, err := service.ApplyCollection(ctx, review.ID, rows[0].ID)
	if !errors.Is(err, organize.ErrAIGroupingNotPending) || id != 0 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	list, err := manual.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("leaked collections=%+v err=%v", list, err)
	}
	state, err := service.Review(ctx, review.ID)
	if err != nil || state.Collections[0].Status != "pending" || state.Collections[0].CreatedCollectionID.Valid {
		t.Fatalf("rolled back state=%+v err=%v", state, err)
	}
}
