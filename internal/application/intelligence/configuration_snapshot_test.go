package intelligence_test

import (
	"context"
	"manga-manager/internal/application"
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
	"path/filepath"
	"testing"
)

func TestGroupingPinsSourceBeforeDeferredRunStarts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Original", Path: filepath.Join(lib.Path, "Series")}); err != nil {
		t.Fatal(err)
	}
	var body func()
	engine := task.New(task.Config{Store: taskstore.New(store.DB()), RunBackground: func(fn func()) { body = fn }})
	original, replacement := &source{}, &source{}
	active := original
	service := intelligence.New(application.NewRuns(engine, nil), store, nil, organize.NewAIGrouping(store, nil), intelligence.Sources{Grouping: func() intelligence.GroupingProvider { return active }}, nil)
	if _, err := service.GroupLibrary(ctx, lib.ID, "en-US", task.TriggerManual); err != nil {
		t.Fatal(err)
	}
	active = replacement
	body()
	if len(original.locales) != 1 || len(replacement.locales) != 0 {
		t.Fatalf("source changed mid-intent: original=%v replacement=%v", original.locales, replacement.locales)
	}
}
