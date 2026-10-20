package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/parser"
	"manga-manager/internal/runtimecfg"

	"gopkg.in/yaml.v3"
)

func TestConfigSaveRejectsStaleSnapshot(t *testing.T) {
	c, _, _, _ := newTestController(t)
	original := c.currentConfig()
	newer := config.CloneConfig(original)
	newer.Scanner.ArchivePoolSize = 9
	if err := c.persistConfigExpected(&original, &newer); err != nil {
		t.Fatal(err)
	}
	stale := config.CloneConfig(original)
	stale.Scanner.MaxAiConcurrency = 7
	if err := c.persistConfigExpected(&original, &stale); !errors.Is(err, config.ErrConflict) {
		t.Fatalf("stale save = %v, want conflict", err)
	}
	disk, err := config.LoadConfigFile(c.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Scanner.ArchivePoolSize != 9 || c.currentConfig().Scanner.ArchivePoolSize != 9 {
		t.Fatal("stale save overwrote accepted configuration")
	}
}

func TestConfigUpdatesSerializeSaveAndReload(t *testing.T) {
	c, _, _, _ := newTestController(t)
	original := c.currentConfig()
	if err := c.persistConfig(&original); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- c.config.Save(c.configPath, &original, func(next *config.Config) error {
			next.Scanner.ArchivePoolSize = 9
			return nil
		}, func(next *config.Config) error {
			close(entered)
			<-release
			return runtimecfg.Apply(next)
		}, nil)
	}()
	<-entered
	staleFinished, reloadFinished := make(chan error, 1), make(chan error, 1)
	go func() {
		stale := config.CloneConfig(original)
		stale.Scanner.MaxAiConcurrency = 7
		staleFinished <- c.persistConfigExpected(&original, &stale)
	}()
	go func() { reloadFinished <- c.config.Reload(c.configPath, runtimecfg.Apply, nil) }()
	if c.currentConfig().Scanner.ArchivePoolSize != 5 {
		t.Fatal("configuration published before application completed")
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := <-staleFinished; !errors.Is(err, config.ErrConflict) {
		t.Fatalf("stale concurrent save = %v", err)
	}
	if err := <-reloadFinished; err != nil {
		t.Fatal(err)
	}
	disk, err := config.LoadConfigFile(c.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Scanner.ArchivePoolSize != 9 || c.currentConfig().Scanner.ArchivePoolSize != 9 || parser.PoolMaxSize() != 9 {
		t.Fatal("save/reload disagreed on accepted configuration")
	}
}

func TestConfigFailedApplicationRestoresDiskAndRuntime(t *testing.T) {
	c, _, _, _ := newTestController(t)
	original := c.currentConfig()
	if err := c.persistConfig(&original); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("derived resource unavailable")
	err := c.config.Save(c.configPath, &original, func(next *config.Config) error {
		next.Scanner.ArchivePoolSize = 9
		return nil
	}, func(next *config.Config) error {
		if err := runtimecfg.Apply(next); err != nil {
			return err
		}
		if next.Scanner.ArchivePoolSize == 9 {
			return failure
		}
		return nil
	}, nil)
	if !errors.Is(err, failure) {
		t.Fatalf("save = %v", err)
	}
	disk, err := config.LoadConfigFile(c.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Scanner.ArchivePoolSize != 5 || c.currentConfig().Scanner.ArchivePoolSize != 5 || parser.PoolMaxSize() != 5 {
		t.Fatal("failed update was not restored")
	}
	original.Scanner.ArchivePoolSize = 8
	if err := c.persistConfig(&original); err != nil {
		t.Fatalf("save after failed application: %v", err)
	}
}

func TestConfigSaveDoesNotOverwritePendingExternalEdit(t *testing.T) {
	c, _, _, _ := newTestController(t)
	original := c.currentConfig()
	if err := c.persistConfig(&original); err != nil {
		t.Fatal(err)
	}
	external := config.CloneConfig(original)
	external.Scanner.ArchivePoolSize = 9
	data, err := yaml.Marshal(&external)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.AtomicWriteFile(c.configPath, data, config.ConfigFilePerm); err != nil {
		t.Fatal(err)
	}
	original.Scanner.MaxAiConcurrency = 7
	if err := c.persistConfig(&original); !errors.Is(err, config.ErrConflict) {
		t.Fatalf("save before reload = %v", err)
	}
	if err := c.config.Reload(c.configPath, runtimecfg.Apply, nil); err != nil {
		t.Fatal(err)
	}
	if c.currentConfig().Scanner.ArchivePoolSize != 9 {
		t.Fatal("external edit lost")
	}
}

func TestConfigPersistenceFailureAllowsLaterSave(t *testing.T) {
	c, _, _, _ := newTestController(t)
	original := c.currentConfig()
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := config.CloneConfig(original)
	bad.Cache.Dir = filepath.Join(blocker, "cache")
	if err := c.persistConfig(&bad); err == nil {
		t.Fatal("failed persistence reported success")
	}
	if c.currentConfig().Cache.Dir != original.Cache.Dir {
		t.Fatal("failed persistence published configuration")
	}
	if err := c.persistConfig(&original); err != nil {
		t.Fatalf("subsequent save: %v", err)
	}
}

type delayedConfigBody struct {
	reader  *bytes.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *delayedConfigBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered); <-b.release })
	return b.reader.Read(p)
}

func TestConfigHTTPOverlappingSaveReturnsConflict(t *testing.T) {
	c, _, _, _ := newTestController(t)
	old := c.currentConfig()
	first := config.CloneConfig(old)
	first.Scanner.MaxAiConcurrency = 7
	firstData, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	body := &delayedConfigBody{reader: bytes.NewReader(firstData), entered: make(chan struct{}), release: make(chan struct{})}
	firstRequest := httptest.NewRequest(http.MethodPut, "/api/system/config", body)
	firstRequest.Header.Set("If-Match", c.config.Version(old))
	firstResponse := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); c.updateSystemConfig(firstResponse, firstRequest) }()
	<-body.entered
	second := config.CloneConfig(old)
	second.Scanner.ArchivePoolSize = 9
	secondData, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	secondResponse := httptest.NewRecorder()
	c.updateSystemConfig(secondResponse, versionedConfigRequest(c, secondData, "/api/system/config"))
	close(body.release)
	<-done
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("accepted save = %d: %s", secondResponse.Code, secondResponse.Body.String())
	}
	if firstResponse.Code != http.StatusConflict {
		t.Fatalf("overlapping stale save = %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
	if c.currentConfig().Scanner.ArchivePoolSize != 9 {
		t.Fatal("overlapping request lost accepted setting")
	}
}

func versionedConfigRequest(c *Controller, data []byte, path string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("If-Match", c.config.Version(c.currentConfig()))
	return req
}

func TestConfigHTTPRejectsCompletedUpdateFromStaleBrowser(t *testing.T) {
	for _, endpoint := range []string{"/api/system/config", "/api/system/koreader"} {
		t.Run(endpoint, func(t *testing.T) {
			c, _, _, _ := newTestController(t)
			get := httptest.NewRecorder()
			c.getSystemConfig(get, httptest.NewRequest(http.MethodGet, "/api/system/config", nil))
			oldVersion := get.Header().Get("ETag")
			if oldVersion == "" {
				t.Fatal("GET omitted expected-state token")
			}
			cfg := c.currentConfig()
			cfg.Scanner.ArchivePoolSize = 9
			if err := c.persistConfig(&cfg); err != nil {
				t.Fatal(err)
			}
			var body []byte
			if endpoint == "/api/system/config" {
				body, _ = json.Marshal(cfg)
			} else {
				body = []byte(`{"enabled":true,"base_path":"/koreader","match_mode":"binary_hash"}`)
			}
			req := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
			req.Header.Set("If-Match", oldVersion)
			rec := httptest.NewRecorder()
			if endpoint == "/api/system/config" {
				c.updateSystemConfig(rec, req)
			} else {
				c.updateKOReaderSettings(rec, req)
			}
			if rec.Code != http.StatusConflict {
				t.Fatalf("stale browser save = %d: %s", rec.Code, rec.Body.String())
			}
			req = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
			rec = httptest.NewRecorder()
			if endpoint == "/api/system/config" {
				c.updateSystemConfig(rec, req)
			} else {
				c.updateKOReaderSettings(rec, req)
			}
			if rec.Code != http.StatusPreconditionRequired {
				t.Fatalf("unversioned save = %d: %s", rec.Code, rec.Body.String())
			}
			if c.currentConfig().Scanner.ArchivePoolSize != 9 {
				t.Fatal("stale browser changed accepted configuration")
			}
		})
	}
}

func TestConfigFailedReloadCanBeFollowedBySave(t *testing.T) {
	c, _, _, _ := newTestController(t)
	original := c.currentConfig()
	if err := c.persistConfig(&original); err != nil {
		t.Fatal(err)
	}
	external := config.CloneConfig(original)
	external.Scanner.ArchivePoolSize = 9
	data, _ := yaml.Marshal(external)
	if err := config.AtomicWriteFile(c.configPath, data, config.ConfigFilePerm); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("runtime apply failed")
	if err := c.config.Reload(c.configPath, func(next *config.Config) error {
		if err := runtimecfg.Apply(next); err != nil {
			return err
		}
		if next.Scanner.ArchivePoolSize == 9 {
			return failure
		}
		return nil
	}, nil); !errors.Is(err, failure) {
		t.Fatalf("reload=%v", err)
	}
	original.Scanner.MaxAiConcurrency = 6
	if err := c.persistConfig(&original); err != nil {
		t.Fatalf("save after failed reload: %v", err)
	}
}

func TestConfigQueueReleaseWaitsForApplication(t *testing.T) {
	c, _, _, _ := newTestController(t)
	setSlots(t, c, 1)
	seedTask(t, c.taskEngine, scanSeed(1))
	queued := scanSeed(2)
	if _, err := trySeedTask(t, c.taskEngine, queued); err != errSeededRunQueued {
		t.Fatalf("queue = %v", err)
	}
	original := c.currentConfig()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- c.config.Save(c.configPath, &original, func(next *config.Config) error { next.Tasks.RunSlots = 3; return nil }, func(next *config.Config) error { close(entered); <-release; return runtimecfg.Apply(next) }, nil)
	}()
	<-entered
	if got := currentTask(t, c.taskEngine, queued.Key).Status; got != "queued" {
		t.Fatalf("released before application: %s", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := currentTask(t, c.taskEngine, queued.Key).Status; got == "queued" {
		t.Fatal("queue not released after publication")
	}
}
