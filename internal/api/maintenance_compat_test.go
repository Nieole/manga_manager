package api

import (
	"context"
	"manga-manager/internal/application/maintenance"
	"manga-manager/internal/koreader"
	"manga-manager/internal/runhandle"
	"time"
)

func (c *Controller) runRebuildFileIdentities(ctx context.Context, limit int, h koreader.TaskHandle) (int, int, error) {
	return c.maintenance.RebuildFileIdentityRecords(ctx, limit, h)
}

func (c *Controller) runBackfillFullHashesLowPriority(ctx context.Context, limit int, gap time.Duration, h koreader.TaskHandle) (int, int, error) {
	return c.maintenance.BackfillFullHashRecords(ctx, limit, gap, h)
}

func reportHashProgress(h *runhandle.Handle, current, total int, code string, io runhandle.IOMetrics) {
	maintenance.ReportHashProgress(h, current, total, code, io)
}

const (
	rebuildBookHashesTaskKey   = "rebuild_book_hashes"
	lowPriorityBookHashTaskKey = "background_book_hash_backfill"
)
