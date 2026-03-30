package api

import (
	"context"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

func TestApplyMetadataToSeriesHonorsLocksAndCreatesTagsAndLinks(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	db := controller.store.(*database.SqlStore).DB()
	if _, err := db.ExecContext(context.Background(), `
		UPDATE series
		SET title = ?, summary = ?, publisher = ?, rating = ?, locked_fields = ?
		WHERE id = ?
	`, "Locked Title", "Old summary", "Old publisher", 7.2, "title,publisher", series.ID); err != nil {
		t.Fatalf("seed locked series metadata failed: %v", err)
	}

	series, err := controller.store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries failed: %v", err)
	}

	input := &metadata.SeriesMetadata{
		Title:     "New Title",
		Summary:   "New summary",
		Publisher: "New publisher",
		Rating:    8.8,
		Tags:      []string{"Action", "Mystery", "Action", " "},
		SourceID:  12345,
	}

	if err := controller.applyMetadataToSeries(context.Background(), series, input, "bangumi"); err != nil {
		t.Fatalf("applyMetadataToSeries failed: %v", err)
	}

	updated, err := controller.store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries after update failed: %v", err)
	}

	if !updated.Title.Valid || updated.Title.String != "Locked Title" {
		t.Fatalf("expected title lock preserved, got %+v", updated.Title)
	}
	if !updated.Publisher.Valid || updated.Publisher.String != "Old publisher" {
		t.Fatalf("expected publisher lock preserved, got %+v", updated.Publisher)
	}
	if !updated.Summary.Valid || updated.Summary.String != "New summary" {
		t.Fatalf("expected summary updated, got %+v", updated.Summary)
	}
	if !updated.Rating.Valid || updated.Rating.Float64 != 8.8 {
		t.Fatalf("expected rating updated, got %+v", updated.Rating)
	}

	tags, err := controller.store.GetTagsForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetTagsForSeries failed: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 deduplicated tags, got %d", len(tags))
	}

	links, err := controller.store.GetLinksForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetLinksForSeries failed: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 source link, got %d", len(links))
	}
	if links[0].Name != "Bangumi" || links[0].Url != "https://bgm.tv/subject/12345" {
		t.Fatalf("unexpected source link: %+v", links[0])
	}

	if err := controller.applyMetadataToSeries(context.Background(), updated, input, "bangumi"); err != nil {
		t.Fatalf("second applyMetadataToSeries failed: %v", err)
	}

	links, err = controller.store.GetLinksForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetLinksForSeries second pass failed: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected link deduplication, got %d links", len(links))
	}
}
