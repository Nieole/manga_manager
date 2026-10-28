package libraries

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

type CreateInput struct {
	Name                string `json:"name"`
	Path                string `json:"path"`
	ScanMode            string `json:"scan_mode"`
	KOReaderSyncEnabled *bool  `json:"koreader_sync_enabled"`
	ScanInterval        int64  `json:"scan_interval"`
	ScanFormats         string `json:"scan_formats"`
}

type UpdateInput CreateInput

func (s *Service) Update(ctx context.Context, id int64, update UpdateInput) (SaveResult, error) {
	input := normalize(CreateInput(update))
	releaseRoot, err := s.guardRoot(input.Path)
	if err != nil {
		return SaveResult{}, err
	}
	defer releaseRoot()
	release, err := s.engine.AcquireLibraryRead(id)
	if err != nil {
		return SaveResult{}, err
	}
	defer release()
	current, err := s.store.GetLibrary(ctx, id)
	if err != nil {
		return SaveResult{}, err
	}
	protection, err := s.store.LibraryRelocationProtection(ctx, id)
	if err != nil {
		return SaveResult{}, err
	}
	if protection != nil && input.ScanMode != "none" {
		return SaveResult{}, ErrRelocationProtected
	}
	if !samePath(current.Path, input.Path) {
		release()
		release, err = s.engine.AcquireLibraryExclusive(ctx, id)
		if err != nil {
			return SaveResult{}, err
		}
		defer release()
		populated, err := s.store.LibraryHasRecords(ctx, id)
		if err != nil {
			return SaveResult{}, err
		}
		if populated {
			return SaveResult{}, ErrRelocationRequired
		}
	}
	if err := s.validate(ctx, id, input); err != nil {
		return SaveResult{}, err
	}
	enabled := current.KoreaderSyncEnabled
	if input.KOReaderSyncEnabled != nil {
		enabled = *input.KOReaderSyncEnabled
	}
	lib, err := s.store.UpdateLibrary(ctx, database.UpdateLibraryParams{ID: id, Name: input.Name, Path: input.Path, ScanMode: input.ScanMode, KoreaderSyncEnabled: enabled, ScanInterval: input.ScanInterval, ScanFormats: input.ScanFormats})
	if err != nil {
		return SaveResult{}, err
	}
	if s.effects.Watcher != nil {
		s.effects.Watcher.UnwatchLibrary(current.Path)
	}
	return s.saved(lib), nil
}

type InitialScanResult struct {
	Admitted bool   `json:"admitted"`
	RunID    int64  `json:"run_id,omitempty"`
	Status   string `json:"status,omitempty"`
}

// SaveResult separates committed library facts from watcher and scan admission outcomes.
type SaveResult struct {
	database.Library
	Saved            bool               `json:"saved"`
	Watching         string             `json:"watching"`
	InitialScan      *InitialScanResult `json:"initial_scan,omitempty"`
	RefreshRequested bool               `json:"refresh_requested"`
	Warnings         []string           `json:"warnings,omitempty"`
}

type ValidationIssue struct {
	Field string
	Code  string
}
type ValidationError struct{ Issues []ValidationIssue }

func (e *ValidationError) Error() string { return "library validation failed" }

var ErrRelocationRequired = errors.New("library path relocation required")
var ErrRelocationProtected = errors.New("library relocation protection active")

func samePath(a, b string) bool {
	a, b = canonicalPath(a), canonicalPath(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func canonicalPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(absolute)
}

// normalize preserves the configuration defaults for omitted or unsupported scan inputs.
func normalize(input CreateInput) CreateInput {
	if input.ScanInterval <= 0 {
		input.ScanInterval = config.DefaultScanInterval
	}
	input.ScanFormats = config.NormalizeScanFormatsCSV(input.ScanFormats)
	return input
}

func (s *Service) validate(ctx context.Context, id int64, input CreateInput) error {
	issues := []ValidationIssue{}
	add := func(field, code string) {
		issues = append(issues, ValidationIssue{Field: field, Code: "library.validation." + code})
	}
	if strings.TrimSpace(input.Name) == "" {
		add("name", "name_required")
	}
	if strings.TrimSpace(input.Path) == "" {
		add("path", "path_required")
	} else if info, err := os.Stat(input.Path); err != nil {
		add("path", "path_missing")
	} else if !info.IsDir() {
		add("path", "path_not_dir")
	}
	libs, err := s.store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		if lib.ID != id && samePath(lib.Path, input.Path) {
			add("path", "path_in_use")
			break
		}
	}
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func (s *Service) guardRoot(path string) (func(), error) {
	if s.effects.GuardRoot != nil {
		return s.effects.GuardRoot(path)
	}
	return func() {}, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (SaveResult, error) {
	input = normalize(input)
	release, err := s.guardRoot(input.Path)
	if err != nil {
		return SaveResult{}, err
	}
	defer release()
	if err := s.validate(ctx, 0, input); err != nil {
		return SaveResult{}, err
	}
	lib, err := s.store.CreateLibrary(ctx, database.CreateLibraryParams{Name: input.Name, Path: input.Path, ScanMode: input.ScanMode, KoreaderSyncEnabled: input.KOReaderSyncEnabled == nil || *input.KOReaderSyncEnabled, ScanInterval: input.ScanInterval, ScanFormats: input.ScanFormats})
	if err != nil {
		return SaveResult{}, err
	}
	result := s.saved(lib)
	result.InitialScan = &InitialScanResult{}
	if s.effects.StartInitialScan != nil {
		launched, err := s.effects.StartInitialScan(lib)
		if err == nil {
			result.InitialScan.Admitted = true
			result.InitialScan.RunID = launched.Run.ID
			result.InitialScan.Status = string(launched.Run.Status)
		} else {
			result.Warnings = append(result.Warnings, "initial_scan_not_admitted")
		}
	} else {
		result.Warnings = append(result.Warnings, "initial_scan_not_admitted")
	}
	return result, nil
}

func (s *Service) saved(lib database.Library) SaveResult {
	result := SaveResult{Library: lib, Saved: true, Watching: "disabled"}
	if lib.ScanMode == "watch" {
		result.Watching = "failed"
		if s.effects.Watcher != nil && s.effects.Watcher.WatchLibrary(lib.ID, lib.Path, lib.ScanFormats) == nil {
			result.Watching = "active"
		} else {
			result.Warnings = append(result.Warnings, "watcher_start_failed")
		}
	}
	if s.effects.AfterSave != nil {
		s.effects.AfterSave()
		result.RefreshRequested = true
	}
	return result
}
