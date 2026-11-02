package diskjobs

import (
	"context"
	"errors"
	"fmt"
	"manga-manager/internal/external"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"strconv"
	"strings"
)

// TransferLaunch contains the single prepared plan and optional admitted run.
type TransferLaunch struct {
	Plan external.TransferPlan
	Run  task.Launched
}

// LaunchError identifies failed admission after domain preparation succeeded.
type LaunchError struct{ Err error }

func (e *LaunchError) Error() string { return e.Err.Error() }
func (e *LaunchError) Unwrap() error { return e.Err }

// TransferExternal prepares once, then owns execution and completion publication.
func (c *Service) TransferExternal(ctx context.Context, libraryID int64, sessionID string, seriesIDs []int64) (TransferLaunch, error) {
	plan, err := c.external.PrepareTransfer(ctx, libraryID, sessionID, seriesIDs)
	if err != nil {
		return TransferLaunch{}, err
	}
	result := TransferLaunch{Plan: plan}
	if plan.MissingBooks == 0 {
		return result, nil
	}
	result.Run, err = c.TransferPrepared(ctx, libraryID, sessionID, plan)
	if err != nil {
		return TransferLaunch{}, &LaunchError{err}
	}
	return result, nil
}

// ExternalTransferKey is the diagnostic key shared with transport responses.
func ExternalTransferKey(libraryID int64, sessionID string) string {
	return fmt.Sprintf("transfer_external_library_%s_%d", sessionID, libraryID)
}

// TransferPrepared executes an accepted in-memory plan without replanning or restoring it.
func (c *Service) TransferPrepared(ctx context.Context, libraryID int64, sessionID string, plan external.TransferPlan) (task.Launched, error) {
	total := len(plan.Operations)
	spec := task.RunSpec{
		ValidateAccess: func() error { return c.external.ValidateSession(context.WithoutCancel(ctx), libraryID, sessionID) },
		Key:            ExternalTransferKey(libraryID, sessionID),
		StartCode:      "task.msg.transfer_external_library.start",
		Total:          total,
		CanCancel:      true,
		CanPause:       true,
		Labels: map[string]string{
			"session_id":   sessionID,
			"series_count": strconv.Itoa(plan.SeriesCount),
		},
		CompleteCode: "task.msg.transfer_external_library.complete",
		CancelCode:   "task.msg.transfer_external_library.cancelled",
		FailCode:     "task.msg.transfer_external_library.failed",
	}
	if c.store != nil {
		if lib, err := c.store.GetLibrary(ctx, libraryID); err == nil {
			spec.ScopeName = lib.Name
		}
	}
	spec.Identity = task.Identity{Type: "transfer_external_library", Scope: task.ScopeLibrary, ScopeID: libraryID}
	spec.Trigger = task.TriggerManual
	spec.LibraryIDs = []int64{libraryID}

	paths := make([]string, 0, len(plan.Operations)*2)
	for _, op := range plan.Operations {
		paths = append(paths, op.SourcePath, op.Destination)
	}
	if err := c.cover(ctx, &spec, libraryID, paths); err != nil {
		return task.Launched{}, err
	}

	return c.runs.Start(ctx, spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		defer c.publish()
		if err := ctx.Err(); err != nil {
			return task.Result{}, err
		}
		if err := c.external.ValidateSession(ctx, libraryID, sessionID); err != nil {
			return task.Result{}, err
		}
		if total == 0 {
			return task.Result{Code: "task.msg.transfer_external_library.all_exist"}, nil
		}
		lastCommitted := 0
		result, transferErr := c.external.Transfer(ctx, libraryID, sessionID, plan, handle, func(p external.TransferProgress) {
			done := len(p.Result.CommittedBookIDs) + len(p.Result.ExistingBookIDs)
			handle.Report(runhandle.Frame{
				Current: &done, Total: &total, Phase: "transferring_files", Item: p.Operation.RelativePath,
				Code: "task.msg.transfer_external_library.transferring", Params: map[string]string{"path": p.Operation.RelativePath},
				Metrics: map[string]int64{"transferred_files": int64(len(p.Result.CommittedBookIDs)), "transfer_bytes_staged": p.CopiedBytes, "transfer_current_bytes": p.TotalBytes},
			})
			if len(p.Result.CommittedBookIDs) > lastCommitted {
				handle.MergeParams(map[string]string{"committed_book_ids": transferBookIDs(p.Result.CommittedBookIDs)})
				lastCommitted = len(p.Result.CommittedBookIDs)
			}
		})
		for _, failure := range result.Failures {
			handle.ItemFailed(failure.Operation.RelativePath, failure.Err.Error())
		}
		done := len(result.CommittedBookIDs) + len(result.ExistingBookIDs)
		handle.Report(runhandle.Frame{
			Current: &done, Total: &total,
			Code: "task.msg.transfer_external_library.progress", Params: map[string]string{"done": strconv.Itoa(done), "total": strconv.Itoa(total)},
			Metrics: map[string]int64{"transferred_files": int64(len(result.CommittedBookIDs)), "failed": int64(len(result.Failures)), "transfer_bytes_staged": 0},
		})
		message := task.Result{Params: map[string]string{
			"added":    strconv.Itoa(len(result.CommittedBookIDs)),
			"existing": strconv.Itoa(plan.ExistingBooks + len(result.ExistingBookIDs)),
			"success":  strconv.Itoa(done), "failed": strconv.Itoa(len(result.Failures)),
			"committed_book_ids": transferBookIDs(result.CommittedBookIDs),
		}}
		if errors.Is(transferErr, context.Canceled) {
			message.Code = "task.msg.transfer_external_library.cancelled_with_results"
		} else if len(result.Failures) > 0 {
			message.Code = "task.msg.transfer_external_library.complete_with_failures"
		}
		return message, transferErr
	})
}

func transferBookIDs(ids []int64) string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, strconv.FormatInt(id, 10))
	}
	return strings.Join(values, ",")
}
