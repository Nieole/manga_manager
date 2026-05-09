package koreader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintQuickFileChangesWithTailContent(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.bin")
	second := filepath.Join(dir, "second.bin")

	if err := os.WriteFile(first, []byte("abcdef0123456789"), 0o644); err != nil {
		t.Fatalf("write first file failed: %v", err)
	}
	if err := os.WriteFile(second, []byte("abcdef0123456799"), 0o644); err != nil {
		t.Fatalf("write second file failed: %v", err)
	}

	a, err := FingerprintQuickFile(first)
	if err != nil {
		t.Fatalf("FingerprintQuickFile first failed: %v", err)
	}
	b, err := FingerprintQuickFile(second)
	if err != nil {
		t.Fatalf("FingerprintQuickFile second failed: %v", err)
	}
	if a == b {
		t.Fatal("expected different quick fingerprints for different file content")
	}
}
