package api

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCopyFileToExternalLibraryCopiesWithBufferedStaging(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "source.cbz")
	dst := filepath.Join(dstDir, "nested", "target.cbz")

	content := bytes.Repeat([]byte("manga-manager-buffered-copy"), 4096)
	modTime := time.Now().Add(-2 * time.Hour).Round(time.Second)
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("write source failed: %v", err)
	}
	if err := os.Chtimes(src, modTime, modTime); err != nil {
		t.Fatalf("set source mod time failed: %v", err)
	}

	skipped, err := copyFileToExternalLibrary(src, dst, make(map[string]struct{}))
	if err != nil {
		t.Fatalf("copyFileToExternalLibrary failed: %v", err)
	}
	if skipped {
		t.Fatal("expected copy to execute for missing destination")
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination failed: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("destination content mismatch")
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat destination failed: %v", err)
	}
	if !info.ModTime().Equal(modTime) {
		t.Fatalf("expected mod time %v, got %v", modTime, info.ModTime())
	}
}

func TestCopyFileToExternalLibrarySkipsExistingDestination(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "source.cbz")
	dst := filepath.Join(dstDir, "existing.cbz")

	if err := os.WriteFile(src, []byte("new-content"), 0o644); err != nil {
		t.Fatalf("write source failed: %v", err)
	}
	if err := os.WriteFile(dst, []byte("existing-content"), 0o644); err != nil {
		t.Fatalf("write destination failed: %v", err)
	}

	skipped, err := copyFileToExternalLibrary(src, dst, make(map[string]struct{}))
	if err != nil {
		t.Fatalf("copyFileToExternalLibrary failed: %v", err)
	}
	if !skipped {
		t.Fatal("expected existing destination to be skipped")
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination failed: %v", err)
	}
	if string(got) != "existing-content" {
		t.Fatalf("expected existing destination content to remain unchanged, got %q", string(got))
	}
}
