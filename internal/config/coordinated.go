package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// UpdateSession holds the same update boundary as Save and Reload. Durable
// participants decide compensation themselves; Publish never rolls back disk.
type UpdateSession struct {
	manager  *Manager
	path     string
	apply    func(*Config) error
	observed []byte
	existed  bool
}

// Coordinate keeps configuration persistence, an external durable decision and
// runtime publication serialized. The callback must not reenter Manager updates.
func (m *Manager) Coordinate(path string, apply func(*Config) error, fn func(*UpdateSession) error) error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	s := &UpdateSession{manager: m, path: path, apply: apply}
	return fn(s)
}

func (s *UpdateSession) Snapshot() Config { return s.manager.Snapshot() }

// Read observes the actual file, including edits not yet published by Reload.
// A missing file uses the current snapshot, as on first configuration save.
func (s *UpdateSession) Read() (Config, error) {
	data, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return Config{}, err
	}
	s.existed = err == nil
	s.observed = data
	if !s.existed {
		return s.Snapshot(), nil
	}
	cfg, err := LoadConfigFile(s.path)
	if err != nil {
		return Config{}, err
	}
	if result := ValidateConfigValues(cfg); !result.Valid {
		return Config{}, fmt.Errorf("invalid configuration: %s", FormatValidationIssues(result.Issues))
	}
	return *cfg, nil
}

// Verify rejects edits made outside the serialized configuration boundary.
func (s *UpdateSession) Verify() error {
	data, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if (err == nil) != s.existed || !bytes.Equal(data, s.observed) {
		return ErrConflict
	}
	return nil
}

// Write checks that the observed file is still present before replacing it.
// External edits are never restored from an older full-configuration preimage.
func (s *UpdateSession) Write(next Config) error {
	NormalizeConfig(&next)
	if result := ValidateConfigValues(&next); !result.Valid {
		return fmt.Errorf("invalid configuration: %s", FormatValidationIssues(result.Issues))
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
	if err = s.Verify(); err != nil {
		return err
	}
	if err = s.manager.writeFile(s.path, data, ConfigFilePerm); err != nil {
		return err
	}
	s.observed, s.existed = data, true
	return s.Verify()
}

// Publish checks for concurrent file edits both before and after applying the
// runtime. On failure the durable participant must keep its access restriction.
func (s *UpdateSession) Publish(next Config) error {
	if err := s.Verify(); err != nil {
		return err
	}
	if s.apply != nil {
		if err := s.apply(&next); err != nil {
			return err
		}
	}
	if err := s.Verify(); err != nil {
		return err
	}
	s.manager.replace(&next)
	if s.manager.afterUpdate != nil {
		s.manager.afterUpdate()
	}
	return nil
}
