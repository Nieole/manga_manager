package configuration_test

import (
	"errors"
	"manga-manager/internal/application/configuration"
	"manga-manager/internal/config"
	"path/filepath"
	"testing"
)

func TestKOReaderSettingsSaveOwnsValidationVersionAndSection(t *testing.T) {
	cfg := initialConfig(t)
	manager := config.NewManager(&cfg)
	service := configuration.New(manager, filepath.Join(t.TempDir(), "settings.yaml"), nil)
	in := configuration.KOReaderSettings{Enabled: true, BasePath: " relative ", MatchMode: "binary"}
	_, err := service.SaveKOReaderSettings(manager.Version(cfg), in)
	var invalid *configuration.Invalid
	if !errors.As(err, &invalid) || manager.Snapshot().KOReader.Enabled != cfg.KOReader.Enabled {
		t.Fatalf("invalid settings published: %v", err)
	}
	in.BasePath = " /device "
	in.MatchMode = config.KOReaderMatchModeFilePath
	saved, err := service.SaveKOReaderSettings(manager.Version(cfg), in)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Publication.Saved || !saved.Publication.Published || manager.Snapshot().KOReader.BasePath != "/device" || manager.Snapshot().LLM.Model != "original" {
		t.Fatalf("section publication: %+v", saved)
	}
	if _, err := service.SaveKOReaderSettings(manager.Version(cfg), in); !errors.Is(err, config.ErrConflict) {
		t.Fatalf("stale settings: %v", err)
	}
	if _, err := service.SaveKOReaderSettings("", in); !errors.Is(err, configuration.ErrVersionRequired) {
		t.Fatalf("unversioned settings: %v", err)
	}
}
