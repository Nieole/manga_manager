package application

import (
	"context"
	"errors"
	"fmt"
	"manga-manager/internal/task"
)

// ErrRunCoalesced means this launch supplied no body to await.
var ErrRunCoalesced = errors.New("application: run was coalesced")

// ErrRunSuppressed means automatic admission created no run.
var ErrRunSuppressed = errors.New("application: run was suppressed")

// AwaitWatched only reports success after the launch's actual run completes.
// Callers must reschedule coalesced work and must not clean after suppression.
func (r *Runs) AwaitWatched(ctx context.Context, launched task.Launched, launchErr error) error {
	if launchErr != nil {
		return launchErr
	}
	if launched.Stalled != task.StallNone {
		return ErrRunSuppressed
	}
	if launched.Coalesced {
		return ErrRunCoalesced
	}
	return r.AwaitOutcome(ctx, launched.Run.ID)
}

// AwaitOutcome waits by run identity, including queued cancellation without a body.
func (r *Runs) AwaitOutcome(ctx context.Context, runID int64) error {
	run, err := r.engine.Await(ctx, runID)
	if err != nil {
		return err
	}
	switch run.Status {
	case task.StatusCompleted:
		return nil
	case task.StatusCancelled:
		return context.Canceled
	default:
		return fmt.Errorf("run %d ended as %s: %s", runID, run.Status, run.Error)
	}
}
