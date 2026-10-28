package diskjobs_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"manga-manager/internal/application"
	"manga-manager/internal/application/diskjobs"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/storageio"
	"manga-manager/internal/task"
	"manga-manager/internal/taskcontrol"
)

func TestTransferRunPauseResumeAndCancelKeepCommittedRange(t *testing.T) {
	for _, action := range []string{"resume", "source_changed", "cancel"} {
		t.Run(action, func(t *testing.T) {
			rig := newDiskRig(t, func(fn func()) { fn() })
			second := filepath.Join(rig.series.Path, "second.cbz")
			if err := os.WriteFile(second, []byte("second"), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := rig.store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: rig.lib.ID, SeriesID: rig.series.ID, Name: "second.cbz", Path: second, Size: 6, PageCount: 1, FileModifiedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			session := rig.ready(t)
			plan, err := rig.manager.PrepareTransfer(context.Background(), rig.lib.ID, session.SessionID, []int64{rig.series.ID})
			if err != nil || len(plan.Operations) != 2 {
				t.Fatalf("plan=%+v err=%v", plan, err)
			}
			current := plan.Operations[1]
			content := bytes.Repeat([]byte("second book"), 1<<19)
			if err := os.WriteFile(current.SourcePath, content, 0644); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{}
			config.NormalizeConfig(&cfg)
			cfg.Library.StorageProfile = config.StorageProfileCustom
			cfg.Library.IOPolicy = config.StorageIOPolicy{HashConcurrency: 1, ArchiveOpenConcurrency: 1}
			scheduler := storageio.NewScheduler()
			paused := make(chan int64, 1)
			var runCtx context.Context
			var ticks atomic.Int64
			base := time.Now()
			barrier := false
			engine := task.New(task.Config{
				Store: rig.tasks, RunBackground: func(fn func()) { go fn() },
				Now:      func() time.Time { return base.Add(time.Duration(ticks.Add(1)) * time.Second) },
				DiskWork: diskwork.NewRunner(func() config.Config { return cfg }, scheduler),
				DecorateRunContext: func(ctx context.Context, _ task.Run, _ task.RunSpec) context.Context {
					runCtx = ctx
					return runCtx
				},
				Publish: func(snapshot task.Snapshot) {
					if !barrier && snapshot.Side.Metrics["transferred_files"] == 1 && snapshot.Side.Metrics["transfer_bytes_staged"] > 0 {
						barrier = true
						taskcontrol.FromContext(runCtx).Pause()
						paused <- snapshot.Run.ID
					}
				},
			})
			service := diskjobs.New(application.NewRuns(engine, nil), rig.store, rig.manager, diskjobs.Effects{})
			launch, err := service.TransferPrepared(context.Background(), rig.lib.ID, session.SessionID, plan)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = engine.Cancel(launch.Run.ID)
				cleanupCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
				defer stop()
				_, _ = engine.Await(cleanupCtx, launch.Run.ID)
			})
			select {
			case <-paused:
			case <-time.After(3 * time.Second):
				t.Fatal("run did not reach mid-book barrier")
			}
			if err := engine.Pause(launch.Run.ID); err != nil {
				t.Fatal(err)
			}
			snapshot, err := engine.SnapshotOf(context.Background(), launch.Run.ID)
			if err != nil || snapshot.Run.Status != task.StatusPaused || snapshot.Run.Current != 1 || snapshot.Side.Args["committed_book_ids"] != strconv.FormatInt(plan.Operations[0].BookID, 10) {
				t.Fatalf("paused=%+v err=%v", snapshot, err)
			}
			for _, volume := range scheduler.Snapshot() {
				if volume.Active != 0 {
					t.Fatalf("paused run held quota: %+v", volume)
				}
			}
			readCtx, stop := context.WithTimeout(context.Background(), time.Second)
			lease, err := scheduler.Acquire(readCtx, storageio.Request{VolumeKey: config.VolumeKey(current.SourcePath), Limit: 1, Kind: storageio.WorkKindReader})
			stop()
			if err != nil {
				t.Fatal(err)
			}
			read, readErr := os.ReadFile(plan.Operations[0].Destination)
			lease.Release()
			if readErr != nil || len(read) == 0 {
				t.Fatalf("reader could not read committed file while paused: %v", readErr)
			}
			switch action {
			case "source_changed":
				if err := os.Chtimes(current.SourcePath, time.Now(), time.Now().Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				if err := engine.Cancel(launch.Run.ID); err != nil {
					t.Fatal(err)
				}
			}
			if action != "cancel" {
				if err := engine.Resume(launch.Run.ID); err != nil {
					t.Fatal(err)
				}
			}
			wait, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			run, err := engine.Await(wait, launch.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := task.StatusCompleted
			if action == "source_changed" {
				want = task.StatusFailed
			}
			if action == "cancel" {
				want = task.StatusCancelled
			}
			if run.Status != want {
				t.Fatalf("run=%+v", run)
			}
			if !strings.Contains(run.MessageParams["committed_book_ids"], strconv.FormatInt(plan.Operations[0].BookID, 10)) {
				t.Fatalf("lost earlier commit: %+v", run)
			}
			coverage, err := rig.manager.GetSession(rig.lib.ID, session.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if action == "resume" {
				read, err := os.ReadFile(current.Destination)
				if err != nil || !bytes.Equal(read, content) || coverage.MatchedBooks != 2 || run.MessageParams["added"] != "2" {
					t.Fatalf("resume content=%v coverage=%+v run=%+v err=%v", len(read), coverage, run, err)
				}
			} else {
				if _, err := os.Stat(current.Destination); !os.IsNotExist(err) || coverage.MatchedBooks != 1 || run.MessageParams["added"] != "1" {
					t.Fatalf("uncommitted result coverage=%+v run=%+v err=%v", coverage, run, err)
				}
			}
			staging, err := filepath.Glob(filepath.Join(filepath.Dir(current.Destination), ".mm-transfer-*.tmp"))
			if err != nil || len(staging) != 0 {
				t.Fatalf("staging=%v err=%v", staging, err)
			}
			for _, volume := range scheduler.Snapshot() {
				if volume.Active != 0 {
					t.Fatalf("quota leaked: %+v", volume)
				}
			}
		})
	}
}
