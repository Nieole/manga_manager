package api

import (
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
)

func newTaskScanObserver(h *runhandle.Handle) scanner.ScanObserver { return scanning.NewObserver(h) }
