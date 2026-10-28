package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"manga-manager/internal/runhandle"
)

func TestLibraryDrainCancelsOnlyEligibleIdentityAndActualCoverage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		scope    Scope
		scopeID  int64
		ids      []int64
		cancel   bool
		noCancel bool
	}{
		{"library", ScopeLibrary, 1, []int64{1}, true, false},
		{"series", ScopeSeries, 17, []int64{1}, true, false},
		{"series-shared", ScopeSeries, 17, []int64{1, 2}, false, false},
		{"library-mismatch", ScopeLibrary, 2, []int64{1}, false, false},
		{"system", ScopeSystem, 0, []int64{1}, false, false},
		{"global", ScopeSystem, 0, []int64{AllLibraries}, false, false},
		{"uncancellable", ScopeLibrary, 1, []int64{1}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestEngine(t, func(fn func()) { go fn() }, 2)
			entered, exit, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			spec := coveredLibrarySpec(1)
			spec.Identity.Scope = tc.scope
			spec.Identity.ScopeID = tc.scopeID
			spec.LibraryIDs = tc.ids
			spec.CanCancel = !tc.noCancel
			run := h.start(t, spec, func(ctx context.Context, _ *runhandle.Handle) (Result, error) {
				close(entered)
				<-exit
				close(done)
				return Result{}, ctx.Err()
			})
			defer func() { close(exit); <-done }()
			<-entered
			r, err := h.engine.ReserveLibrary(context.Background(), 1)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Release()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			result, err := r.Drain(ctx)
			if !errors.Is(err, context.DeadlineExceeded) || len(result.Conflicts) != 1 || len(result.ExitedRuns) != 0 {
				t.Fatalf("drain=%+v %v", result, err)
			}
			if (len(result.CancellationRequested) == 1) != tc.cancel {
				t.Fatalf("wrong cancellation: %+v", result)
			}
			status := h.load(t, run.ID).Status
			if tc.cancel && status != StatusCancelling || !tc.cancel && status != StatusRunning {
				t.Fatalf("status=%s", status)
			}
		})
	}
}

func TestReleasedLibraryReservationCannotCancelNewWork(t *testing.T) {
	h := newTestEngine(t, registerOnly, 2)
	r, err := h.engine.ReserveLibrary(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	r.Release()
	run := h.start(t, coveredLibrarySpec(1), idleBody)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := r.Drain(ctx)
	if !errors.Is(err, ErrLibraryUnavailable) || len(result.CancellationRequested) > 0 || h.load(t, run.ID).Status != StatusRunning {
		t.Fatalf("released reservation controlled new work: %+v %v", result, err)
	}
}
