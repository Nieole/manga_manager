package relocation

import (
	"context"
	"errors"

	"manga-manager/internal/runhandle"
)

// CommitOutcome reports durable facts independently of later configuration or effects failures.
type CommitOutcome struct {
	Operation      RelocationOperation
	PathsCommitted bool
	Completed      bool
}

// Execute completes one durable confirmation and observes its persisted boundary even on failure.
func (s *Service) Execute(ctx context.Context, id int64, handle *runhandle.Handle) (CommitOutcome, error) {
	var committed *RelocationOperation
	err := s.commitObserved(ctx, id, handle, func(op RelocationOperation) { committed = &op })
	op, readErr := s.Operation(context.WithoutCancel(ctx), id)
	if readErr != nil && committed != nil {
		op = *committed
		if err == nil {
			op.Status = "completed"
		}
	}
	return CommitOutcome{Operation: op, PathsCommitted: op.Status == "paths_committed" || op.Status == "completed", Completed: op.Status == "completed"}, errors.Join(err, readErr)
}
