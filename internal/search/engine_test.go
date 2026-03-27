package search

import (
	"testing"

	"manga-manager/internal/database"
)

func TestEngineRebuildKeepsEngineUsable(t *testing.T) {
	tmpDir := t.TempDir()

	engine, err := NewEngine(tmpDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	book := database.Book{
		ID:   1,
		Name: "Test Book",
	}
	if err := engine.IndexBook(book, "Series A"); err != nil {
		t.Fatalf("IndexBook before rebuild failed: %v", err)
	}

	if err := engine.Rebuild(tmpDir); err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}

	book2 := database.Book{
		ID:   2,
		Name: "Another Book",
	}
	if err := engine.IndexBook(book2, "Series B"); err != nil {
		t.Fatalf("IndexBook after rebuild failed: %v", err)
	}

	result, err := engine.Search("Another", "book", 10)
	if err != nil {
		t.Fatalf("Search after rebuild failed: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("expected 1 hit after rebuild, got %d", result.Total)
	}
}
