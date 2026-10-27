package bookcontent_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"manga-manager/internal/bookcontent"
	"manga-manager/internal/config"
	"manga-manager/internal/parser"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
	_ "modernc.org/sqlite"
)

func TestWholeBookHandleRetainsAccessUntilClose(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(path, []byte("whole book bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	source := bookcontent.Source{ID: 1, LibraryID: 7, Path: path}
	s := bookcontent.New(func(context.Context, int64) (bookcontent.Source, error) { return source, nil }, func() config.Config { return config.Config{} }, nil, 1, 1)
	access := contentAccess(t)
	h, err := s.Open(ctx, 1, access)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if parser.ActiveArchiveBorrows(path) != 1 {
		t.Fatal("whole book is invisible to publication")
	}
	if release, err := access.AcquireLibraryExclusive(ctx, 7); !errors.Is(err, task.ErrLibraryUnavailable) {
		if release != nil {
			release()
		}
		t.Fatalf("active stream ignored: %v", err)
	}
	data, err := io.ReadAll(h)
	if err != nil || string(data) != "whole book bytes" {
		t.Fatalf("stream: %q %v", data, err)
	}
	if parser.ActiveArchiveBorrows(path) != 1 {
		t.Fatal("EOF released before Close")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if parser.ActiveArchiveBorrows(path) != 0 {
		t.Fatal("closed stream retained borrow")
	}
	release, err := access.AcquireLibraryExclusive(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := s.Open(ctx, 1, access); !errors.Is(err, bookcontent.ErrBusy) {
		t.Fatalf("exclusive open: %v", err)
	}
}

func contentAccess(t *testing.T) *task.Engine {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "tasks.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := taskstore.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return task.New(task.Config{Store: taskstore.New(db), RunBackground: func(fn func()) { fn() }})
}

func TestWholeBookOpenFailuresReturnAllAccess(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "book.cbz")
			if kind == "directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			s := bookcontent.New(func(context.Context, int64) (bookcontent.Source, error) {
				return bookcontent.Source{ID: 1, LibraryID: 7, Path: path}, nil
			}, func() config.Config { return config.Config{} }, nil, 1, 1)
			access := contentAccess(t)
			if h, err := s.Open(ctx, 1, access); err == nil {
				_ = h.Close()
				t.Fatal("invalid open succeeded")
			}
			if got := parser.ActiveArchiveBorrows(path); got != 0 {
				t.Fatalf("failed open retained %d borrows", got)
			}
			release, err := access.AcquireLibraryExclusive(context.Background(), 7)
			if err != nil {
				t.Fatal(err)
			}
			release()
		})
	}
}

func TestWholeBookUsesCurrentSourceAfterRelocation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "original.cbz")
	if err := os.WriteFile(path, []byte("original bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	source := bookcontent.Source{ID: 1, LibraryID: 7, Path: path}
	s := bookcontent.New(func(context.Context, int64) (bookcontent.Source, error) { return source, nil }, func() config.Config { return config.Config{} }, nil, 1, 1)
	if _, err := s.Source(ctx, 1); err != nil {
		t.Fatal(err)
	}
	access := contentAccess(t)
	release, err := access.AcquireLibraryExclusive(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	source.Path = filepath.Join(t.TempDir(), "relocated.cbz")
	if err := os.Rename(path, source.Path); err != nil {
		t.Fatal(err)
	}
	release()
	h, err := s.Open(ctx, 1, access)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if h.Source.Path != source.Path {
		t.Fatalf("stale source: %s", h.Source.Path)
	}
	if _, err := h.Seek(9, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(h)
	if err != nil || string(data) != "bytes" {
		t.Fatalf("seekable relocated content: %q %v", data, err)
	}
}
