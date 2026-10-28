package parser

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComicInfoPublicationWaitsForEvictedBorrowAndBlocksNewReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbz")
	writeTestCBZ(t, path, map[string]string{"001.jpg": "page"})
	InitPool(2)
	arc, release, err := GetArchiveFromPool(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	stage, err := PrepareComicInfo(context.Background(), path, ComicInfo{Title: "Published"})
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	EvictArchiveFromPool(path)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := stage.Publish(ctx); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		f, err := OpenContentFile(path)
		if errors.Is(err, ErrArchivePublishing) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		if time.Now().After(deadline) {
			t.Fatal("publication never closed admission")
		}
		time.Sleep(time.Millisecond)
	}
	if _, _, err := GetArchiveFromPool(path); !errors.Is(err, ErrArchivePublishing) {
		t.Fatalf("pool bypassed publication: %v", err)
	}
	if _, err := arc.ReadPage("001.jpg"); err != nil {
		t.Fatalf("evicted live reader interrupted: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("published before reader exits: %v", err)
	default:
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := readArchiveEntries(t, path)["ComicInfo.xml"]; !bytes.Contains([]byte(got), []byte("Published")) {
		t.Fatalf("metadata: %s", got)
	}
}

func TestComicInfoPublicationTimeoutCancelAndSourceChangePreserveOriginal(t *testing.T) {
	for _, kind := range []string{"timeout", "cancel", "changed"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "book.cbz")
			writeTestCBZ(t, path, map[string]string{"001.jpg": "page"})
			stage, err := PrepareComicInfo(context.Background(), path, ComicInfo{Title: "Uncommitted"})
			if err != nil {
				t.Fatal(err)
			}
			defer stage.Close()
			ctx := context.Background()
			var cancel context.CancelFunc
			if kind != "changed" {
				f, err := OpenContentFile(path)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				ctx, cancel = context.WithTimeout(ctx, 15*time.Millisecond)
				defer cancel()
				if kind == "cancel" {
					cancel()
				}
			} else {
				if err := os.WriteFile(path, []byte("external replacement"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := stage.Publish(ctx); err == nil {
				t.Fatal("unsafe publish succeeded")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want, got) {
				t.Fatal("failed publication changed original")
			}
			if err := stage.Close(); err != nil {
				t.Fatal(err)
			}
			staged, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".comicinfo-*.tmp"))
			if len(staged) != 0 {
				t.Fatalf("staging leaked: %v", staged)
			}
		})
	}
}
