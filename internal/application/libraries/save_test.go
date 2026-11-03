package libraries_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"manga-manager/internal/application/libraries"
	"manga-manager/internal/database"
	"manga-manager/internal/task"
)

func TestCreateLibraryKeepsCommittedResultWhenInitialScanAdmissionFails(t *testing.T) {
	store, engine, _, watcher := deletionFixture(t)
	watcher.fail = true
	refreshed := false
	service := libraries.New(store, engine, libraries.Effects{Watcher: watcher, StartInitialScan: func(database.Library) (task.Launched, error) {
		return task.Launched{}, errors.New("admission rejected")
	}, AfterSave: func() { refreshed = true }})
	result, err := service.Create(context.Background(), libraries.CreateInput{Name: "New", Path: t.TempDir(), ScanMode: "watch", ScanFormats: "cbz"})
	if err != nil || !result.Saved || result.InitialScan == nil || result.InitialScan.Admitted || result.Watching != "failed" || !refreshed {
		t.Fatalf("outcome=%+v %v", result, err)
	}
	if _, err := store.GetLibrary(context.Background(), result.ID); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryValidationRejectsEquivalentRegisteredRootAcrossCreateAndUpdate(t *testing.T) {
	store, engine, existing, _ := deletionFixture(t)
	service := libraries.New(store, engine, libraries.Effects{})
	t.Chdir(filepath.Dir(existing.Path))
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, existing.Path)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Other", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz"})
	if err != nil {
		t.Fatal(err)
	}
	for _, update := range []bool{false, true} {
		input := libraries.CreateInput{Name: "Conflict", Path: relative, ScanMode: "none", ScanFormats: "cbz"}
		var err error
		if update {
			_, err = service.Update(context.Background(), other.ID, libraries.UpdateInput(input))
		} else {
			_, err = service.Create(context.Background(), input)
		}
		var invalid *libraries.ValidationError
		if !errors.As(err, &invalid) || len(invalid.Issues) != 1 || invalid.Issues[0].Code != "library.validation.path_in_use" {
			t.Fatalf("update=%v: %v", update, err)
		}
	}
}

func TestLibraryValidationReportsMissingAndFilePathsWithoutSaving(t *testing.T) {
	store, engine, _, _ := deletionFixture(t)
	service := libraries.New(store, engine, libraries.Effects{})
	file := filepath.Join(t.TempDir(), "book.cbz")
	if err := os.WriteFile(file, []byte("book"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, code string }{{file, "library.validation.path_not_dir"}, {file + "missing", "library.validation.path_missing"}} {
		result, err := service.Create(context.Background(), libraries.CreateInput{Name: "Invalid", Path: tc.path, ScanFormats: "cbz"})
		var invalid *libraries.ValidationError
		if result.Saved || !errors.As(err, &invalid) || invalid.Issues[0].Code != tc.code {
			t.Fatalf("result=%+v %v", result, err)
		}
	}
}

func TestLibrarySaveRejectsDatabaseFailureBeforeEffectsAndReservedUpdate(t *testing.T) {
	store, engine, lib, watcher := deletionFixture(t)
	refreshed, scanned := false, false
	service := libraries.New(store, engine, libraries.Effects{Watcher: watcher, AfterSave: func() { refreshed = true }, StartInitialScan: func(database.Library) (task.Launched, error) { scanned = true; return task.Launched{}, nil }})
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_new_library BEFORE INSERT ON libraries BEGIN SELECT RAISE(ABORT,'failed'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := service.Create(context.Background(), libraries.CreateInput{Name: "Rejected", Path: t.TempDir(), ScanMode: "watch", ScanFormats: "cbz"})
	if err == nil || result.Saved || refreshed || scanned {
		t.Fatalf("uncommitted save effects: %+v %v", result, err)
	}
	reservation, err := engine.ReserveLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	result, err = service.Update(context.Background(), lib.ID, libraries.UpdateInput{Name: "Blocked", Path: lib.Path, ScanMode: "none", ScanFormats: "cbz"})
	if !errors.Is(err, task.ErrLibraryUnavailable) || result.Saved || !watcher.active {
		t.Fatalf("reserved update=%+v %v", result, err)
	}
	current, err := store.GetLibrary(context.Background(), lib.ID)
	if err != nil || current.Name != lib.Name {
		t.Fatalf("reserved mutation=%+v %v", current, err)
	}
}

func TestUpdateLibraryPreservesIdentityAndRejectsOccupiedPathChange(t *testing.T) {
	store, engine, lib, watcher := deletionFixture(t)
	service := libraries.New(store, engine, libraries.Effects{Watcher: watcher})
	_, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "Existing", Path: lib.Path + "/series"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Update(context.Background(), lib.ID, libraries.UpdateInput{Name: "Moved", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz"})
	if !errors.Is(err, libraries.ErrRelocationRequired) {
		t.Fatalf("path change=%v", err)
	}
	result, err := service.Update(context.Background(), lib.ID, libraries.UpdateInput{Name: "Renamed", Path: lib.Path, ScanMode: "none", ScanFormats: "cbz"})
	if err != nil || !result.Saved || result.ID != lib.ID || result.KoreaderSyncEnabled != lib.KoreaderSyncEnabled || watcher.active {
		t.Fatalf("save=%+v %v", result, err)
	}
}
