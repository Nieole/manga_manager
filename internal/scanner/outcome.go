package scanner

import (
	"context"
	"errors"
)

// ScanStatus describes completed ingestion work, never run admission or cover completion.
type ScanStatus string

const (
	ScanComplete  ScanStatus = "complete"
	ScanPartial   ScanStatus = "partial"
	ScanFailed    ScanStatus = "failed"
	ScanCancelled ScanStatus = "cancelled"
)

// ErrScanIncomplete prevents a partially ingested rename from qualifying destructive cleanup.
var ErrScanIncomplete = errors.New("scanner: ingestion did not complete safely")

// ErrCleanupDeferred means a queued cleanup no longer covers the latest delivered changes.
var ErrCleanupDeferred = errors.New("scanner: cleanup awaits newer ingestion")

// ScanOutcome is produced after discovery, interpretation and committed writes have drained.
// The zero value supplies no evidence that cleanup is safe.
type ScanOutcome struct {
	Status           ScanStatus
	FailedArchives   int64
	UncertainEntries int64
}

// CleanupEligible requires a complete, unambiguous ingestion of the observed filesystem.
func (o ScanOutcome) CleanupEligible() bool { return o.Status == ScanComplete }

// Err exposes incomplete ingestion to run consumers without interpreting logs or metrics.
func (o ScanOutcome) Err() error {
	if o.CleanupEligible() {
		return nil
	}
	return ErrScanIncomplete
}

func scanOutcome(metrics *scanMetrics, err error) ScanOutcome {
	outcome := ScanOutcome{Status: ScanComplete, FailedArchives: metrics.failedArchives.Load(), UncertainEntries: metrics.uncertainEntries.Load()}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		outcome.Status = ScanCancelled
	case err != nil:
		outcome.Status = ScanFailed
	case outcome.FailedArchives > 0 || outcome.UncertainEntries > 0:
		outcome.Status = ScanPartial
	}
	return outcome
}
