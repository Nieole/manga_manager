package diskjobs_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/application"
	"manga-manager/internal/application/diskjobs"
	"manga-manager/internal/task"
)

func TestTransferCancelledDuringCurrentBookPublishesOnlyCommittedFacts(t *testing.T) {
	rig := newDiskRig(t, func(fn func()) { fn() })
	session := rig.ready(t)
	if err := os.WriteFile(rig.book.Path, bytes.Repeat([]byte("book"), 1<<20), 0644); err != nil {
		t.Fatal(err)
	}
	var cancel context.CancelFunc
	now := time.Now()
	engine := task.New(task.Config{
		Store: rig.tasks, RunBackground: func(fn func()) { fn() },
		Now: func() time.Time { now = now.Add(time.Second); return now },
		DecorateRunContext: func(ctx context.Context, _ task.Run, _ task.RunSpec) context.Context {
			var runCtx context.Context
			runCtx, cancel = context.WithCancel(ctx)
			return runCtx
		},
		Publish: func(snapshot task.Snapshot) {
			if snapshot.Side.Metrics["transfer_bytes_staged"] > 0 {
				cancel()
			}
		},
	})
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()
	publications := 0
	service := diskjobs.New(application.NewRuns(engine, nil), rig.store, rig.manager, diskjobs.Effects{Publish: func(string) { publications++ }})
	launch, err := service.TransferExternal(context.Background(), rig.lib.ID, session.SessionID, []int64{rig.series.ID})
	if err != nil {
		t.Fatal(err)
	}
	run, err := rig.tasks.LoadRun(context.Background(), launch.Run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != task.StatusCancelled || run.MessageParams["added"] != "0" || run.MessageParams["committed_book_ids"] != "" {
		t.Fatalf("run = %+v", run)
	}
	if publications == 0 {
		t.Fatal("cancelled transfer did not publish session refresh")
	}
	if _, err := os.Stat(filepath.Join(rig.destination, "Series", "book.cbz")); !os.IsNotExist(err) {
		t.Fatalf("uncommitted destination = %v", err)
	}
}
