package task

import "context"

// LibraryReservation closes admission while existing readers and runs drain.
// Release must be called on every outcome; Upgrade never opens an admission window.
type LibraryReservation struct {
	engine    *Engine
	libraryID int64
}

// ReserveLibrary closes reads, starts and queued launches using the engine's access boundary.
func (e *Engine) ReserveLibrary(ctx context.Context, libraryID int64) (*LibraryReservation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.stopping {
		return nil, ErrEngineStopped
	}
	if e.libraryExclusive[libraryID] || e.libraryReservations[libraryID] != nil || e.libraryRetired[libraryID] {
		return nil, &LibraryBusyError{Conflicts: []LibraryConflict{{LibraryID: libraryID, Reason: "exclusive_access"}}}
	}
	reservation := &LibraryReservation{engine: e, libraryID: libraryID}
	e.libraryReservations[libraryID] = reservation
	e.libraryVersion++
	return reservation, nil
}

// Upgrade checks actual occupiers and obtains exclusivity under the same engine lock.
func (r *LibraryReservation) Upgrade(ctx context.Context) error {
	e := r.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.libraryReservations[r.libraryID] != r {
		return ErrLibraryUnavailable
	}
	conflicts, err := e.libraryConflictsLocked(ctx, r.libraryID, 0)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return &LibraryBusyError{Conflicts: conflicts}
	}
	e.libraryExclusive[r.libraryID] = true
	return nil
}

// Release is idempotent and wakes queued work after restoring admission.
func (r *LibraryReservation) Release() {
	e := r.engine
	e.mu.Lock()
	if e.libraryReservations[r.libraryID] != r {
		e.mu.Unlock()
		return
	}
	delete(e.libraryReservations, r.libraryID)
	delete(e.libraryExclusive, r.libraryID)
	e.mu.Unlock()
	e.ReleaseQueued()
}

// Retire invalidates a removed stable identity before releasing exclusivity.
// Global work can still discover the remaining libraries after this identity is removed.
func (r *LibraryReservation) Retire() error {
	e := r.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.libraryReservations[r.libraryID] != r || !e.libraryExclusive[r.libraryID] {
		return ErrLibraryUnavailable
	}
	e.libraryRetired[r.libraryID] = true
	return nil
}
