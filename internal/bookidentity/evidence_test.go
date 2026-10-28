package bookidentity_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"manga-manager/internal/bookidentity"
)

func TestFileEvidenceRetainsWholeByteMD5AndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbz")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	hash, err := bookidentity.FingerprintFileContext(context.Background(), path)
	if err != nil || hash != bookidentity.ContentHash("5d41402abc4b2a76b9719d911017c592") {
		t.Fatalf("content evidence=%q %v", hash, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bookidentity.FingerprintFileContext(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestQuickAndPathEvidenceKeepCompatibleByteConventions(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "Series", "Vol01.cbz")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	quick, err := bookidentity.FingerprintQuickFile(file)
	if err != nil || quick != "67e527aa7412a3b5c5f82b87ede9cea5a3754d0e" {
		t.Fatalf("sample evidence=%q %v", quick, err)
	}
	if got := bookidentity.FingerprintRelativePath(root, file, false); got != "f32485ab299f134b6dddc113de28065f" {
		t.Fatalf("path evidence=%q", got)
	}
	if got := bookidentity.FingerprintRelativePath(root, file, true); got != "ddec5777174a035999fc8703bc39d93d" {
		t.Fatalf("path without extension=%q", got)
	}
}
