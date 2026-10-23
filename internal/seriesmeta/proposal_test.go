package seriesmeta_test

import (
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/seriesmeta"
)

func TestProposalDistinguishesAbsentAndEmptyCollections(t *testing.T) {
	store, series := fixture(t)
	tag, err := store.UpsertTag(t.Context(), "Old")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkSeriesTag(t.Context(), database.LinkSeriesTagParams{SeriesID: series.ID, TagID: tag.ID}); err != nil {
		t.Fatal(err)
	}
	for _, selected := range []bool{false, true} {
		err := store.ExecTx(t.Context(), func(q *database.Queries) error {
			return seriesmeta.ApplyProposal(t.Context(), q, series, &metadata.SeriesMetadata{}, seriesmeta.ProposalInput{WrittenFields: map[string]bool{"tags": selected}})
		})
		if err != nil {
			t.Fatal(err)
		}
		tags, err := store.GetTagsForSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatal(err)
		}
		if (!selected && len(tags) != 1) || (selected && len(tags) != 0) {
			t.Fatalf("selected=%v tags=%+v", selected, tags)
		}
		provenance, err := store.GetSeriesMetadataProvenance(t.Context(), series.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(provenance) != 0 {
			t.Fatalf("empty collections created provenance: %+v", provenance)
		}
	}
}

func TestProposalPreservesSnapshotValuesAndUnsupportedFields(t *testing.T) {
	store, series := fixture(t)
	series.Title = nullString("Existing")
	series.Summary = nullString("Summary")
	series.Publisher = nullString("Publisher")
	series.Rating = nullFloat(7.2)
	series.Language = nullString("ja")
	series.LockedFields = nullString("authors")
	series = updateSeries(t, store, series)
	input := &metadata.SeriesMetadata{Title: "Beta", Status: "unrecognized", Rating: -1}
	if err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		return seriesmeta.ApplyProposal(t.Context(), q, series, input, seriesmeta.ProposalInput{ProviderName: " bangumi ", SourceURL: " https://example.com ", Confidence: 0.9})
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title.String != "Beta" || got.NameInitial != "B" || got.Status.String != "unknown" || got.Summary.String != "Summary" || got.Publisher.String != "Publisher" || got.Rating.Float64 != 7.2 || got.Language.String != "ja" || got.LockedFields.String != "authors" {
		t.Fatalf("metadata: %+v", got)
	}
	provenance := provenanceByField(t, store, series.ID)
	if len(provenance) != 2 || provenance["status"].Value != "unknown" || provenance["title"].Source != "bangumi" || provenance["title"].SourceUrl != "https://example.com" {
		t.Fatalf("provenance: %+v", provenance)
	}
}
