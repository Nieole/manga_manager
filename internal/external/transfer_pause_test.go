package external

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
)

type transferDisk struct {
	runner   *diskwork.Runner
	executed chan string
}

func (d transferDisk) Disk(ctx context.Context, work diskwork.Work, fn func() error) error {
	_, err := d.runner.Do(ctx, work, func() error {
		err := fn()
		if d.executed != nil {
			d.executed <- work.Path
		}
		return err
	})
	return err
}

func transferResources() (transferDisk, *storageio.Scheduler) {
	cfg := config.Config{}
	config.NormalizeConfig(&cfg)
	cfg.Library.StorageProfile = config.StorageProfileCustom
	cfg.Library.IOPolicy = config.StorageIOPolicy{HashConcurrency: 1, ArchiveOpenConcurrency: 1}
	scheduler := storageio.NewScheduler()
	return transferDisk{runner: diskwork.NewRunner(func() config.Config { return cfg }, scheduler)}, scheduler
}

func TestTransferPausedWhileQueuedForQuotaDoesNotStartRead(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	ids := seedTransferSeries(t, store, lib, 1)
	m := NewManager(store, time.Hour)
	session := newReadyTransferSession(t, m, store, lib)
	plan, err := m.PrepareTransfer(context.Background(), lib.ID, session, ids)
	if err != nil {
		t.Fatal(err)
	}
	work, scheduler := transferResources()
	work.executed = make(chan string, 32)
	gate := taskcontrol.NewPauseGate()
	ctx, cancel := context.WithCancel(taskcontrol.WithPauseGate(context.Background(), gate))
	defer cancel()
	lease, err := scheduler.Acquire(ctx, storageio.Request{VolumeKey: config.VolumeKey(plan.Operations[0].SourcePath), Limit: 1, Kind: storageio.WorkKindIdentityHash})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	done := make(chan error, 1)
	go func() { _, err := m.Transfer(ctx, lib.ID, session, plan, work, nil); done <- err }()
	deadline := time.After(2 * time.Second)
	waiting := false
	for !waiting {
		for _, volume := range scheduler.Snapshot() {
			if volume.BackgroundWaiting > 0 {
				waiting = true
			}
		}
		select {
		case <-deadline:
			cancel()
			lease.Release()
			<-done
			t.Fatal("copy did not queue for quota")
		default:
		}
		if !waiting {
			time.Sleep(time.Millisecond)
		}
	}
	gate.Pause()
	lease.Release()
	select {
	case path := <-work.executed:
		cancel()
		gate.Resume()
		<-done
		t.Fatalf("paused copy performed IO after waiting for quota: %s", path)
	case err := <-done:
		t.Fatalf("paused copy exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	for _, volume := range scheduler.Snapshot() {
		if volume.Active != 0 {
			cancel()
			gate.Resume()
			<-done
			t.Fatalf("paused copy retained acquired quota: %+v", volume)
		}
	}
	gate.Resume()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("resume did not complete")
	}
	content, err := os.ReadFile(plan.Operations[0].Destination)
	if err != nil || string(content) != "x" {
		t.Fatalf("resumed book=%q err=%v", content, err)
	}
}

func TestTransferPausesWithinCurrentBookAndResumesSameStaging(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	ids := seedTransferSeries(t, store, lib, 1)
	m := NewManager(store, time.Hour)
	session := newReadyTransferSession(t, m, store, lib)
	plan, err := m.PrepareTransfer(context.Background(), lib.ID, session, ids)
	if err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("book"), 1<<20)
	if err := os.WriteFile(plan.Operations[0].SourcePath, content, 0644); err != nil {
		t.Fatal(err)
	}
	gate := taskcontrol.NewPauseGate()
	ctx, cancel := context.WithCancel(taskcontrol.WithPauseGate(context.Background(), gate))
	defer cancel()
	chunks := make(chan int64, 8)
	done := make(chan error, 1)
	work, scheduler := transferResources()
	go func() {
		_, transferErr := m.Transfer(ctx, lib.ID, session, plan, work, func(p TransferProgress) {
			if p.CopiedBytes > 0 {
				gate.Pause()
				chunks <- p.CopiedBytes
			}
		})
		done <- transferErr
	}()
	first := <-chunks
	select {
	case second := <-chunks:
		cancel()
		gate.Resume()
		<-done
		t.Fatalf("paused transfer kept copying current book: %d then %d", first, second)
	case err := <-done:
		t.Fatalf("paused transfer exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := os.Stat(plan.Operations[0].Destination); !errors.Is(err, os.ErrNotExist) {
		cancel()
		gate.Resume()
		<-done
		t.Fatalf("partial target visible: %v", err)
	}
	staging, err := filepath.Glob(filepath.Join(filepath.Dir(plan.Operations[0].Destination), ".mm-transfer-*.tmp"))
	if err != nil || len(staging) != 1 {
		cancel()
		gate.Resume()
		<-done
		t.Fatalf("staging=%v %v", staging, err)
	}
	info, err := os.Stat(staging[0])
	if err != nil || info.Size() != first {
		cancel()
		gate.Resume()
		<-done
		t.Fatalf("staging offset changed: %v %v", info, err)
	}
	for _, volume := range scheduler.Snapshot() {
		if volume.Active != 0 {
			cancel()
			gate.Resume()
			<-done
			t.Fatalf("paused copy retained IO quota: %+v", volume)
		}
	}
	if len(scheduler.Snapshot()) == 0 {
		cancel()
		gate.Resume()
		<-done
		t.Fatal("copy never acquired a real IO quota")
	}
	readerCtx, stopReader := context.WithTimeout(context.Background(), time.Second)
	defer stopReader()
	lease, err := scheduler.Acquire(readerCtx, storageio.Request{VolumeKey: config.VolumeKey(plan.Operations[0].SourcePath), Limit: 1, Kind: storageio.WorkKindReader})
	if err != nil {
		cancel()
		gate.Resume()
		<-done
		t.Fatalf("reader blocked by paused copy: %v", err)
	}
	read, readErr := os.ReadFile(plan.Operations[0].SourcePath)
	lease.Release()
	if readErr != nil || !bytes.Equal(read, content) {
		cancel()
		gate.Resume()
		<-done
		t.Fatalf("reader failed during pause: %v", readErr)
	}
	// Resume each observed block; the same staged file must accumulate the whole book.
	gate.Resume()
	for {
		select {
		case <-chunks:
			gate.Resume()
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(plan.Operations[0].Destination)
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("resumed book differs: %v", err)
			}
			return
		case <-time.After(3 * time.Second):
			cancel()
			gate.Resume()
			<-done
			t.Fatal("resume did not finish")
		}
	}
}

func TestPausedTransferRejectsChangedSourceOrStagingAndCleansUp(t *testing.T) {
	for _, change := range []string{"source_size", "source_time", "staging_size", "staging_time", "target_root", "cancel"} {
		t.Run(change, func(t *testing.T) {
			store, lib, _ := newExternalTestStore(t)
			ids := seedTransferSeries(t, store, lib, 1)
			m := NewManager(store, time.Hour)
			session := newReadyTransferSession(t, m, store, lib)
			plan, err := m.PrepareTransfer(context.Background(), lib.ID, session, ids)
			if err != nil {
				t.Fatal(err)
			}
			op := plan.Operations[0]
			if err := os.WriteFile(op.SourcePath, bytes.Repeat([]byte("book"), 1<<20), 0644); err != nil {
				t.Fatal(err)
			}
			gate := taskcontrol.NewPauseGate()
			ctx, cancel := context.WithCancel(taskcontrol.WithPauseGate(context.Background(), gate))
			defer cancel()
			paused := make(chan struct{}, 1)
			type outcome struct {
				result TransferResult
				err    error
			}
			done := make(chan outcome, 1)
			work, scheduler := transferResources()
			go func() {
				result, err := m.Transfer(ctx, lib.ID, session, plan, work, func(p TransferProgress) {
					if p.CopiedBytes > 0 {
						gate.Pause()
						select {
						case paused <- struct{}{}:
						default:
						}
					}
				})
				done <- outcome{result, err}
			}()
			select {
			case <-paused:
			case <-time.After(time.Second):
				cancel()
				gate.Resume()
				<-done
				t.Fatal("copy did not reach pause boundary")
			}
			staging, err := filepath.Glob(filepath.Join(filepath.Dir(op.Destination), ".mm-transfer-*.tmp"))
			if err != nil || len(staging) != 1 {
				cancel()
				gate.Resume()
				<-done
				t.Fatalf("staging=%v err=%v", staging, err)
			}
			switch change {
			case "source_size":
				err = os.WriteFile(op.SourcePath, []byte("changed"), 0644)
			case "source_time":
				err = os.Chtimes(op.SourcePath, time.Now(), time.Now().Add(time.Hour))
			case "staging_size":
				err = os.Truncate(staging[0], 0)
			case "staging_time":
				err = os.Chtimes(staging[0], time.Now(), time.Now().Add(time.Hour))
			case "target_root":
				_, err = store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "New root", Path: filepath.Dir(op.Destination), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
			case "cancel":
				cancel()
			}
			if err != nil {
				cancel()
				gate.Resume()
				<-done
				t.Fatal(err)
			}
			if change != "cancel" {
				gate.Resume()
			}
			select {
			case finished := <-done:
				if finished.err == nil || len(finished.result.CommittedBookIDs) != 0 {
					t.Fatalf("unsafe resume succeeded: %+v", finished)
				}
				if change == "cancel" && !errors.Is(finished.err, context.Canceled) {
					t.Fatalf("cancel result=%v", finished.err)
				}
			case <-time.After(2 * time.Second):
				cancel()
				gate.Resume()
				<-done
				t.Fatal("resume or cancellation did not exit")
			}
			if _, err := os.Stat(op.Destination); !os.IsNotExist(err) {
				t.Fatalf("unsafe copy published: %v", err)
			}
			if _, err := os.Stat(staging[0]); !os.IsNotExist(err) {
				t.Fatalf("unsafe staging not removed: %v", err)
			}
			for _, volume := range scheduler.Snapshot() {
				if volume.Active != 0 {
					t.Fatalf("IO quota leaked: %+v", volume)
				}
			}
		})
	}
}
