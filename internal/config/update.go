package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ErrConflict means an expected configuration or the on-disk configuration changed.
var ErrConflict = errors.New("configuration changed; reload before saving")

// WithSnapshot holds the update boundary without writing or publishing config.
// The callback must not call Save, Reload or Replace on this manager.
func (m *Manager) WithSnapshot(fn func(Config) error) error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	return fn(m.Snapshot())
}

// Save serializes read, expected-state validation, mutation, persistence and application.
// A nil expected value mutates the latest state. Callbacks must not call Save, Reload
// or Replace on this Manager. Snapshot remains available during callbacks.
// after runs only after successful publication, before another update may start.
func (m *Manager) Save(path string, expected *Config, mutate func(*Config) error, apply func(*Config) error, after func()) error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	current := m.Snapshot()
	if expected != nil && !configsEqual(expected, &current) {
		return ErrConflict
	}
	previous, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	existed := err == nil
	if existed {
		disk, err := LoadConfigFile(path)
		if err != nil || !configsEqual(disk, &current) {
			return ErrConflict
		}
	}
	next := CloneConfig(current)
	if err := mutate(&next); err != nil {
		return err
	}
	NormalizeConfig(&next)
	if result := ValidateConfigValues(&next); !result.Valid {
		return fmt.Errorf("configuration validation failed: %s", FormatValidationIssues(result.Issues))
	}
	if err := os.MkdirAll(filepath.Dir(next.Database.Path), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(next.Cache.Dir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&next)
	if err != nil {
		return err
	}
	if err := m.writeFile(path, data, ConfigFilePerm); err != nil {
		return err
	}
	if !configsEqual(&next, &current) && apply != nil {
		if err := apply(&next); err != nil {
			var restoreErr error
			if verifyErr := verifyConfigFile(path, data); verifyErr != nil {
				restoreErr = verifyErr
			} else if existed {
				restoreErr = m.writeFile(path, previous, ConfigFilePerm)
			} else {
				restoreErr = os.Remove(path)
			}
			return errors.Join(err, restoreErr, apply(&current))
		}
	}
	m.replace(&next)
	if after != nil {
		after()
	}
	if m.afterUpdate != nil {
		m.afterUpdate()
	}
	return nil
}

// Reload reads the file inside the same boundary as Save. Failed application
// restores the published configuration to disk and derived resources. Validation
// failures leave the edited file available for correction without publishing it.
func (m *Manager) Reload(path string, apply func(*Config) error, after func()) error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	observed, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, err := LoadConfigFile(path)
	if err != nil {
		return err
	}
	if result := ValidateConfigValues(next); !result.Valid {
		return fmt.Errorf("configuration validation failed: %s", FormatValidationIssues(result.Issues))
	}
	current := m.Snapshot()
	if configsEqual(next, &current) {
		return nil
	}
	if apply != nil {
		if err := apply(next); err != nil {
			data, marshalErr := yaml.Marshal(current)
			var restoreErr error
			if verifyErr := verifyConfigFile(path, observed); verifyErr != nil {
				restoreErr = verifyErr
			} else if marshalErr == nil {
				restoreErr = m.writeFile(path, data, ConfigFilePerm)
			}
			return errors.Join(err, marshalErr, restoreErr, apply(&current))
		}
	}
	m.replace(next)
	if after != nil {
		after()
	}
	if m.afterUpdate != nil {
		m.afterUpdate()
	}
	return nil
}

// verifyConfigFile protects compensation from overwriting an external editor.
func verifyConfigFile(path string, observed []byte) error {
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, observed) {
		return ErrConflict
	}
	return nil
}
