package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/external"
	"manga-manager/internal/parser"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

func TestRelocationCommitPreservesIdentityAndReadsNewLocation(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "旧盘", "漫画 空格", "第一卷.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	read := func() {
		t.Helper()
		rec := httptest.NewRecorder()
		c.servePageImage(rec, requestWithRouteParams(http.MethodGet, "/api/books/page/1/1", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), png1x1) {
			t.Fatalf("read same book: %d %s", rec.Code, rec.Body.String())
		}
	}
	read()
	parser.EvictArchiveFromPool(book.Path)
	session, err := c.external.CreateSession(context.Background(), lib.ID, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.external.ScanSession(context.Background(), session.SessionID, discardScanHandle{}); err != nil {
		t.Fatal(err)
	}
	transfer, err := c.external.PrepareTransfer(context.Background(), lib.ID, session.SessionID, []int64{series.ID})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "目标盘")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if plan.BlockerCount != 0 || plan.AnomalyCount != 0 {
		t.Fatalf("preview: %+v", plan)
	}
	rec := httptest.NewRecorder()
	c.commitRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/relocation-plans/1/commit", []byte(`{}`), "planId", strconv.FormatInt(plan.ID, 10)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body.String())
	}
	var result RelocationOperation
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for result.Status != "completed" {
		select {
		case <-deadline:
			t.Fatalf("operation did not finish: %+v", result)
		default:
		}
		rec = httptest.NewRecorder()
		c.getRelocationOperation(rec, requestWithRouteParam(http.MethodGet, "/api/relocation-operations/1", nil, "operationId", strconv.FormatInt(result.ID, 10)))
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("get operation: %d %s %v", rec.Code, rec.Body.String(), err)
		}
		time.Sleep(time.Millisecond)
	}
	gotLib, _ := store.GetLibrary(context.Background(), lib.ID)
	gotSeries, _ := store.GetSeries(context.Background(), series.ID)
	gotBook, _ := store.GetBook(context.Background(), book.ID)
	if gotLib.Path != target || gotSeries.Path != filepath.Join(target, "漫画 空格") || gotBook.Path != filepath.Join(target, "漫画 空格", "第一卷.cbz") {
		t.Fatalf("paths: %s %s %s", gotLib.Path, gotSeries.Path, gotBook.Path)
	}
	if gotBook.Title != book.Title || gotSeries.Name != series.Name || result.PlanID != plan.ID || result.RunID == 0 {
		t.Fatalf("preserved identity/result: %+v %+v", gotBook, result)
	}
	read()
	if err = c.launchExternalLibraryTransferTask(lib.ID, session.SessionID, transfer); !errors.Is(err, external.ErrSessionNotFound) {
		t.Fatalf("migration retained old transfer plan: %v", err)
	}
}

func TestRelocationCommittedEffectsFailureBlocksUntilRealStartupRecovery(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	effects := c.relocation.Effects
	c.relocation.Effects = func(ctx context.Context, op RelocationOperation) error {
		if err := effects(ctx, op); err != nil {
			return err
		}
		return errors.New("watcher unavailable")
	}
	op := submitRelocation(t, c, plan.ID)
	deadline := time.After(10 * time.Second)
	for {
		run, err := c.taskEngine.runStore.LoadRun(context.Background(), op.RunID)
		if err == nil && run.Status.IsTerminal() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("commit run did not finish")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	op = awaitRelocationOperation(t, c, op.ID)
	if op.Status != "paths_committed" || op.Code != "relocation_recovering" {
		t.Fatalf("truthful result: %+v", op)
	}
	page := httptest.NewRecorder()
	c.servePageImage(page, requestWithRouteParams(http.MethodGet, "/api/books/page/1/1", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
	if page.Code != http.StatusServiceUnavailable {
		t.Fatalf("incomplete migration reads: %d", page.Code)
	}
	c.Close()
	reopened := restartController(t, c, store, root)
	op = awaitRelocationOperation(t, reopened, op.ID)
	if op.Status != "completed" {
		t.Fatalf("startup did not recover: %+v", op)
	}
	page = httptest.NewRecorder()
	reopened.servePageImage(page, requestWithRouteParams(http.MethodGet, "/api/books/page/1/1", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
	if page.Code != http.StatusOK || !bytes.Equal(page.Body.Bytes(), png1x1) {
		t.Fatalf("recovered page: %d", page.Code)
	}
	second := restartController(t, reopened, store, root)
	op = awaitRelocationOperation(t, second, op.ID)
	if op.Status != "completed" {
		t.Fatalf("recovery not idempotent: %+v", op)
	}
}

func TestRelocationCommitCanCancelBeforeWriting(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	started, resume := make(chan struct{}), make(chan struct{})
	acquire := c.relocation.AcquireOperation
	c.relocation.AcquireOperation = func(ctx context.Context, op RelocationOperation) (func(), error) {
		release, err := acquire(ctx, op)
		close(started)
		<-resume
		return release, err
	}
	op := submitRelocation(t, c, plan.ID)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("revalidation not started")
	}
	rec := httptest.NewRecorder()
	c.cancelRun(rec, requestWithRouteParam(http.MethodPost, "/api/system/runs/1/cancel", nil, "runID", strconv.FormatInt(op.RunID, 10)))
	close(resume)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("cancel before commit: %d %s", rec.Code, rec.Body.String())
	}
	op = awaitRelocationOperation(t, c, op.ID)
	if op.Status != "cancelled" {
		t.Fatalf("cancelled result: %+v", op)
	}
	got, _ := store.GetBook(context.Background(), book.ID)
	if got.Path != book.Path {
		t.Fatal("cancelled confirmation changed paths")
	}
}

func TestLibraryRootEditingRequiresRelocationOnlyWhenPopulated(t *testing.T) {
	for _, populated := range []bool{true, false} {
		c, store, _, root := newTestController(t)
		lib, series, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
		if !populated {
			if err := store.DeleteBook(context.Background(), book.ID); err != nil {
				t.Fatal(err)
			}
			if err := store.DeleteSeries(context.Background(), series.ID); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(root, "target")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"name": "unconfirmed rename", "path": target, "scan_mode": "none", "scan_interval": 60})
		rec := httptest.NewRecorder()
		c.updateLibrary(rec, requestWithRouteParam(http.MethodPut, "/api/libraries/1", body, "libraryId", strconv.FormatInt(lib.ID, 10)))
		got, _ := store.GetLibrary(context.Background(), lib.ID)
		if populated {
			if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("relocation_required")) || got.Path != lib.Path || got.Name != lib.Name {
				t.Fatalf("root edit bypassed migration: %d %s %+v", rec.Code, rec.Body.String(), got)
			}
		} else if rec.Code != http.StatusOK || got.Path != target {
			t.Fatalf("empty library edit rejected: %d %s", rec.Code, rec.Body.String())
		}
	}
}

func TestRelocationDuplicateConfirmationAndDatabaseFailureAreDurable(t *testing.T) {
	for _, failDB := range []bool{false, true} {
		c, store, _, root := newTestController(t)
		lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
		seedBookArchivePages(t, c, book, png1x1)
		target := filepath.Join(root, "target")
		if err := os.Rename(lib.Path, target); err != nil {
			t.Fatal(err)
		}
		plan := startRelocationPreview(t, c, lib.ID, target)
		if failDB {
			if _, err := store.DB().Exec(`CREATE TRIGGER fail_relocation BEFORE UPDATE OF path ON libraries BEGIN SELECT RAISE(ABORT,'disk full'); END`); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		results := make(chan RelocationOperation, 2)
		for range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); results <- submitRelocation(t, c, plan.ID) }()
		}
		wg.Wait()
		close(results)
		var id int64
		for op := range results {
			if id != 0 && id != op.ID {
				t.Fatalf("duplicate operations: %d %d", id, op.ID)
			}
			id = op.ID
		}
		op := awaitRelocationOperation(t, c, id)
		want := "completed"
		if failDB {
			want = "failed"
		}
		if op.Status != want {
			t.Fatalf("database boundary: %+v", op)
		}
		got, _ := store.GetBook(context.Background(), book.ID)
		if failDB && got.Path != book.Path {
			t.Fatal("failed transaction changed child path")
		}
		if again := submitRelocation(t, c, plan.ID); again.ID != id || again.Status != want {
			t.Fatalf("retry replayed operation: %+v", again)
		}
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM relocation_operations`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("operation count %d %v", count, err)
		}
	}
}

func TestRelocationQueuedCancellationSurvivesPrunedHistoryAndRestart(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if _, err := c.taskEngine.pauseAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	op := submitRelocation(t, c, plan.ID)
	rec := httptest.NewRecorder()
	c.cancelRun(rec, requestWithRouteParam(http.MethodPost, "/api/system/runs/1/cancel", nil, "runID", strconv.FormatInt(op.RunID, 10)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("queued cancel: %d %s", rec.Code, rec.Body.String())
	}
	clearCancelledPreviewRuns(t, c)
	c.Close()
	reopened := restartController(t, c, store, root)
	op = awaitRelocationOperation(t, reopened, op.ID)
	if op.Status != "cancelled" {
		t.Fatalf("cancel fact lost: %+v", op)
	}
	got, _ := store.GetBook(context.Background(), book.ID)
	if got.Path != book.Path {
		t.Fatal("queued cancel changed paths")
	}
}

func TestRelocationCommitRejectsWorkWhoseOtherLibraryCoversMappedFiles(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "parent", Path: root, ScanMode: "none", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	started, resume := make(chan struct{}), make(chan struct{})
	if err = c.taskEngine.Run(libraryTask("test_transfer", other.ID, variantSole), task.TriggerManual, RunSpec{Key: "test_transfer", CanCancel: false}, func(context.Context, *runhandle.Handle) (RunResult, error) {
		close(started)
		<-resume
		return RunResult{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	op := submitRelocation(t, c, plan.ID)
	op = awaitRelocationOperation(t, c, op.ID)
	close(resume)
	if op.Status != "failed" || op.Code != "relocation_conflict" {
		t.Fatalf("overlapping work accepted: %+v", op)
	}
	got, _ := store.GetBook(context.Background(), book.ID)
	if got.Path != book.Path {
		t.Fatal("conflict changed paths")
	}
}

func TestRelocationPreviewListsOtherLibraryWorkCoveringMappedFiles(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "parent", Path: root, ScanMode: "none", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	started, resume := make(chan struct{}), make(chan struct{})
	defer close(resume)
	launched, err := c.taskEngine.start(libraryTask("test_transfer", other.ID, variantSole), task.TriggerManual, RunSpec{Key: "test_transfer", CanCancel: false}, func(context.Context, *runhandle.Handle) (RunResult, error) {
		close(started)
		<-resume
		return RunResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	plan := startRelocationPreview(t, c, lib.ID, target)
	page := previewDetails(t, c, plan.ID, "kind=blocker")
	if plan.BlockerCount != 1 || len(page.Items) != 1 || page.Items[0].RunID != launched.Run.ID || page.Items[0].Reason != "wait_for_completion" {
		t.Fatalf("related work not listed: %+v %+v", plan, page)
	}
}

func TestRelocationWatchRecoveryKeepsLibraryBlockedWhenTargetCannotBeWatched(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	if _, err := store.DB().Exec(`UPDATE libraries SET scan_mode='watch' WHERE id=?`, lib.ID); err != nil {
		t.Fatal(err)
	}
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	effects := c.relocation.Effects
	c.relocation.Effects = func(ctx context.Context, op RelocationOperation) error {
		if err := effects(ctx, op); err != nil {
			return err
		}
		return errors.New("completion interrupted")
	}
	op := submitRelocation(t, c, plan.ID)
	op = awaitRelocationOperation(t, c, op.ID)
	if op.Status != "paths_committed" {
		t.Fatalf("initial incomplete operation: %+v", op)
	}
	c.Close()
	offline := filepath.Join(root, "offline")
	if err := os.Rename(target, offline); err != nil {
		t.Fatal(err)
	}
	reopened := restartController(t, c, store, root)
	op = awaitRelocationOperation(t, reopened, op.ID)
	if op.Status != "paths_committed" || op.Code != "relocation_recovering" {
		t.Fatalf("unwatched root reported complete: %+v", op)
	}
	rec := httptest.NewRecorder()
	reopened.servePageImage(rec, requestWithRouteParams(http.MethodGet, "/api/books/page/1/1", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed recovery exposed library: %d", rec.Code)
	}
	reopened.Close()
	if err := os.Rename(offline, target); err != nil {
		t.Fatal(err)
	}
	final := restartController(t, reopened, store, root)
	if got := awaitRelocationOperation(t, final, op.ID); got.Status != "completed" {
		t.Fatalf("watch recovery failed: %+v", got)
	}
}

func TestRelocationBlocksNewConflictingLibraryRootsUntilEffectsFinish(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	otherRoot := filepath.Join(root, "other")
	if err := os.MkdirAll(otherRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "empty", Path: otherRoot, ScanMode: "none", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	started, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(resume) }) }
	defer release()
	effects := c.relocation.Effects
	c.relocation.Effects = func(ctx context.Context, op RelocationOperation) error {
		close(started)
		<-resume
		return effects(ctx, op)
	}
	op := submitRelocation(t, c, plan.ID)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("effects did not start")
	}
	for _, create := range []bool{false, true} {
		path := filepath.Join(target, "series", "new-child")
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"name": "new root", "path": path, "scan_mode": "none", "scan_interval": 60})
		rec := httptest.NewRecorder()
		if create {
			c.createLibrary(rec, httptest.NewRequest(http.MethodPost, "/api/libraries", bytes.NewReader(body)))
		} else {
			c.updateLibrary(rec, requestWithRouteParam(http.MethodPut, "/api/libraries/2", body, "libraryId", strconv.FormatInt(other.ID, 10)))
		}
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("conflicting root allowed create=%v: %d %s", create, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	c.cancelRun(rec, requestWithRouteParam(http.MethodPost, "/api/system/runs/1/cancel", nil, "runID", strconv.FormatInt(op.RunID, 10)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("commit advertised cancellation after write: %d", rec.Code)
	}
	release()
	if got := awaitRelocationOperation(t, c, op.ID); got.Status != "completed" {
		t.Fatalf("completion: %+v", got)
	}
}

func submitRelocation(t *testing.T, c *Controller, planID int64) RelocationOperation {
	t.Helper()
	rec := httptest.NewRecorder()
	c.commitRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/relocation-plans/1/commit", []byte(`{}`), "planId", strconv.FormatInt(planID, 10)))
	var op RelocationOperation
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil || rec.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s %v", rec.Code, rec.Body.String(), err)
	}
	return op
}

func TestRelocationCommitRejectsChangedConfirmationFacts(t *testing.T) {
	for _, change := range []string{"record", "config", "root", "occupied"} {
		t.Run(change, func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, series, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
			seedBookArchivePages(t, c, book, png1x1)
			target := filepath.Join(root, "target")
			if err := os.Rename(lib.Path, target); err != nil {
				t.Fatal(err)
			}
			plan := startRelocationPreview(t, c, lib.ID, target)
			switch change {
			case "record":
				if _, err := store.DB().Exec(`UPDATE books SET size=size+1 WHERE id=?`, book.ID); err != nil {
					t.Fatal(err)
				}
			case "config":
				cfg := c.config.Snapshot()
				cfg.Library.IOPolicy.ArchiveOpenConcurrency++
				c.config.Replace(&cfg)
			case "root":
				if _, err := store.DB().Exec(`UPDATE libraries SET path=? WHERE id=?`, root, lib.ID); err != nil {
					t.Fatal(err)
				}
			case "occupied":
				other, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "other", Path: filepath.Join(root, "other"), ScanMode: "none", ScanInterval: 60})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: other.ID, Name: "occupied", Path: filepath.Join(target, "series"), NameInitial: "O"}); err != nil {
					t.Fatal(err)
				}
			}
			rec := httptest.NewRecorder()
			c.commitRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/relocation-plans/1/commit", []byte(`{}`), "planId", strconv.FormatInt(plan.ID, 10)))
			if rec.Code != http.StatusAccepted {
				t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
			}
			var op RelocationOperation
			if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
				t.Fatal(err)
			}
			op = awaitRelocationOperation(t, c, op.ID)
			if op.Status != "failed" || op.Code != "relocation_stale_plan" {
				t.Fatalf("changed facts accepted: %+v", op)
			}
			gotSeries, _ := store.GetSeries(context.Background(), series.ID)
			gotBook, _ := store.GetBook(context.Background(), book.ID)
			if gotSeries.Path != series.Path || gotBook.Path != book.Path {
				t.Fatal("stale plan changed paths")
			}
		})
	}
}

func awaitRelocationOperation(t *testing.T, c *Controller, id int64) RelocationOperation {
	t.Helper()
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		rec := httptest.NewRecorder()
		c.getRelocationOperation(rec, requestWithRouteParam(http.MethodGet, "/api/relocation-operations/1", nil, "operationId", strconv.FormatInt(id, 10)))
		var op RelocationOperation
		if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("result: %d %s %v", rec.Code, rec.Body.String(), err)
		}
		if op.Status != "pending" && op.Status != "prepared" && (op.Status != "paths_committed" || op.Code == "relocation_recovering" || op.Code == "restoration_recovering") {
			return op
		}
		select {
		case <-deadline:
			t.Fatalf("operation pending: %+v", op)
		case <-ticker.C:
		}
	}
}
