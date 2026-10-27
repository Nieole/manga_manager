package organize

import (
	"context"
	"log/slog"
	"time"

	"manga-manager/internal/database"
)

// FranchiseState describes the durable global projection, independently of notification delivery.
type FranchiseState struct {
	DesiredVersion int64  `json:"desired_version"`
	AppliedVersion int64  `json:"applied_version"`
	Available      bool   `json:"available"`
	Stale          bool   `json:"stale"`
	Status         string `json:"status"`
	LastError      string `json:"last_error,omitempty"`
	NextRetryAtMS  int64  `json:"next_retry_at_ms,omitempty"`
}

func (s *Relations) FranchiseState(ctx context.Context) (FranchiseState, error) {
	state, err := s.store.GetFranchiseRebuildState(ctx)
	if err != nil {
		return FranchiseState{}, err
	}
	result := FranchiseState{DesiredVersion: state.DesiredVersion, AppliedVersion: state.AppliedVersion, Available: state.HasSuccess != 0, Stale: state.DesiredVersion > state.AppliedVersion, LastError: state.LastError, NextRetryAtMS: state.NextRetryAtMs, Status: "current"}
	if result.Stale {
		result.Status = "pending"
	}
	if result.Stale && state.LastError != "" {
		result.Status = "failed"
	}
	return result, nil
}

// RepairDue performs at most one pending repair. The application owns its polling and cancellation.
func (s *Relations) RepairDue(ctx context.Context, now time.Time) (bool, error) {
	state, err := s.store.GetFranchiseRebuildState(ctx)
	if err != nil {
		return false, err
	}
	if state.DesiredVersion <= state.AppliedVersion || state.NextRetryAtMs > now.UnixMilli() {
		return false, nil
	}
	return true, s.Rebuild(ctx)
}

func (s *Relations) recordFranchiseFailure(ctx context.Context, attempted database.FranchiseRebuildState, cause error) {
	if attempted.DesiredVersion == 0 {
		return
	}
	// Keep retries bounded while preserving responsibility for every future process start.
	delay := time.Second << min(attempted.Failures, 6)
	if delay > time.Minute {
		delay = time.Minute
	}
	now := time.Now()
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := s.store.RecordFranchiseRebuildFailure(recordCtx, database.RecordFranchiseRebuildFailureParams{Version: attempted.DesiredVersion, AtMs: now.UnixMilli(), RetryAtMs: now.Add(delay).UnixMilli(), Error: cause.Error()})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to record franchise repair failure", "error", err)
	}
}

// RetryFranchiseRepair requests a fresh verification and attempts it without waiting for backoff.
func (s *Relations) RetryFranchiseRepair(ctx context.Context) error {
	if err := s.store.ExecTx(ctx, func(q *database.Queries) error { return q.MarkFranchiseRebuildPending(ctx) }); err != nil {
		return err
	}
	return s.Rebuild(ctx)
}
