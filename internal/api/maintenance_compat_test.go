package api

import (
	"context"
	"manga-manager/internal/application/maintenance"
	"manga-manager/internal/bookidentity"
	"manga-manager/internal/runhandle"
	"time"
)

func (c *apiFixture) runRebuildFileIdentities(ctx context.Context, limit int, h bookidentity.TaskHandle) (int, int, error) {
	return c.maintenance.RebuildFileIdentityRecords(ctx, limit, h)
}

func (c *apiFixture) runBackfillFullHashesLowPriority(ctx context.Context, limit int, gap time.Duration, h bookidentity.TaskHandle) (int, int, error) {
	return c.maintenance.BackfillFullHashRecords(ctx, limit, gap, h)
}

func reportHashProgress(h *runhandle.Handle, current, total int, code string, io runhandle.IOMetrics) {
	maintenance.ReportHashProgress(h, current, total, code, io)
}

const (
	rebuildBookHashesTaskKey   = "rebuild_book_hashes"
	lowPriorityBookHashTaskKey = "background_book_hash_backfill"
)
