package external

import (
	"bytes"
	"context"
	"errors"
	"manga-manager/internal/database"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	ids := seedTransferSeries(t, store, lib, 3)
	m := NewManager(store, time.Hour)
	session := newReadyTransferSession(t, m, store, lib)
	plan, err := m.PrepareTransfer(context.Background(), lib.ID, session, ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range plan.Operations {
		if err := os.WriteFile(op.SourcePath, bytes.Repeat([]byte("book"), 1<<20), 0644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var staged []int64
	result, err := m.Transfer(ctx, lib.ID, session, plan, func(p TransferProgress) {
		if p.Operation.BookID == plan.Operations[1].BookID && p.CopiedBytes > 0 {
			staged = append(staged, p.CopiedBytes)
			if p.CopiedBytes >= p.TotalBytes {
				t.Error("cancellation was not observable until the whole book was copied")
			}
			cancel()
		}
	})
	if len(staged) != 1 {
		t.Fatalf("copying continued after cancellation: %v", staged)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if len(result.CommittedBookIDs) != 1 || result.CommittedBookIDs[0] != plan.Operations[0].BookID {
		t.Fatalf("committed = %+v", result)
	}
	if _, err := os.Stat(plan.Operations[0].Destination); err != nil {
		t.Fatal(err)
	}
	for _, op := range plan.Operations[1:] {
		if _, err := os.Stat(op.Destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("uncommitted destination: %v", err)
		}
	}
	snapshot, err := m.GetSession(lib.ID, session)
	if err != nil || snapshot.MatchedBooks != 1 {
		t.Fatalf("coverage = %+v, %v", snapshot, err)
	}
	root, err := m.GetSession(lib.ID, session)
	if err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(root.ExternalPath, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Ext(path) == ".tmp" {
			t.Errorf("staging remains: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTransferCancellationAfterPublicationRetainsCommitFacts(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	ids := seedTransferSeries(t, store, lib, 1)
	m := NewManager(store, time.Hour)
	session := newReadyTransferSession(t, m, store, lib)
	plan, err := m.PrepareTransfer(context.Background(), lib.ID, session, ids)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := m.Transfer(ctx, lib.ID, session, plan, func(p TransferProgress) {
		if len(p.Result.CommittedBookIDs) == 1 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || len(result.CommittedBookIDs) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(plan.Operations[0].Destination); err != nil {
		t.Fatal(err)
	}
}

func TestTransferRechecksRegisteredRootsBeforePublishingCurrentBook(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	ids := seedTransferSeries(t, store, lib, 1)
	m := NewManager(store, time.Hour)
	session := newReadyTransferSession(t, m, store, lib)
	plan, err := m.PrepareTransfer(context.Background(), lib.ID, session, ids)
	if err != nil {
		t.Fatal(err)
	}
	registered := false
	_, err = m.Transfer(context.Background(), lib.ID, session, plan, func(p TransferProgress) {
		if p.CopiedBytes == 0 || registered {
			return
		}
		registered = true
		_, createErr := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "New", Path: filepath.Dir(plan.Operations[0].Destination), ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz"})
		if createErr != nil {
			t.Fatal(createErr)
		}
	})
	if !errors.Is(err, ErrExternalPathInsideLibrary) {
		t.Fatalf("unsafe publication: %v", err)
	}
	if _, err := os.Stat(plan.Operations[0].Destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe target exists: %v", err)
	}
}

func TestTransferPreservesDestinationCreatedDuringCopy(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	ids := seedTransferSeries(t, store, lib, 1)
	m := NewManager(store, time.Hour)
	session := newReadyTransferSession(t, m, store, lib)
	plan, err := m.PrepareTransfer(context.Background(), lib.ID, session, ids)
	if err != nil {
		t.Fatal(err)
	}
	created := false
	result, err := m.Transfer(context.Background(), lib.ID, session, plan, func(p TransferProgress) {
		if p.CopiedBytes > 0 && !created {
			created = true
			if err := os.WriteFile(p.Operation.Destination, []byte("other writer's complete book"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CommittedBookIDs) != 0 || len(result.ExistingBookIDs) != 1 {
		t.Fatalf("result=%+v", result)
	}
	content, err := os.ReadFile(plan.Operations[0].Destination)
	if err != nil || string(content) != "other writer's complete book" {
		t.Fatalf("racing destination overwritten: %q %v", content, err)
	}
	entries, err := os.ReadDir(filepath.Dir(plan.Operations[0].Destination))
	if err != nil || len(entries) != 1 {
		t.Fatalf("uncommitted staging remained: %v %v", entries, err)
	}
}

func TestTransferFailureKeepsSuccessfulRangeAndSessionProjection(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	ids := seedTransferSeries(t, store, lib, 3)
	m := NewManager(store, time.Hour)
	session := newReadyTransferSession(t, m, store, lib)
	plan, err := m.PrepareTransfer(context.Background(), lib.ID, session, ids)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(plan.Operations[1].SourcePath); err != nil {
		t.Fatal(err)
	}
	result, err := m.Transfer(context.Background(), lib.ID, session, plan, nil)
	if !errors.Is(err, os.ErrNotExist) || len(result.Failures) != 1 || len(result.CommittedBookIDs) != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.CommittedBookIDs[0] != plan.Operations[0].BookID || result.CommittedBookIDs[1] != plan.Operations[2].BookID {
		t.Fatalf("wrong commit range: %+v", result)
	}
	snapshot, err := m.GetSession(lib.ID, session)
	if err != nil || snapshot.MatchedBooks != 2 {
		t.Fatalf("projection=%+v err=%v", snapshot, err)
	}
}

func TestCancelledValidationRetainsTransferSession(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	m := NewManager(store, time.Hour)
	session := newReadyTransferSession(t, m, store, lib)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.ValidateSession(ctx, lib.ID, session); !errors.Is(err, context.Canceled) {
		t.Fatalf("validation=%v", err)
	}
	if _, err := m.GetSession(lib.ID, session); err != nil {
		t.Fatalf("cancellation discarded retained session: %v", err)
	}
}
