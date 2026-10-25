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
		ValidateAccess: func() error { _, err := c.external.GetSession(libraryID, sessionID); return err },
		Key:            ExternalTransferKey(libraryID, sessionID),
		StartCode:      "task.msg.transfer_external_library.start",
		Total:          total,
		CanCancel:      true,
		CanPause:       true,
		Args: map[string]string{
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
		if err := ctx.Err(); err != nil {
			return task.Result{}, err
		}
		if _, err := c.external.GetSession(libraryID, sessionID); err != nil {
			return task.Result{}, err
		}
		if total == 0 {
			c.publish()
			return task.Result{Code: "task.msg.transfer_external_library.all_exist"}, nil
		}

		failures := make([]string, 0)
		skipped := 0
		createdDirs := make(map[string]struct{})
		for index, op := range plan.Operations {
			// 非取消错误一律视为失败：**暂停闸门**今天只返回 nil 或 ctx.Err()，而任务上下文无
			// deadline，因此这与「只认取消」等价。给任务上下文加超时会让这条等价失效。
			if err := handle.Checkpoint(ctx); err != nil {
				return task.Result{}, err
			}
			// 帧报的是**已完成**数，因此在拷贝之前报：单本几百 MB 要拷几分钟，
			// 这段时间里用户要看到的是正在传的那本书，而不是上一本传完时的旧帧。
			done := index
			handle.Report(runhandle.Frame{
				Current: &done,
				Total:   &total,
				Phase:   "transferring_files",
				Item:    op.RelativePath,
				Code:    "task.msg.transfer_external_library.transferring",
				Params:  map[string]string{"path": op.RelativePath},
				Metrics: map[string]int64{"transferred_files": int64(done)},
			})
			skippedCopy, err := external.CopyFile(op.SourcePath, op.Destination, createdDirs)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", op.RelativePath, err))
				continue
			}
			if err := c.external.MarkTransferred(libraryID, sessionID, op); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", op.RelativePath, err))
				continue
			}
			if skippedCopy {
				skipped++
			}
		}
		// 收尾这一帧的计数与指标回答的是两个问题：Current 是「走完了几本」（失败的也走过了），
		// transferred_files 是「传成了几本」。
		transferred := total - len(failures)
		handle.Report(runhandle.Frame{
			Current: &total,
			Total:   &total,
			Code:    "task.msg.transfer_external_library.progress",
			Params:  map[string]string{"done": strconv.Itoa(transferred), "total": strconv.Itoa(total)},
			Metrics: map[string]int64{"transferred_files": int64(transferred)},
		})
		c.publish()

		if len(failures) > 0 {
			return task.Result{
				Code: "task.msg.transfer_external_library.complete_with_failures",
				Params: map[string]string{
					"success": strconv.Itoa(total - len(failures)),
					"failed":  strconv.Itoa(len(failures)),
				},
			}, errors.New(strings.Join(failures, "\n"))
		}
		return task.Result{Params: map[string]string{
			"added":    strconv.Itoa(total),
			"existing": strconv.Itoa(plan.ExistingBooks + skipped),
		}}, nil
	})
}
