package task

import "context"

// AwaitBodyExit joins an already admitted callback, including the background handoff gap.
// Queued work has no callback to join. Its input owner must separately withdraw or
// guard future execution; terminal run snapshots alone never establish body exit.
func (e *Engine) AwaitBodyExit(ctx context.Context, runID int64) error {
	e.mu.Lock()
	exited := e.bodyExits[runID]
	e.mu.Unlock()
	if exited == nil {
		return nil
	}
	select {
	case <-exited:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) releaseBodyExit(runID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if exited := e.bodyExits[runID]; exited != nil {
		delete(e.bodyExits, runID)
		close(exited)
	}
}
