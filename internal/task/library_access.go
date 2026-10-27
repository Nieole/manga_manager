package task

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// AllLibraries declares work whose coverage includes libraries discovered after admission.
const AllLibraries int64 = -1

// ErrLibraryUnavailable identifies a library identity whose access is closed.
var ErrLibraryUnavailable = errors.New("library is temporarily unavailable")

// LibraryConflict identifies work that must finish before exclusive access.
type LibraryConflict struct {
	TaskID              int64     `json:"task_id,omitempty"`
	Scope               Scope     `json:"scope,omitempty"`
	ScopeID             int64     `json:"scope_id,omitempty"`
	LibraryIDs          []int64   `json:"library_ids,omitempty"`
	CanCancelForLibrary bool      `json:"can_cancel_for_library"`
	RunID               int64     `json:"run_id,omitempty"`
	LibraryID           int64     `json:"library_id"`
	Status              RunStatus `json:"status,omitempty"`
	CanCancel           bool      `json:"can_cancel"`
	Reason              string    `json:"reason"`
}

// LibraryBusyError describes occupiers without addressing tasks by their keys.
type LibraryBusyError struct{ Conflicts []LibraryConflict }

func (e *LibraryBusyError) Error() string {
	return fmt.Sprintf("%v: %d conflicting accesses", ErrLibraryUnavailable, len(e.Conflicts))
}
func (e *LibraryBusyError) Unwrap() error { return ErrLibraryUnavailable }

// AcquireLibraryRead holds access until the returned, idempotent release is called.
func (e *Engine) AcquireLibraryRead(libraryID int64) (func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.libraryExclusive[libraryID] || e.libraryReservations[libraryID] != nil || e.libraryRetired[libraryID] {
		return nil, ErrLibraryUnavailable
	}
	e.libraryReaders[libraryID]++
	var once sync.Once
	return func() {
		once.Do(func() {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.libraryReaders[libraryID]--
			if e.libraryReaders[libraryID] == 0 {
				delete(e.libraryReaders, libraryID)
			}
		})
	}, nil
}

// AcquireLibraryExclusive checks occupancy and closes admission in one boundary.
// Queued closures count as occupiers because they may retain the old root.
func (e *Engine) AcquireLibraryExclusive(ctx context.Context, libraryID int64) (func(), error) {
	return e.AcquireLibraryExclusiveForRun(ctx, libraryID, 0)
}

// AcquireLibraryExclusiveForRun reserves access for an existing relocation run.
func (e *Engine) AcquireLibraryExclusiveForRun(ctx context.Context, libraryID, ownerRunID int64) (func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.libraryExclusive[libraryID] || e.libraryReservations[libraryID] != nil || e.libraryRetired[libraryID] {
		return nil, ErrLibraryUnavailable
	}
	conflicts, err := e.libraryConflictsLocked(ctx, libraryID, ownerRunID)
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return nil, &LibraryBusyError{Conflicts: conflicts}
	}
	e.libraryVersion++
	e.libraryExclusive[libraryID] = true
	var once sync.Once
	return func() { once.Do(func() { e.mu.Lock(); defer e.mu.Unlock(); delete(e.libraryExclusive, libraryID) }) }, nil
}

// LibraryConflicts lists active and queued work and current reads. excludeRunID is
// the caller's own check run; it never excludes other work from admission.
func (e *Engine) LibraryConflicts(ctx context.Context, libraryID, excludeRunID int64) ([]LibraryConflict, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.libraryConflictsLocked(ctx, libraryID, excludeRunID)
}

func (e *Engine) libraryConflictsLocked(ctx context.Context, libraryID, excludeRunID int64) ([]LibraryConflict, error) {
	conflicts := []LibraryConflict{}
	if e.libraryReaders[libraryID] > 0 {
		conflicts = append(conflicts, LibraryConflict{LibraryID: libraryID, Reason: "wait_for_reads"})
	}
	for runID, ids := range e.libraryRuns {
		if runID == excludeRunID || !containsLibrary(ids, libraryID) {
			continue
		}
		run, err := e.store.LoadRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		canCancel := e.capabilitiesLocked(run).CanCancel
		reason := "wait_for_completion"
		if canCancel {
			reason = "finish_or_cancel_run"
		}
		if run.Status.IsTerminal() {
			reason = "wait_for_exit"
		}
		conflicts = append(conflicts, LibraryConflict{TaskID: run.TaskID, LibraryIDs: append([]int64(nil), ids...), RunID: runID, LibraryID: libraryID, Status: run.Status, CanCancel: canCancel, Reason: reason})
	}
	taskIDs := make([]int64, 0, len(conflicts))
	for _, conflict := range conflicts {
		if conflict.TaskID != 0 {
			taskIDs = append(taskIDs, conflict.TaskID)
		}
	}
	if len(taskIDs) > 0 {
		owners, err := e.store.LoadTasks(ctx, taskIDs)
		if err != nil {
			return nil, err
		}
		for i := range conflicts {
			conflict := &conflicts[i]
			owner, ok := owners[conflict.TaskID]
			if !ok {
				continue
			}
			conflict.Scope, conflict.ScopeID = owner.Identity.Scope, owner.Identity.ScopeID
			local := len(conflict.LibraryIDs) > 0
			for _, id := range conflict.LibraryIDs {
				if id != libraryID {
					local = false
				}
			}
			conflict.CanCancelForLibrary = conflict.CanCancel && local && owner.Identity.Scope == ScopeLibrary && owner.Identity.ScopeID == libraryID
		}
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].RunID < conflicts[j].RunID })
	return conflicts, nil
}

func containsLibrary(ids []int64, id int64) bool {
	for _, candidate := range ids {
		if candidate == id || candidate == AllLibraries {
			return true
		}
	}
	return false
}
func (e *Engine) libraryAdmissionLocked(ids []int64) error {
	for _, id := range ids {
		if e.libraryExclusive[id] || e.libraryReservations[id] != nil || e.libraryRetired[id] || (id == AllLibraries && (len(e.libraryExclusive) > 0 || len(e.libraryReservations) > 0)) {
			return ErrLibraryUnavailable
		}
	}
	return nil
}

// LibraryAccessVersion changes whenever closing access can invalidate a path
// snapshot. A snapshot may be cached only after acquiring a read lease and
// confirming this version is unchanged since before its lookup.
func (e *Engine) LibraryAccessVersion() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.libraryVersion
}
