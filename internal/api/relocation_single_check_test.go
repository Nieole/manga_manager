package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
)

type forbiddenCommitFiles struct{ calls atomic.Int64 }

type observedRunWriter struct {
	task.Store
	runID   atomic.Int64
	entered chan struct{}
}

func (s *observedRunWriter) SaveRun(ctx context.Context, run task.Run) error {
	if run.ID == s.runID.Load() {
		select {
		case s.entered <- struct{}{}:
		default:
		}
	}
	return s.Store.SaveRun(ctx, run)
}

func TestRelocationProgressDoesNotWaitForAnotherTasksWriter(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	writes := &observedRunWriter{Store: taskstore.New(store.DB()), entered: make(chan struct{}, 1)}
	c.taskEngine = newTaskEngine(taskEngineConfig{Store: writes, RunBackground: c.runBackground})
	otherHandle, releaseOther := make(chan *runhandle.Handle, 1), make(chan struct{})
	t.Cleanup(func() { close(releaseOther) })
	other, err := c.taskEngine.start(libraryTask("scan_library", lib.ID+1, variantSole), task.TriggerManual,
		RunSpec{}, func(_ context.Context, handle *runhandle.Handle) (RunResult, error) {
			otherHandle <- handle
			<-releaseOther
			return RunResult{}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	handleOther := <-otherHandle
	writes.runID.Store(other.Run.ID)
	main, err := c.taskEngine.start(libraryTask("relocation_commit", lib.ID, variantSole), task.TriggerManual,
		RunSpec{CanCancel: true}, func(ctx context.Context, handle *runhandle.Handle) (RunResult, error) {
			if err := handle.DisableCancellation(); err != nil {
				return RunResult{}, err
			}
			handle.Advance(0, 200, "task.msg.relocation.updating", nil)
			handle.AdvanceLive(0, 200)
			tx, err := store.DB().BeginTx(ctx, nil)
			if err != nil {
				return RunResult{}, err
			}
			defer tx.Rollback()
			if _, err = tx.Exec(`UPDATE libraries SET name='in transaction' WHERE id=?`, lib.ID); err != nil {
				return RunResult{}, err
			}
			writerDone := make(chan struct{})
			go func() { handleOther.Advance(1, 2, "", nil); close(writerDone) }()
			select {
			case <-writes.entered:
			case <-time.After(2 * time.Second):
				return RunResult{}, errors.New("concurrent writer did not start")
			}
			progressDone := make(chan struct{})
			go func() { handle.AdvanceLive(100, 200); close(progressDone) }()
			select {
			case <-progressDone:
			case <-time.After(2 * time.Second):
				return RunResult{}, errors.New("transaction progress waited on competing writer")
			}
			if err := tx.Commit(); err != nil {
				return RunResult{}, err
			}
			<-writerDone
			handle.Advance(200, 200, "task.msg.relocation.updating", nil)
			return RunResult{}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.taskEngine.awaitRunOutcome(ctx, main.Run.ID); err != nil {
		t.Fatal(err)
	}
	otherSnapshot, err := c.taskEngine.engine.SnapshotOf(ctx, other.Run.ID)
	if err != nil || otherSnapshot.Run.Current != 1 {
		t.Fatalf("concurrent task lost progress: %+v %v", otherSnapshot, err)
	}
}

func (f *forbiddenCommitFiles) Stat(string) (os.FileInfo, error) {
	f.calls.Add(1)
	return nil, os.ErrPermission
}

func TestRelocationTransactionProgressRemainsLiveAndRollsBack(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
			clock := &fakeClock{now: time.Now()}
			c.taskEngine = newTaskEngine(taskEngineConfig{
				Store: taskstore.New(store.DB()), RunBackground: func(fn func()) { fn() }, Now: clock.Now,
			})
			_, err := c.taskEngine.start(libraryTask("relocation_commit", lib.ID, variantSole), task.TriggerManual,
				RunSpec{CanCancel: true}, func(ctx context.Context, handle *runhandle.Handle) (RunResult, error) {
					if err := handle.DisableCancellation(); err != nil {
						return RunResult{}, err
					}
					handle.Phase("commit", "task.msg.relocation.updating", nil)
					handle.Advance(0, 200, "task.msg.relocation.updating", nil)
					handle.AdvanceLive(0, 200)
					tx, err := store.DB().BeginTx(ctx, nil)
					if err != nil {
						return RunResult{}, err
					}
					defer tx.Rollback()
					if _, err = tx.Exec(`UPDATE libraries SET name='transaction in progress' WHERE id=?`, lib.ID); err != nil {
						return RunResult{}, err
					}
					clock.advance(11 * time.Second)
					// Sampling is due, but must not try a second writer while the
					// transaction's owner is about to publish its next counters.
					c.taskEngine.engine.SampleActiveRuns(ctx)
					handle.AdvanceLive(100, 200)
					rec := httptest.NewRecorder()
					c.getTaskCenterLive(rec, httptest.NewRequest(http.MethodGet, "/api/system/tasks/live", nil))
					var live RunLive
					if err := json.Unmarshal(rec.Body.Bytes(), &live); err != nil || rec.Code != http.StatusOK || len(live.Runs) != 1 {
						t.Fatalf("live progress: %d %s %v", rec.Code, rec.Body.String(), err)
					}
					if run := live.Runs[0]; run.Current != 100 || run.Total != 200 || run.CanCancel {
						t.Fatalf("transaction counters unavailable: %+v", run)
					}
					if rollback {
						return RunResult{}, errors.New("rollback fixture")
					}
					if err := tx.Commit(); err != nil {
						return RunResult{}, err
					}
					handle.Advance(200, 200, "task.msg.relocation.updating", nil)
					return RunResult{}, nil
				})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			c.listTasks(rec, httptest.NewRequest(http.MethodGet, "/api/system/tasks", nil))
			var runs []RunSnapshot
			if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil || len(runs) != 1 {
				t.Fatalf("terminal runs: %s %v", rec.Body.String(), err)
			}
			want, count := "completed", 200
			if rollback {
				want, count = "failed", 0
			}
			if runs[0].Status != want || runs[0].Current != count {
				t.Fatalf("provisional progress survived terminal state: %+v", runs[0])
			}
		})
	}
}

func (f *forbiddenCommitFiles) Open(string) (io.Closer, error) {
	f.calls.Add(1)
	return nil, os.ErrPermission
}

func TestRelocationCommitUsesPreviewWithoutReadingTargetFiles(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	// A persisted preview remains usable after restart even if the destination
	// changed outside the application; file state is the administrator's snapshot.
	c = restartController(t, c, store, root)
	if err := os.WriteFile(filepath.Join(target, "series", "book.cbz"), []byte("changed after preview"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := &forbiddenCommitFiles{}
	c.relocation.Files = files
	op := awaitRelocationOperation(t, c, submitRelocation(t, c, plan.ID).ID)
	if op.Status != "completed" || files.calls.Load() != 0 {
		t.Fatalf("commit must reuse checked files: status=%s code=%s filesystem_calls=%d", op.Status, op.Code, files.calls.Load())
	}
	got, err := store.GetBook(context.Background(), book.ID)
	if err != nil || got.Path != filepath.Join(target, "series", "book.cbz") {
		t.Fatalf("mapped book: %+v %v", got, err)
	}
}

func TestRelocationCommitRequiresRecordSnapshot(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if _, err := store.DB().Exec(`DELETE FROM relocation_record_facts WHERE plan_id=?`, plan.ID); err != nil {
		t.Fatal(err)
	}
	op := awaitRelocationOperation(t, c, submitRelocation(t, c, plan.ID).ID)
	if op.Status != "failed" || op.Code != "relocation_stale_plan" {
		t.Fatalf("old preview must require a new check: %+v", op)
	}
	got, err := store.GetBook(context.Background(), book.ID)
	if err != nil || got.Path != book.Path {
		t.Fatalf("expired preview changed paths: %+v %v", got, err)
	}
}
