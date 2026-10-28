package database_test

import (
	"context"
	"database/sql"
	"manga-manager/internal/database"
	"path/filepath"
	"testing"
)

func TestCollectionTransactionCapabilitiesDoNotExposeSQLOrUnrelatedEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collections.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	assertNoSQL := func(value any) {
		t.Helper()
		if _, ok := value.(database.Querier); ok {
			t.Fatal("collection callback exposes all generated queries")
		}
		if _, ok := value.(interface {
			WithTx(*sql.Tx) *database.Queries
		}); ok {
			t.Fatal("collection callback can recover the full sqlc handle")
		}
		if _, ok := value.(interface{ DB() *sql.DB }); ok {
			t.Fatal("collection callback exposes arbitrary SQL")
		}
	}
	if err := store.WithCollectionWrite(context.Background(), func(tx database.CollectionWrite) error {
		assertNoSQL(tx)
		if _, ok := tx.(database.SmartSnapshotWrite); ok {
			t.Fatal("manual collection callback exposes smart shelf queries")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithSmartSnapshot(context.Background(), func(tx database.SmartSnapshotWrite) error {
		assertNoSQL(tx)
		if _, ok := tx.(database.CollectionWrite); ok {
			t.Fatal("snapshot callback exposes unrelated manual updates and deletes")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithAIGroupingWrite(context.Background(), func(tx database.AIGroupingWrite) error {
		assertNoSQL(tx)
		if _, ok := tx.(database.CollectionWrite); ok {
			t.Fatal("AI adjudication exposes manual collection deletion")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

}
