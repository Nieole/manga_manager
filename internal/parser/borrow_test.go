package parser

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestArchiveBorrowCountIncludesDirectAndEvictedPoolReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "borrow.cbz")
	writeTestCBZ(t, path, map[string]string{"001.png": "page bytes"})
	InitPool(1)
	t.Cleanup(ResetArchivePool)
	pooled, release, err := GetArchiveFromPool(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	direct, err := OpenArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer direct.Close()
	if got := ActiveArchiveBorrows(path); got != 2 {
		t.Fatalf("active borrows = %d, want 2", got)
	}
	if runtime.GOOS == "windows" && ActiveArchiveBorrows(strings.ToUpper(path)) != 2 {
		t.Fatal("case alias hid active Windows archive borrows")
	}
	EvictArchiveFromPool(path)
	if got := ActiveArchiveBorrows(path); got != 2 {
		t.Fatalf("eviction hid active borrows: %d", got)
	}
	if data, err := pooled.ReadPage("001.png"); err != nil || string(data) != "page bytes" {
		t.Fatalf("evicted reader: %q %v", data, err)
	}
	release()
	release()
	if got := ActiveArchiveBorrows(path); got != 1 {
		t.Fatalf("pool release = %d, want 1", got)
	}
	if err := direct.Close(); err != nil {
		t.Fatal(err)
	}
	if got := ActiveArchiveBorrows(path); got != 0 {
		t.Fatalf("direct close left %d active borrows", got)
	}
}

func TestFormatSpecificReadersParticipateInArchiveBorrows(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "direct.cbz")
	writeTestCBZ(t, zipPath, map[string]string{"001.png": "page"})
	for _, tc := range []struct {
		name, path string
		open       func(string) (Archive, error)
	}{
		{"zip", zipPath, OpenZip}, {"rar", filepath.Join("testdata", "vol.cbr"), OpenRar},
	} {
		t.Run(tc.name, func(t *testing.T) {
			arc, err := tc.open(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer arc.Close()
			if got := ActiveArchiveBorrows(tc.path); got != 1 {
				t.Fatalf("direct format opener not tracked: %d", got)
			}
			if _, err := arc.GetPages(); err != nil {
				t.Fatal(err)
			}
			if err := arc.Close(); err != nil {
				t.Fatal(err)
			}
			if got := ActiveArchiveBorrows(tc.path); got != 0 {
				t.Fatalf("closed format opener still borrowed: %d", got)
			}
		})
	}
}

func TestArchiveBorrowCountSurvivesPoolReplacementAndExcludesIdleHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-pool.cbz")
	writeTestCBZ(t, path, map[string]string{"001.png": "page"})
	InitPool(1)
	t.Cleanup(ResetArchivePool)
	_, releaseOld, err := GetArchiveFromPool(path)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOld()
	CloseArchivePool()
	if got := ActiveArchiveBorrows(path); got != 1 {
		t.Fatalf("pool closure hid old borrower: %d", got)
	}
	InitPool(1)
	_, releaseNew, err := GetArchiveFromPool(path)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseNew()
	if got := ActiveArchiveBorrows(path); got != 2 {
		t.Fatalf("replacement lost old borrower: %d", got)
	}
	releaseOld()
	if got := ActiveArchiveBorrows(path); got != 1 {
		t.Fatalf("old release: %d", got)
	}
	releaseNew()
	if got := ActiveArchiveBorrows(path); got != 0 {
		t.Fatalf("idle cached handle counted as borrowed: %d", got)
	}
}
