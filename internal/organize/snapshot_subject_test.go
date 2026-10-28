package organize_test

import (
	"context"
	"errors"
	"manga-manager/internal/organize"
	"manga-manager/internal/reading"
	"testing"
)

func TestSmartSnapshotCommandsRequireExplicitSubject(t *testing.T) {
	store, _ := newCollections(t)
	service := organize.NewSmartCollections(store)
	if _, err := service.PreviewSnapshot(context.Background(), 1, reading.Subject{}, organize.SmartSnapshotRequest{}, 1); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("preview missing subject: %v", err)
	}
	if _, err := service.Snapshot(context.Background(), 1, reading.Subject{}, organize.SmartSnapshotRequest{}); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("snapshot missing subject: %v", err)
	}
}
