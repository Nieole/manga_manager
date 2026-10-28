package external

import (
	"context"
	"errors"
	"manga-manager/internal/database"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExternalRootRejectsEveryRegisteredLibrary(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	other := filepath.Join(t.TempDir(), "other")
	child := filepath.Join(other, "child")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Other", Path: other, ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz"}); err != nil {
		t.Fatal(err)
	}
	m := NewManager(store, time.Hour)
	for _, path := range []string{other, child, filepath.Dir(other)} {
		if _, err := m.CreateSession(context.Background(), lib.ID, path, false); !errors.Is(err, ErrExternalPathInsideLibrary) {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestExternalSessionCannotPrepareAfterRootBecomesLibrary(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	root := t.TempDir()
	m := NewManager(store, time.Hour)
	ctx := context.Background()
	session, err := m.CreateSession(ctx, lib.ID, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ScanSession(ctx, session.SessionID, discardScanHandle{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "New", Path: root, ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PrepareTransfer(ctx, lib.ID, session.SessionID, nil); !errors.Is(err, ErrExternalPathInsideLibrary) {
		t.Fatalf("stale session accepted: %v", err)
	}
}

func TestExternalRootCannotResolveInvalidRegisteredPath(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	if _, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Unresolvable", Path: "relative-root", ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(store, time.Hour).CreateSession(context.Background(), lib.ID, t.TempDir(), false); !errors.Is(err, ErrPathUnresolvable) {
		t.Fatalf("unsafe boundary classification: %v", err)
	}
}
