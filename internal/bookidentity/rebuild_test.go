package bookidentity_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/bookidentity"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
)

type workHandle struct{ reads int }

func (h *workHandle) Advance(int, int)                     {}
func (h *workHandle) Checkpoint(ctx context.Context) error { return ctx.Err() }
func (h *workHandle) Disk(_ context.Context, _ diskwork.Work, fn func() error) error {
	h.reads++
	return fn()
}

func TestRebuildUsesEvidenceWithoutChangingBookIdentityAndSkipsUnreadableTail(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "books.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Series", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, name := range []string{"good.cbz", "missing.cbz"} {
		path := filepath.Join(root, name)
		if name == "good.cbz" {
			if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		book, err := store.CreateBook(ctx, database.CreateBookParams{SeriesID: series.ID, LibraryID: lib.ID, Name: name, Path: path, Size: 5, FileModifiedAt: time.Unix(100, 0)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, book.ID)
	}
	handle := &workHandle{}
	updated, total, err := bookidentity.New(store).Rebuild(ctx, bookidentity.RebuildOptions{BatchSize: 1, MatchMode: bookidentity.ContentMode}, handle)
	if err != nil || updated != 1 || total != 2 || handle.reads != 2 {
		t.Fatalf("rebuild=%d/%d reads%d %v", updated, total, handle.reads, err)
	}
	good, err := store.GetBook(ctx, ids[0])
	if err != nil || good.ID != ids[0] || good.FileHash.String != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatalf("good=%+v %v", good, err)
	}
	missing, err := store.GetBook(ctx, ids[1])
	if err != nil || missing.FileHash.Valid {
		t.Fatalf("missing=%+v %v", missing, err)
	}
}
