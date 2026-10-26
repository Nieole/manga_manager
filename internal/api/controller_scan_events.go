package api

import (
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
)

func (c *Controller) handleScannerBatchEvent(action string)        { c.scans.BatchEvent(action) }
func newTaskScanObserver(h *runhandle.Handle) scanner.ScanObserver { return scanning.NewObserver(h) }
