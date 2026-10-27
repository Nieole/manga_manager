package proposal

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/database"
)

// transactionBarrier lets a competing SQLite commit finish before the waiting command takes its write lock.
type transactionBarrier struct {
	Database
	reached chan struct{}
	proceed chan struct{}
}

func (d *transactionBarrier) ExecTx(ctx context.Context, fn func(Queries) error) error {
	close(d.reached)
	select {
	case <-d.proceed:
	case <-ctx.Done():
		return ctx.Err()
	}
	return d.Database.ExecTx(ctx, fn)
}

func TestPartialApplyConflictsWhenRejectCommitsBeforeItsTransaction(t *testing.T) {
	svc, store := newTestService(t)
	series := seedTitledSeries(t, store, "Lib", "Alpha", "Manual title")
	review := seedProposal(t, svc, series, partialResult())
	barrier := &transactionBarrier{Database: testDB{Store: store}, reached: make(chan struct{}), proceed: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type outcome struct {
		result ApplyResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := NewService(barrier).Apply(ctx, review.ID, ApplyModeFillEmpty)
		done <- outcome{result, err}
	}()
	select {
	case <-barrier.reached:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	rejected, err := svc.Reject(ctx, review.ID)
	close(barrier.proceed)
	if err != nil || rejected.Status != RejectRejected {
		t.Fatalf("Reject = %+v, %v", rejected, err)
	}
	got := <-done
	if got.err != nil || got.result.Status != ApplyConflict {
		t.Fatalf("waiting Apply = %+v, %v; want conflict", got.result, got.err)
	}
	updated, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Summary.Valid || updated.Publisher.Valid {
		t.Fatalf("rejected proposal changed series: summary=%v publisher=%v", updated.Summary, updated.Publisher)
	}
	provenance, err := store.GetSeriesMetadataProvenance(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(provenance) != 0 {
		t.Fatalf("rejected proposal left provenance: %+v", provenance)
	}
	if got := remainingFieldNames(t, store, review.ID); len(got) != 3 {
		t.Fatalf("rejected fields changed: %v", got)
	}
}

func TestConcurrentPartialApplicationsDoNotReuseConsumedFields(t *testing.T) {
	svc, store := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	series := seedTitledSeries(t, store, "Lib", "Alpha", "Manual title")
	review := seedProposal(t, svc, series, partialResult())
	barrier := &transactionBarrier{Database: testDB{Store: store}, reached: make(chan struct{}), proceed: make(chan struct{})}
	type outcome struct {
		result ApplyResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := NewService(barrier).Apply(ctx, review.ID, ApplyModeFillEmpty)
		done <- outcome{result, err}
	}()
	select {
	case <-barrier.reached:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	first, err := svc.Apply(ctx, review.ID, ApplyModeFillEmpty)
	if err != nil || first.Status != ApplyPartial {
		close(barrier.proceed)
		t.Fatalf("first Apply = %+v, %v", first, err)
	}
	// A later manual edit must not be undone by the other request's stale field list.
	updated, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		close(barrier.proceed)
		t.Fatal(err)
	}
	updated.Publisher = nullString("Manual Publisher")
	updateSeries(t, store, updated)
	close(barrier.proceed)
	second := <-done
	if second.err != nil || second.result.Status != ApplyNoChanges {
		t.Fatalf("second Apply = %+v, %v; want no changes", second.result, second.err)
	}
	after, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Publisher.String != "Manual Publisher" {
		t.Fatalf("reused publisher field: %q", after.Publisher.String)
	}
	if status, _, _ := proposalStatus(t, store, review.ID); status != "pending" {
		t.Fatalf("remaining title lost: %s", status)
	}
	if got := remainingFieldNames(t, store, review.ID); len(got) != 1 || !got["title"] {
		t.Fatalf("remaining fields = %v", got)
	}
	rejected, err := svc.Reject(ctx, review.ID)
	if err != nil || rejected.Status != RejectRejected {
		t.Fatalf("Reject = %+v, %v", rejected, err)
	}
	after, err = store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Summary.String != "External summary" || after.Publisher.String != "Manual Publisher" {
		t.Fatalf("reject undid committed metadata: %+v", after)
	}
}

func TestApplySelectsUsingTransactionCurrentValuesAndLocks(t *testing.T) {
	for _, change := range []string{"filled", "locked"} {
		t.Run(change, func(t *testing.T) {
			svc, store := newTestService(t)
			series := seedTitledSeries(t, store, "Lib", "Alpha", "Manual title")
			review := seedProposal(t, svc, series, partialResult())
			waiting := NewService(&preemptingDB{Database: testDB{Store: store}, preempt: func() {
				latest, err := store.GetSeries(context.Background(), series.ID)
				if err != nil {
					t.Fatal(err)
				}
				if change == "filled" {
					latest.Summary = nullString("Manual summary")
					latest.Publisher = nullString("Manual Publisher")
				} else {
					latest.LockedFields = nullString("summary,publisher")
				}
				updateSeries(t, store, latest)
			}})
			got, err := waiting.Apply(context.Background(), review.ID, ApplyModeFillEmpty)
			want := ApplyNoChanges
			if change == "locked" {
				want = ApplyLockedSkipped
			}
			if err != nil || got.Status != want {
				t.Fatalf("Apply = %+v, %v; want %s", got, err, want)
			}
			latest, err := store.GetSeries(context.Background(), series.ID)
			if err != nil {
				t.Fatal(err)
			}
			if change == "filled" && latest.Summary.String != "Manual summary" {
				t.Fatalf("overwrote current summary: %+v", latest.Summary)
			}
			if change == "locked" && latest.Summary.Valid {
				t.Fatalf("wrote locked summary: %+v", latest.Summary)
			}
			if fields := remainingFieldNames(t, store, review.ID); len(fields) != 3 {
				t.Fatalf("unchanged proposal lost fields: %v", fields)
			}
		})
	}
}

func TestPartialApplyFailureRollsBackMetadataFieldsAndProvenance(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedTitledSeries(t, store, "Lib", "Alpha", "Manual title")
	review := seedProposal(t, svc, series, partialResult())
	sqlStore := store.(*database.SqlStore)
	_, err := sqlStore.DB().ExecContext(ctx, `CREATE TRIGGER fail_field_delete BEFORE DELETE ON metadata_review_fields BEGIN SELECT RAISE(ABORT, 'injected storage failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Apply(ctx, review.ID, ApplyModeFillEmpty); err == nil {
		t.Fatalf("Apply swallowed storage failure: %+v", got)
	}
	latest, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Summary.Valid || latest.Publisher.Valid {
		t.Fatalf("failed partial committed metadata: %+v", latest)
	}
	if fields := remainingFieldNames(t, store, review.ID); len(fields) != 3 {
		t.Fatalf("failed partial removed fields: %v", fields)
	}
	prov, err := store.GetSeriesMetadataProvenance(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(prov) != 0 {
		t.Fatalf("failed partial committed provenance: %+v", prov)
	}
	if status, _, _ := proposalStatus(t, store, review.ID); status != "pending" {
		t.Fatalf("failed partial advanced proposal: %s", status)
	}
}
