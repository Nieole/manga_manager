package configuration_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
	"manga-manager/internal/application/configuration"
	"manga-manager/internal/config"
	"time"
)

func initialConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Config{}
	config.NormalizeConfig(&cfg)
	cfg.LLM.Model = "original"
	cfg.Database.Path = filepath.Join(t.TempDir(), "database.db")
	cfg.Cache.Dir = t.TempDir()
	return cfg
}

func writeConfig(t *testing.T, path string, cfg config.Config) {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.AtomicWriteFile(path, data, config.ConfigFilePerm); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsAndReloadSharePublicationAndRetainRestartRequirement(t *testing.T) {
	cfg := initialConfig(t)
	manager := config.NewManager(&cfg)
	path := filepath.Join(t.TempDir(), "config.yaml")
	service := configuration.New(manager, path, nil)
	next := config.CloneConfig(cfg)
	next.Server.Port++
	next.LLM.Model = "future"
	saved, err := service.SaveSettings(manager.Version(cfg), next)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Publication.Saved || !slices.Contains(saved.Publication.NextOperation, "llm.model") || !slices.Contains(saved.Publication.RestartRequired, "server.port") {
		t.Fatalf("publication=%+v", saved.Publication)
	}
	next.Logging.Level = "debug"
	writeConfig(t, path, next)
	reloaded, err := service.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Saved || !reloaded.Published || !slices.Contains(reloaded.Immediate, "logging.level") || !slices.Contains(reloaded.RestartRequired, "server.port") {
		t.Fatalf("reload=%+v", reloaded)
	}
	if _, err := service.SaveSettings(saved.Publication.Version, saved.Config); !errors.Is(err, config.ErrConflict) {
		t.Fatalf("stale save=%v", err)
	}
	next.Server.Port = cfg.Server.Port
	writeConfig(t, path, next)
	if _, err := service.Reload(); err != nil {
		t.Fatal(err)
	}
	if len(service.Status().RestartRequired) != 0 {
		t.Fatal("restored startup value still requires restart")
	}
}

func TestSuccessfulApplyCannotPublishOverConcurrentExternalEditor(t *testing.T) {
	for _, source := range []string{"settings", "reload"} {
		t.Run(source, func(t *testing.T) {
			cfg := initialConfig(t)
			manager := config.NewManager(&cfg)
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeConfig(t, path, cfg)
			entered, release := make(chan struct{}), make(chan struct{})
			applied := ""
			service := configuration.New(manager, path, func(next *config.Config) error {
				applied = next.LLM.Model
				if next.LLM.Model == "request" {
					close(entered)
					<-release
				}
				return nil
			})
			requested := config.CloneConfig(cfg)
			requested.LLM.Model = "request"
			if source == "reload" {
				writeConfig(t, path, requested)
			}
			done := make(chan error, 1)
			go func() {
				if source == "settings" {
					_, err := service.SaveSettings(manager.Version(cfg), requested)
					done <- err
				} else {
					_, err := service.Reload()
					done <- err
				}
			}()
			<-entered
			external := config.CloneConfig(cfg)
			external.LLM.Model = "external editor"
			writeConfig(t, path, external)
			close(release)
			if err := <-done; !errors.Is(err, config.ErrConflict) {
				t.Fatalf("external edit did not conflict: %v", err)
			}
			if manager.Snapshot().LLM.Model != cfg.LLM.Model || applied != cfg.LLM.Model {
				t.Fatal("conflicted publication changed Manager or active resources")
			}
			disk, err := config.LoadConfigFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if disk.LLM.Model != "external editor" {
				t.Fatal("external file was overwritten")
			}
		})
	}
}

func TestInvalidReloadLeavesEditedFileAndPublishedConfigurationSeparate(t *testing.T) {
	cfg := initialConfig(t)
	manager := config.NewManager(&cfg)
	path := filepath.Join(t.TempDir(), "config.yaml")
	service := configuration.New(manager, path, nil)
	cfg.Server.Port = -3
	writeConfig(t, path, cfg)
	if _, err := service.Reload(); err == nil {
		t.Fatal("invalid reload accepted")
	}
	if manager.Snapshot().Server.Port == -3 {
		t.Fatal("invalid config published")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("invalid edited file removed")
	}
}

func TestRealFileWatcherUsesTheSamePublicationAndVersionBoundary(t *testing.T) {
	cfg := initialConfig(t)
	manager := config.NewManager(&cfg)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, cfg)
	service := configuration.New(manager, path, nil)
	watch, err := config.StartPublicationWatcher(path, func() error { _, err := service.Reload(); return err })
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	edited := config.CloneConfig(cfg)
	edited.Logging.Level = "debug"
	edited.Server.Port++
	writeConfig(t, path, edited)
	deadline := time.After(5 * time.Second)
	for manager.Snapshot().Logging.Level != "debug" {
		select {
		case <-deadline:
			t.Fatal("file watcher did not publish edited config")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if _, err := service.SaveSettings(manager.Version(cfg), cfg); !errors.Is(err, config.ErrConflict) {
		t.Fatalf("stale settings overwrote reloaded config: %v", err)
	}
	if !slices.Contains(service.Status().RestartRequired, "server.port") {
		t.Fatal("watcher hid pending restart")
	}
}

func TestSettingsKeepMaskedSecretsAndVersionConflicts(t *testing.T) {
	cfg := initialConfig(t)
	cfg.LLM.APIKey = "private-value"
	manager := config.NewManager(&cfg)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, cfg)
	service := configuration.New(manager, path, nil)
	proposed := config.MaskSecrets(cfg)
	proposed.Logging.Level = "debug"
	if _, err := service.SaveSettings("stale", proposed); !errors.Is(err, config.ErrConflict) {
		t.Fatalf("stale version=%v", err)
	}
	result, err := service.SaveSettings(manager.Version(cfg), proposed)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := config.LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if disk.LLM.APIKey != "private-value" || result.Config.LLM.APIKey != "private-value" {
		t.Fatal("mask replaced saved secret")
	}
}
