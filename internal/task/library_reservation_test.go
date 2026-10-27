package task

import (
	"context"
	"errors"
	"manga-manager/internal/runhandle"
	"testing"
)

func coveredLibrarySpec(id int64) RunSpec {
	spec := libraryScanSpec(id)
	spec.LibraryIDs = []int64{id}
	return spec
}

func TestLibraryReservationBlocksQueueAndReleasesOtherLibraries(t *testing.T) {
	runner := &deferredRunner{}
	h := newTestEngine(t, runner.run, 1)
	h.start(t, coveredLibrarySpec(2), idleBody)
	held := h.start(t, coveredLibrarySpec(1), idleBody)
	other := h.start(t, coveredLibrarySpec(3), idleBody)
	reservation, err := h.engine.ReserveLibrary(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	if _, err := h.engine.AcquireLibraryRead(1); !errors.Is(err, ErrLibraryUnavailable) {
		t.Fatalf("reserved read=%v", err)
	}
	if _, err := h.engine.Start(context.Background(), coveredLibrarySpec(1), idleBody); !errors.Is(err, ErrLibraryUnavailable) {
		t.Fatalf("reserved start=%v", err)
	}
	global := RunSpec{Identity: Identity{Type: "global", Scope: ScopeSystem}, LibraryIDs: []int64{AllLibraries}, Trigger: TriggerManual}
	if _, err := h.engine.Start(context.Background(), global, idleBody); !errors.Is(err, ErrLibraryUnavailable) {
		t.Fatalf("all-libraries start=%v", err)
	}
	if _, err := h.engine.AcquireLibraryExclusive(context.Background(), 1); !errors.Is(err, ErrLibraryUnavailable) {
		t.Fatalf("migration crossed reservation: %v", err)
	}
	runner.drain()
	if h.load(t, held.ID).Status != StatusQueued || h.load(t, other.ID).Status != StatusCompleted {
		t.Fatal("reservation either started blocked queue or blocked another library")
	}
	var busy *LibraryBusyError
	if err := reservation.Upgrade(context.Background()); !errors.As(err, &busy) || len(busy.Conflicts) != 1 || busy.Conflicts[0].RunID != held.ID {
		t.Fatalf("queued occupier=%+v %v", busy, err)
	}
	reservation.Release()
	runner.drain()
	if h.load(t, held.ID).Status != StatusCompleted {
		t.Fatal("released reservation did not wake queue")
	}
	reservation.Release()
}

func TestLibraryReservationWaitsForBodyExitDespiteTerminalSnapshot(t *testing.T) {
	h := newTestEngine(t, registerOnly, 2)
	run := h.start(t, coveredLibrarySpec(1), idleBody)
	if _, err := h.engine.MarkInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	reservation, err := h.engine.ReserveLibrary(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	var busy *LibraryBusyError
	if err := reservation.Upgrade(context.Background()); !errors.As(err, &busy) || len(busy.Conflicts) != 1 || busy.Conflicts[0].RunID != run.ID {
		t.Fatalf("terminal snapshot released a live body: %+v %v", busy, err)
	}
}

func TestLibraryReservationUpgradeNeverReopensReadAdmission(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 2)
	oldRead, err := h.engine.AcquireLibraryRead(1)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := h.engine.ReserveLibrary(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	if err := reservation.Upgrade(context.Background()); !errors.Is(err, ErrLibraryUnavailable) {
		t.Fatalf("live reader was ignored: %v", err)
	}
	oldRead()
	checks := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func() {
			release, err := h.engine.AcquireLibraryRead(1)
			if release != nil {
				release()
			}
			checks <- err
		}()
	}
	if err := reservation.Upgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		if err := <-checks; !errors.Is(err, ErrLibraryUnavailable) {
			t.Fatalf("reader crossed atomic upgrade: %v", err)
		}
	}
	reservation.Release()
	lease, err := h.engine.AcquireLibraryExclusive(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.ReserveLibrary(context.Background(), 1); !errors.Is(err, ErrLibraryUnavailable) {
		t.Fatalf("reservation crossed migration: %v", err)
	}
	lease()
}

func TestLibraryConflictsUseIdentityAndCoverageForCancellationEligibility(t *testing.T) {
	h := newTestEngine(t, registerOnly, 4)
	local := h.start(t, coveredLibrarySpec(1), idleBody)
	shared := coveredLibrarySpec(1)
	shared.Identity.Type = "shared"
	shared.LibraryIDs = []int64{1, 2}
	sharedRun := h.start(t, shared, idleBody)
	global := RunSpec{Identity: Identity{Type: "global", Scope: ScopeSystem}, LibraryIDs: []int64{AllLibraries}, Trigger: TriggerManual, CanCancel: true, Key: "scan_library_1"}
	system := h.start(t, global, idleBody)
	conflicts, err := h.engine.LibraryConflicts(context.Background(), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 3 {
		t.Fatalf("coverage conflicts=%+v", conflicts)
	}
	for _, conflict := range conflicts {
		if conflict.TaskID == 0 || len(conflict.LibraryIDs) == 0 {
			t.Fatalf("identity/coverage missing: %+v", conflict)
		}
		if conflict.CanCancelForLibrary != (conflict.RunID == local.ID) {
			t.Fatalf("unsafe cancellation eligibility: %+v", conflict)
		}
		if conflict.RunID == sharedRun.ID && len(conflict.LibraryIDs) != 2 {
			t.Fatalf("shared coverage lost: %+v", conflict)
		}
		if conflict.RunID == system.ID && conflict.Scope != ScopeSystem {
			t.Fatalf("task key overrode scope: %+v", conflict)
		}
	}
}

func TestLibraryReservationBecomesAvailableOnlyAfterInterruptedBodyExits(t *testing.T) {
	store := newMemStore()
	entered, leave, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	engine := New(Config{Store: store, RunBackground: func(fn func()) { go func() { fn(); close(exited) }() }})
	_, err := engine.Start(context.Background(), coveredLibrarySpec(1), func(context.Context, *runhandle.Handle) (Result, error) {
		close(entered)
		<-leave
		return Result{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := engine.MarkInterrupted(context.Background()); err != nil {
		close(leave)
		t.Fatal(err)
	}
	reservation, err := engine.ReserveLibrary(context.Background(), 1)
	if err != nil {
		close(leave)
		t.Fatal(err)
	}
	defer reservation.Release()
	if err := reservation.Upgrade(context.Background()); !errors.Is(err, ErrLibraryUnavailable) {
		close(leave)
		t.Fatalf("interrupted body still owns access: %v", err)
	}
	close(leave)
	<-exited
	if err := reservation.Upgrade(context.Background()); err != nil {
		t.Fatalf("exited body kept access: %v", err)
	}
	if err := reservation.Retire(); err != nil {
		t.Fatal(err)
	}
	reservation.Release()
	if _, err := engine.Start(context.Background(), coveredLibrarySpec(1), idleBody); !errors.Is(err, ErrLibraryUnavailable) {
		t.Fatalf("retired ID started: %v", err)
	}
}
