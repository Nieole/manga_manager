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
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/runtimecfg"
	"manga-manager/internal/scanner"
)

func TestRelocationCommitMovesExplicitPoliciesAndPreservesOtherSettings(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	archiveBefore, err := os.ReadFile(book.Path)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	cfg := c.currentConfig()
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{
		{Path: root, StorageProfile: "ssd"},
		{Path: lib.Path, StorageProfile: "hdd_external"},
		{Path: filepath.Join(lib.Path, "series"), StorageProfile: "custom", IOPolicy: config.StorageIOPolicy{ArchiveOpenConcurrency: 3}},
		{Path: filepath.Join(target, "series"), StorageProfile: "custom", IOPolicy: config.StorageIOPolicy{ArchiveOpenConcurrency: 3}},
		{Path: target, StorageProfile: "hdd_external"},
	}
	if _, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "unchanged nested", Path: filepath.Join(target, "unrelated-nested"), ScanMode: "none", ScanInterval: 60}); err != nil {
		t.Fatal(err)
	}
	if err := c.persistConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if plan.BlockerCount != 0 || plan.PolicyCount != 2 {
		t.Fatalf("preview: %+v", plan)
	}
	// A completed settings save after preview must survive the policy-only patch.
	cfg = c.currentConfig()
	cfg.Tasks.RunSlots = 7
	if err := c.persistConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	op := submitRelocation(t, c, plan.ID)
	op = awaitRelocationOperation(t, c, op.ID)
	if op.Status != "completed" {
		t.Fatalf("commit: %+v", op)
	}
	disk, err := config.LoadConfigFile(c.configPath)
	if err != nil {
		t.Fatal(err)
	}
	live := c.currentConfig()
	for _, got := range []config.Config{*disk, live} {
		if got.Tasks.RunSlots != 7 || len(got.Library.StoragePolicies) != 3 {
			t.Fatalf("settings/policy count: %+v", got.Library.StoragePolicies)
		}
		if config.ResolveStoragePolicy(got, target).StorageProfile != "hdd_external" || config.ResolveStoragePolicy(got, filepath.Join(target, "series")).IOPolicy.ArchiveOpenConcurrency != 3 {
			t.Fatal("mapped policy not effective")
		}
		if config.ResolveStoragePolicy(got, lib.Path).StorageProfile != "ssd" {
			t.Fatal("source policy or ancestor changed")
		}
	}
	got, _ := store.GetBook(context.Background(), book.ID)
	if got.Path != filepath.Join(target, "series", "book.cbz") {
		t.Fatalf("book path: %s", got.Path)
	}
	archiveAfter, err := os.ReadFile(got.Path)
	if err != nil || !bytes.Equal(archiveBefore, archiveAfter) {
		t.Fatal("policy migration changed manga bytes")
	}
}

func TestRelocationPolicyDuplicateSourceKeysCannotSilentlyLoseBehavior(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	cfg := c.currentConfig()
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: "hdd_external"}, {Path: lib.Path, StorageProfile: "network"}}
	if err := c.persistConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if plan.BlockerCount == 0 {
		t.Fatal("ambiguous source policies accepted")
	}
}

func TestRelocationPolicyRuntimeFailureRecoversThroughFreshManager(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	cfg := c.currentConfig()
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: "hdd_external"}}
	if err := c.persistConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	c.relocation.ApplyConfig = func(*config.Config) error { return errors.New("runtime unavailable") }
	op := submitRelocation(t, c, plan.ID)
	op = awaitRelocationOperation(t, c, op.ID)
	if op.Status != "paths_committed" || op.Code != "relocation_recovering" {
		t.Fatalf("truthful failed runtime: %+v", op)
	}
	page := httptest.NewRecorder()
	c.servePageImage(page, requestWithRouteParams(http.MethodGet, "/page", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
	if page.Code != http.StatusServiceUnavailable {
		t.Fatalf("mixed-state read: %d", page.Code)
	}
	c.Close()
	disk, err := config.LoadConfigFile(c.configPath)
	if err != nil {
		t.Fatal(err)
	}
	// A committed SQL decision must roll forward even when disk still has the
	// before-policy facts; recovery patches only owned entries.
	disk.Library.StoragePolicies = cfg.Library.StoragePolicies
	disk.Tasks.RunSlots = 8
	writePolicyConfig(t, c.configPath, *disk)
	fresh := config.NewManager(disk)
	reopened := newControllerCore(store, scanner.NewScanner(store, fresh), fresh, c.configPath, defaultControllerCacheSizes())
	t.Cleanup(reopened.Close)
	op = awaitRelocationOperation(t, reopened, op.ID)
	if op.Status != "completed" {
		t.Fatalf("fresh recovery: %+v", op)
	}
	page = httptest.NewRecorder()
	reopened.servePageImage(page, requestWithRouteParams(http.MethodGet, "/page", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
	if page.Code != http.StatusOK || !bytes.Equal(page.Body.Bytes(), png1x1) {
		t.Fatalf("recovered read: %d", page.Code)
	}
	if config.ResolveStoragePolicy(reopened.currentConfig(), target).StorageProfile != "hdd_external" {
		t.Fatal("fresh policy missing")
	}
	if reopened.currentConfig().Tasks.RunSlots != 8 {
		t.Fatal("forward recovery lost unrelated setting")
	}
}

func TestRelocationPolicySQLFailureCompensatesAndAllowsNewConfirmation(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	cfg := c.currentConfig()
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: "hdd_external"}}
	if err := c.persistConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_relocation BEFORE UPDATE OF path ON books BEGIN SELECT RAISE(ABORT,'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	op := submitRelocation(t, c, plan.ID)
	op = awaitRelocationOperation(t, c, op.ID)
	if op.Status != "failed" || op.Code != "relocation_not_committed" {
		t.Fatalf("SQL failure: %+v", op)
	}
	disk, err := config.LoadConfigFile(c.configPath)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetBook(context.Background(), book.ID)
	if got.Path != book.Path || config.ResolveStoragePolicy(*disk, lib.Path).StorageProfile != "hdd_external" || config.ResolveStoragePolicy(c.currentConfig(), lib.Path).StorageProfile != "hdd_external" {
		t.Fatal("SQL failure did not compensate paths/policies")
	}
	if _, err = store.DB().Exec(`DROP TRIGGER reject_relocation`); err != nil {
		t.Fatal(err)
	}
	plan = startRelocationPreview(t, c, lib.ID, target)
	op = submitRelocation(t, c, plan.ID)
	if got := awaitRelocationOperation(t, c, op.ID); got.Status != "completed" {
		t.Fatalf("later confirmation: %+v", got)
	}
}

func TestRelocationPolicyPersistenceFailuresCompensateOrRemainBlocked(t *testing.T) {
	for _, boundary := range []string{"before_rename", "after_rename", "compensation"} {
		t.Run(boundary, func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
			seedBookArchivePages(t, c, book, png1x1)
			target := filepath.Join(root, "target")
			if err := os.Rename(lib.Path, target); err != nil {
				t.Fatal(err)
			}
			cfg := c.currentConfig()
			cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: "hdd_external"}}
			if err := c.persistConfig(&cfg); err != nil {
				t.Fatal(err)
			}
			c.Close()
			cfg = c.currentConfig()
			writes := 0
			writer := config.FileWriter(func(path string, data []byte, mode os.FileMode) error {
				writes++
				if boundary == "before_rename" && writes == 1 || boundary == "compensation" && writes == 2 {
					return errors.New("configuration device unavailable")
				}
				if err := config.AtomicWriteFile(path, data, mode); err != nil {
					return err
				}
				if boundary == "after_rename" && writes == 1 {
					return errors.New("directory sync failed after rename")
				}
				return nil
			})
			mgr := config.NewManager(&cfg, writer)
			current := newControllerCore(store, scanner.NewScanner(store, mgr), mgr, c.configPath, defaultControllerCacheSizes())
			t.Cleanup(current.Close)
			plan := startRelocationPreview(t, current, lib.ID, target)
			if boundary == "compensation" {
				if _, err := store.DB().Exec(`CREATE TRIGGER reject_relocation BEFORE UPDATE OF path ON books BEGIN SELECT RAISE(ABORT,'disk full'); END`); err != nil {
					t.Fatal(err)
				}
			}
			op := submitRelocation(t, current, plan.ID)
			op = awaitPolicyOperation(t, current, op.ID)
			want := "failed"
			if boundary == "compensation" {
				want = "prepared"
			}
			if op.Status != want {
				t.Fatalf("persistence result: %+v", op)
			}
			got, _ := store.GetBook(context.Background(), book.ID)
			if got.Path != book.Path {
				t.Fatal("uncommitted paths changed")
			}
			current.Close()
			disk, err := config.LoadConfigFile(c.configPath)
			if err != nil {
				t.Fatal(err)
			}
			fresh := config.NewManager(disk)
			reopened := newControllerCore(store, scanner.NewScanner(store, fresh), fresh, c.configPath, defaultControllerCacheSizes())
			t.Cleanup(reopened.Close)
			op = awaitPolicyOperation(t, reopened, op.ID)
			if op.Status != "failed" && op.Status != "interrupted" {
				t.Fatalf("compensation recovery: %+v", op)
			}
			if config.ResolveStoragePolicy(reopened.currentConfig(), lib.Path).StorageProfile != "hdd_external" {
				t.Fatal("compensation did not restore policy")
			}
		})
	}
}

func awaitPolicyOperation(t *testing.T, c *Controller, id int64) RelocationOperation {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		rec := httptest.NewRecorder()
		c.getRelocationOperation(rec, requestWithRouteParam(http.MethodGet, "/operation", nil, "operationId", strconv.FormatInt(id, 10)))
		var op RelocationOperation
		if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("operation: %d %v", rec.Code, err)
		}
		if op.Status != "pending" && op.Code != "" {
			return op
		}
		select {
		case <-deadline:
			t.Fatalf("unfinished policy operation: %+v", op)
		case <-time.After(time.Millisecond):
		}
	}
}

func writePolicyConfig(t *testing.T, path string, cfg config.Config) {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = config.AtomicWriteFile(path, data, config.ConfigFilePerm); err != nil {
		t.Fatal(err)
	}
}

func TestRelocationPolicyDivergentEditsStayBlockedAcrossRestartsAndHistoryPruning(t *testing.T) {
	for _, edit := range []string{"owned_policy", "default_profile", "default_io", "ancestor_policy"} {
		t.Run(edit, func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
			seedBookArchivePages(t, c, book, png1x1)
			_, _, otherBook := seedBookFixture(t, store, root, "other", "other-series", "other.cbz", 1)
			seedBookArchivePages(t, c, otherBook, png1x1)
			target := filepath.Join(root, "target")
			if err := os.Rename(lib.Path, target); err != nil {
				t.Fatal(err)
			}
			cfg := c.currentConfig()
			cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: "hdd_external"}}
			if err := c.persistConfig(&cfg); err != nil {
				t.Fatal(err)
			}
			plan := startRelocationPreview(t, c, lib.ID, target)
			c.relocation.ApplyConfig = func(*config.Config) error { return errors.New("runtime unavailable") }
			op := submitRelocation(t, c, plan.ID)
			op = awaitPolicyOperation(t, c, op.ID)
			if op.Status != "paths_committed" {
				t.Fatalf("expected committed: %+v", op)
			}
			c.Close()
			after, err := config.LoadConfigFile(c.configPath)
			if err != nil {
				t.Fatal(err)
			}
			diverged := config.CloneConfig(*after)
			diverged.Tasks.RunSlots = 11
			switch edit {
			case "owned_policy":
				diverged.Library.StoragePolicies[0].IOPolicy.ArchiveOpenConcurrency = 8
			case "default_profile":
				diverged.Library.StorageProfile = "network"
			case "ancestor_policy":
				diverged.Library.StoragePolicies = append(diverged.Library.StoragePolicies, config.LibraryStoragePolicy{Path: root, StorageProfile: "network"})
			case "default_io":
				diverged.Library.IOPolicy.ArchiveOpenConcurrency = 7
			}
			writePolicyConfig(t, c.configPath, diverged)
			// Recovery data belongs to relocation, rather than the short-lived run.
			if _, err = store.DB().Exec(`DELETE FROM runs WHERE id=?`, op.RunID); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				disk, err := config.LoadConfigFile(c.configPath)
				if err != nil {
					t.Fatal(err)
				}
				mgr := config.NewManager(disk)
				current := newControllerCore(store, scanner.NewScanner(store, mgr), mgr, c.configPath, defaultControllerCacheSizes())
				result := awaitPolicyOperation(t, current, op.ID)
				if result.Status != "paths_committed" || result.Code != "relocation_policy_recovery_conflict" {
					t.Fatalf("divergent recovery: %+v", result)
				}
				for _, item := range []struct {
					id     int64
					status int
				}{{book.ID, http.StatusServiceUnavailable}, {otherBook.ID, http.StatusOK}} {
					rec := httptest.NewRecorder()
					current.servePageImage(rec, requestWithRouteParams(http.MethodGet, "/page", nil, map[string]string{"bookId": strconv.FormatInt(item.id, 10), "pageNumber": "1"}))
					if rec.Code != item.status {
						t.Fatalf("scoped access: book %d status %d", item.id, rec.Code)
					}
				}
				current.Close()
			}
			// Explicitly restore the owned policy facts while retaining unrelated edits.
			after.Tasks.RunSlots = 11
			writePolicyConfig(t, c.configPath, *after)
			mgr := config.NewManager(after)
			recovered := newControllerCore(store, scanner.NewScanner(store, mgr), mgr, c.configPath, defaultControllerCacheSizes())
			t.Cleanup(recovered.Close)
			if got := awaitPolicyOperation(t, recovered, op.ID); got.Status != "completed" {
				t.Fatalf("resolved recovery: %+v", got)
			}
			if recovered.currentConfig().Tasks.RunSlots != 11 {
				t.Fatal("recovery lost unrelated edit")
			}
		})
	}
}

func TestRelocationPolicySerializesBrowserSaveAndReloadThroughPublication(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	cfg := c.currentConfig()
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: "hdd_external"}}
	if err := c.persistConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	entered, release := make(chan struct{}), make(chan struct{})
	c.relocation.ApplyConfig = func(next *config.Config) error { close(entered); <-release; return runtimecfg.Apply(next) }
	op := submitRelocation(t, c, plan.ID)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("runtime application not reached")
	}
	cfg.Tasks.RunSlots = 12
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	req := versionedConfigRequest(c, body, "/api/system/config")
	rec := httptest.NewRecorder()
	saveStarted, saveDone := make(chan struct{}), make(chan struct{})
	go func() { close(saveStarted); c.updateSystemConfig(rec, req); close(saveDone) }()
	reloadDone := make(chan error, 1)
	go func() { reloadDone <- c.config.Reload(c.configPath, runtimecfg.Apply, nil) }()
	<-saveStarted
	if config.ResolveStoragePolicy(c.currentConfig(), lib.Path).StorageProfile != "hdd_external" {
		t.Fatal("published configuration before runtime completed")
	}
	close(release)
	<-saveDone
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale browser save: %d %s", rec.Code, rec.Body.String())
	}
	if err := <-reloadDone; err != nil {
		t.Fatal(err)
	}
	if got := awaitPolicyOperation(t, c, op.ID); got.Status != "completed" {
		t.Fatalf("migration: %+v", got)
	}
	disk, err := config.LoadConfigFile(c.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.ResolveStoragePolicy(*disk, target).StorageProfile != "hdd_external" || config.ResolveStoragePolicy(c.currentConfig(), target).StorageProfile != "hdd_external" {
		t.Fatal("concurrent settings overwrote migration")
	}
}

func TestRelocationPolicyExternalEditDuringRuntimeIsPreservedAndBlocked(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	cfg := c.currentConfig()
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: "hdd_external"}}
	if err := c.persistConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	c.relocation.ApplyConfig = func(next *config.Config) error {
		edited := config.CloneConfig(*next)
		edited.Tasks.RunSlots = 13
		edited.Library.StoragePolicies[0].IOPolicy.ArchiveOpenConcurrency = 8
		data, err := yaml.Marshal(edited)
		if err != nil {
			return err
		}
		if err = config.AtomicWriteFile(c.configPath, data, config.ConfigFilePerm); err != nil {
			return err
		}
		return runtimecfg.Apply(next)
	}
	op := submitRelocation(t, c, plan.ID)
	op = awaitPolicyOperation(t, c, op.ID)
	if op.Status != "paths_committed" || op.Code != "relocation_recovering" {
		t.Fatalf("concurrent disk edit: %+v", op)
	}
	if err := c.config.Reload(c.configPath, runtimecfg.Apply, nil); err != nil {
		t.Fatal(err)
	}
	disk, err := config.LoadConfigFile(c.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Tasks.RunSlots != 13 || disk.Library.StoragePolicies[0].IOPolicy.ArchiveOpenConcurrency != 8 {
		t.Fatal("external file edit overwritten")
	}
	rec := httptest.NewRecorder()
	c.servePageImage(rec, requestWithRouteParams(http.MethodGet, "/page", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatal("Reload released unresolved migration")
	}
}

func TestRelocationPolicyExternalEditDuringEffectsRemainsBlocked(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	cfg := c.currentConfig()
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: "hdd_external"}}
	if err := c.persistConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	effects := c.relocation.Effects
	c.relocation.Effects = func(ctx context.Context, op RelocationOperation) error {
		if err := effects(ctx, op); err != nil {
			return err
		}
		edited := c.currentConfig()
		edited.Library.StoragePolicies[0].IOPolicy.ArchiveOpenConcurrency = 8
		edited.Tasks.RunSlots = 17
		writePolicyConfig(t, c.configPath, edited)
		return nil
	}
	op := submitRelocation(t, c, plan.ID)
	op = awaitPolicyOperation(t, c, op.ID)
	if op.Status != "paths_committed" {
		t.Fatalf("external edit released migration: %+v", op)
	}
	rec := httptest.NewRecorder()
	c.servePageImage(rec, requestWithRouteParams(http.MethodGet, "/page", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("mixed policy read: %d", rec.Code)
	}
}

func TestRelocationZeroPolicyPreviewRejectsUnpublishedDiskPolicy(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	seedBookArchivePages(t, c, book, png1x1)
	target := filepath.Join(root, "target")
	if err := os.Rename(lib.Path, target); err != nil {
		t.Fatal(err)
	}
	cfg := c.currentConfig()
	if err := c.persistConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if plan.PolicyCount != 0 {
		t.Fatal("fixture must have no mapped policy")
	}
	edited := config.CloneConfig(cfg)
	edited.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: filepath.Join(lib.Path, "series"), StorageProfile: "hdd_external"}}
	writePolicyConfig(t, c.configPath, edited)
	op := submitRelocation(t, c, plan.ID)
	op = awaitPolicyOperation(t, c, op.ID)
	if op.Status != "failed" || op.Code != "relocation_stale_plan" {
		t.Fatalf("unpublished policy committed: %+v", op)
	}
	got, err := store.GetBook(context.Background(), book.ID)
	if err != nil || got.Path != book.Path {
		t.Fatalf("stale plan changed book path: %v %s", err, got.Path)
	}
	current, err := store.GetLibrary(context.Background(), lib.ID)
	if err != nil || current.Path != lib.Path {
		t.Fatal("stale plan changed library path")
	}
	disk, err := config.LoadConfigFile(c.configPath)
	if err != nil || config.ResolveStoragePolicy(*disk, book.Path).StorageProfile != "hdd_external" {
		t.Fatal("external policy lost")
	}
	if len(c.currentConfig().Library.StoragePolicies) != 0 {
		t.Fatal("test accidentally reloaded disk edit")
	}
}
