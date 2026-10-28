package task

import (
	"context"
	"errors"
	"time"
)

// LibraryDrainResult records control requests separately from observed access exits.
type LibraryDrainResult struct {
	CancellationRequested []int64           `json:"cancellation_requested,omitempty"`
	CancellationFailed    []int64           `json:"cancellation_failed,omitempty"`
	ExitedRuns            []int64           `json:"exited_runs,omitempty"`
	Conflicts             []LibraryConflict `json:"conflicts,omitempty"`
}

// Drain requests cancellation outside the access lock and waits for actual access
// claims to leave. The caller supplies a bounded context and must release the reservation.
func (r *LibraryReservation) Drain(ctx context.Context) (LibraryDrainResult, error) {
	result := LibraryDrainResult{}
	conflicts, err := r.drainConflicts(ctx)
	if err != nil {
		return result, err
	}
	result.Conflicts = conflicts
	for _, conflict := range conflicts {
		if err := ctx.Err(); err != nil {
			result.Conflicts = conflicts
			return result, err
		}
		if !conflict.CanCancelForLibrary {
			continue
		}
		if err := r.engine.Cancel(conflict.RunID); err == nil {
			result.CancellationRequested = append(result.CancellationRequested, conflict.RunID)
		} else if !errors.Is(err, ErrRunNotRunning) {
			result.CancellationFailed = append(result.CancellationFailed, conflict.RunID)
		}
	}
	for {
		err := r.Upgrade(ctx)
		var busy *LibraryBusyError
		if err == nil || errors.As(err, &busy) {
			result.Conflicts = nil
			if busy != nil {
				result.Conflicts = busy.Conflicts
			}
			result.ExitedRuns = nil
			for _, id := range result.CancellationRequested {
				occupied := false
				for _, conflict := range result.Conflicts {
					if conflict.RunID == id {
						occupied = true
						break
					}
				}
				if !occupied {
					result.ExitedRuns = append(result.ExitedRuns, id)
				}
			}
		}
		if err == nil || busy == nil {
			return result, err
		}
		// Recheck the predicate under the engine lock on every wake; no notification
		// subscription can miss the last access exit between checking and waiting.
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *LibraryReservation) drainConflicts(ctx context.Context) ([]LibraryConflict, error) {
	e := r.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.libraryReservations[r.libraryID] != r {
		return nil, ErrLibraryUnavailable
	}
	return e.libraryConflictsLocked(ctx, r.libraryID, 0)
}
