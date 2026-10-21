package api

import (
	"errors"
	"testing"

	"manga-manager/internal/config"

	"gopkg.in/yaml.v3"
)

func TestConfigFailedUpdatePreservesConcurrentExternalEditor(t *testing.T) {
	for _, kind := range []string{"save", "reload"} {
		t.Run(kind, func(t *testing.T) {
			c, _, _, _ := newTestController(t)
			original := c.currentConfig()
			if err := c.persistConfig(&original); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("runtime failed")
			apply := func(next *config.Config) error {
				if next.Tasks.RunSlots == original.Tasks.RunSlots {
					return nil
				}
				edited := config.CloneConfig(*next)
				edited.Tasks.RunSlots = 19
				data, err := yaml.Marshal(edited)
				if err != nil {
					return err
				}
				if err = config.AtomicWriteFile(c.configPath, data, config.ConfigFilePerm); err != nil {
					return err
				}
				return failure
			}
			var err error
			if kind == "save" {
				err = c.config.Save(c.configPath, &original, func(next *config.Config) error { next.Tasks.RunSlots = 7; return nil }, apply, nil)
			} else {
				edited := config.CloneConfig(original)
				edited.Tasks.RunSlots = 7
				writePolicyConfig(t, c.configPath, edited)
				err = c.config.Reload(c.configPath, apply, nil)
			}
			if !errors.Is(err, failure) {
				t.Fatalf("expected runtime failure: %v", err)
			}
			disk, err := config.LoadConfigFile(c.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if disk.Tasks.RunSlots != 19 {
				t.Fatalf("compensation overwrote external edit: %d", disk.Tasks.RunSlots)
			}
		})
	}
}
