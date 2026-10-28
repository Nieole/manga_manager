package external

import (
	"context"
	"errors"
	"fmt"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/taskcontrol"
)

// TransferResult preserves physical commits even when later copying or projection fails.
type TransferResult struct {
	CommittedBookIDs []int64
	ExistingBookIDs  []int64
	Failures         []TransferFailure
}

// TransferFailure identifies an operation that did not finish cleanly.
type TransferFailure struct {
	Operation TransferOperation
	Err       error
}

// TransferProgress describes bytes staged for the current book and completed books.
// CopiedBytes never implies that the current destination has been published.
type TransferProgress struct {
	Operation   TransferOperation
	CopiedBytes int64
	TotalBytes  int64
	Result      TransferResult
}

// TransferWork supplies short IO leases for actual reads and writes. Implementations
// must release a lease before waiting for pause or acquiring a lease for another path.
type TransferWork interface {
	Disk(context.Context, diskwork.Work, func() error) error
}

// Transfer owns staging, pause/cancellation, publication and session projection for
// one prepared plan. Blocks release IO leases before waiting for pause; resume
// rechecks file facts and destination eligibility before continuing the same copy.
// Control cannot interrupt an OS call already blocked in a filesystem driver.
func (m *Manager) Transfer(ctx context.Context, libraryID int64, sessionID string, plan TransferPlan, work TransferWork, progress func(TransferProgress)) (TransferResult, error) {
	var result TransferResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := m.ValidateSession(ctx, libraryID, sessionID); err != nil {
		return result, err
	}
	createdDirs := make(map[string]struct{})
	for _, op := range plan.Operations {
		if err := taskcontrol.Wait(ctx); err != nil {
			return result, err
		}
		if err := m.ValidateOperation(ctx, libraryID, sessionID, op); err != nil {
			return result, err
		}
		notify := func(copied, total int64) {
			if progress != nil {
				progress(TransferProgress{Operation: op, CopiedBytes: copied, TotalBytes: total, Result: cloneTransferResult(result)})
			}
		}
		notify(0, 0)
		existing, err := copyFile(ctx, op.SourcePath, op.Destination, createdDirs, work, notify, func() error {
			return m.ValidateOperation(ctx, libraryID, sessionID, op)
		})
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return result, err
		}
		if err == nil {
			if existing {
				result.ExistingBookIDs = append(result.ExistingBookIDs, op.BookID)
			} else {
				result.CommittedBookIDs = append(result.CommittedBookIDs, op.BookID)
			}
			err = m.MarkTransferred(libraryID, sessionID, op)
		}
		if err != nil {
			result.Failures = append(result.Failures, TransferFailure{Operation: op, Err: err})
		}
		notify(0, 0)
		if err := ctx.Err(); err != nil {
			return result, err
		}
	}
	errs := make([]error, 0, len(result.Failures))
	for _, failure := range result.Failures {
		errs = append(errs, fmt.Errorf("%s: %w", failure.Operation.RelativePath, failure.Err))
	}
	return result, errors.Join(errs...)
}

func cloneTransferResult(result TransferResult) TransferResult {
	return TransferResult{
		CommittedBookIDs: append([]int64(nil), result.CommittedBookIDs...),
		ExistingBookIDs:  append([]int64(nil), result.ExistingBookIDs...),
		Failures:         append([]TransferFailure(nil), result.Failures...),
	}
}
