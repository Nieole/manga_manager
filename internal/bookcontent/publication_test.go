package bookcontent_test

import (
	"context"
	"errors"
	"io"
	"manga-manager/internal/config"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/bookcontent"
	"manga-manager/internal/parser"
)

func TestPublicationRetainsCommittedFactWhenFactStoreFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbz")
	archive(t, path, 20)
	invalidated := false
	publisher := bookcontent.NewPublisher(func(context.Context, bookcontent.Source) error { return errors.New("fact store unavailable") }, func() { invalidated = true })
	result, err := publisher.Publish(context.Background(), bookcontent.Source{ID: 1, Path: path}, parser.ComicInfo{Title: "Committed"})
	if err != nil || !result.Committed || result.Warning == nil || !invalidated {
		t.Fatalf("publication=%+v err=%v invalidated=%v", result, err, invalidated)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Source.Size != stat.Size() || !result.Source.FileModifiedAt.Equal(stat.ModTime()) {
		t.Fatal("committed facts mismatch actual file")
	}
	arc, err := parser.OpenArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer arc.Close()
	data, err := arc.ReadMetadataFile("ComicInfo.xml")
	if err != nil {
		t.Fatal(err)
	}
	info, err := parser.ParseComicInfo(data)
	if err != nil || info.Title != "Committed" {
		t.Fatalf("committed metadata=%+v %v", info, err)
	}
}

func TestPublicationWaitsForWholeBookHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbz")
	archive(t, path, 20)
	source := bookcontent.Source{ID: 1, LibraryID: 7, Path: path}
	s := bookcontent.New(func(context.Context, int64) (bookcontent.Source, error) { return source, nil }, func() config.Config { return config.Config{} }, nil, 1, 1)
	access := contentAccess(t)
	h, err := s.Open(context.Background(), 1, access)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := bookcontent.NewPublisher(nil, s.PurgeSources).Publish(ctx, source, parser.ComicInfo{Title: "After download"})
		done <- err
	}()
	for {
		other, err := s.Open(ctx, 1, access)
		if errors.Is(err, bookcontent.ErrBusy) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		_ = other.Close()
		time.Sleep(time.Millisecond)
	}
	if data, err := io.ReadAll(h); err != nil || len(data) == 0 {
		t.Fatalf("existing download truncated: %v", err)
	}
	_ = h.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
