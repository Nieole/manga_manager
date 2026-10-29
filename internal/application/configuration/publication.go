// Package configuration publishes settings and file reloads through the shared Manager.
// File and cross-durable-participant compensation remain owned by config.Manager.
package configuration

import (
	"encoding/json"
	"errors"
	"fmt"
	"manga-manager/internal/config"
)

// ErrVersionRequired prevents a settings document from overwriting an unseen configuration.
var ErrVersionRequired = errors.New("configuration version is required")

// Invalid describes configuration validation without changing persistence or publication.
type Invalid struct{ Validation config.ValidationResult }

func (e *Invalid) Error() string { return "configuration validation failed" }

// Publication separates persistence and Manager publication from consumer timing.
type Publication struct {
	// Saved is true when this command persisted the file; reload observes an external save.
	Saved bool `json:"saved"`
	// Published refers to the Manager snapshot, independently of restart-bound resources.
	Published       bool     `json:"published"`
	Version         string   `json:"version"`
	Immediate       []string `json:"immediate"`
	NextOperation   []string `json:"next_operation"`
	RestartRequired []string `json:"restart_required"`
}

// Result returns the exact configuration published by this serialized save.
type Result struct {
	Config      config.Config
	Validation  config.ValidationResult
	Publication Publication
}

// Service owns publication entry points; Manager remains the sole mutable configuration source.
type Service struct {
	manager *config.Manager
	path    string
	apply   func(*config.Config) error
	startup map[string]string
}

// New captures only restart-bound resource facts, not a second live configuration.
func New(manager *config.Manager, path string, apply func(*config.Config) error) *Service {
	s := &Service{manager: manager, path: path, apply: apply}
	s.startup = restartFacts(manager.Snapshot())
	return s
}

func restartFacts(cfg config.Config) map[string]string {
	resume := config.DefaultResumeAfterRestart
	if cfg.Tasks.ResumeAfterRestart != nil {
		resume = *cfg.Tasks.ResumeAfterRestart
	}
	origins, _ := json.Marshal(cfg.Server.AllowedOrigins)
	return map[string]string{"server.host": cfg.Server.Host, "server.port": fmt.Sprint(cfg.Server.Port), "server.allowed_origins": string(origins), "database.path": cfg.Database.Path, "tasks.resume_after_restart": fmt.Sprint(resume)}
}

// DatabasePath identifies the active connection's startup location, independent of a pending saved path.
func (s *Service) DatabasePath() string { return s.startup["database.path"] }

func (s *Service) describe(before, after config.Config, saved bool) Publication {
	p := Publication{Saved: saved, Published: true, Version: s.manager.Version(after), Immediate: []string{}, NextOperation: []string{}, RestartRequired: []string{}}
	for _, effect := range config.ChangedFieldEffects(before, after) {
		switch effect.Scope {
		case "immediate":
			p.Immediate = append(p.Immediate, effect.Field)
		case "next_operation":
			p.NextOperation = append(p.NextOperation, effect.Field)
		}
	}
	facts := restartFacts(after)
	for _, effect := range config.FieldEffects() {
		if effect.Scope == "restart" && facts[effect.Field] != s.startup[effect.Field] {
			p.RestartRequired = append(p.RestartRequired, effect.Field)
		}
	}
	return p
}

// Snapshot keeps the published document and its version/effect status in one read.
type Snapshot struct {
	Config      config.Config
	Publication Publication
}

// Read returns one Manager generation and the effect status belonging to that generation.
func (s *Service) Read() Snapshot {
	current := s.manager.Snapshot()
	return Snapshot{Config: current, Publication: s.describe(current, current, false)}
}

// Status reports pending restart effects against resources created for this process.
func (s *Service) Status() Publication { return s.Read().Publication }

// SaveSettings validates a masked client document and its expected version before saving.
func (s *Service) SaveSettings(version string, proposed config.Config) (Result, error) {
	expected := s.manager.Snapshot()
	config.RestoreMaskedSecrets(&proposed, expected)
	config.NormalizeConfig(&proposed)
	validation := config.ValidateConfig(&proposed)
	if !validation.Valid {
		return Result{Validation: validation}, &Invalid{validation}
	}
	if version == "" {
		return Result{}, ErrVersionRequired
	}
	if version != s.manager.Version(expected) {
		return Result{}, config.ErrConflict
	}
	result, err := s.SaveExpected(&expected, proposed)
	result.Validation = validation
	return result, err
}

// SaveExpected preserves Manager's save-specific persistence and rollback policy.
func (s *Service) SaveExpected(expected *config.Config, next config.Config) (Result, error) {
	var result Result
	var before config.Config
	err := s.manager.Save(s.path, expected, func(current *config.Config) error {
		before = config.CloneConfig(*current)
		*current = config.CloneConfig(next)
		return nil
	}, s.apply, func() {
		result.Config = s.manager.Snapshot()
		result.Publication = s.describe(before, result.Config, true)
	})
	return result, err
}

// Reload preserves Manager's file-reload validation and compensation policy.
func (s *Service) Reload() (Publication, error) {
	before := s.manager.Snapshot()
	var result Publication
	err := s.manager.Reload(s.path, func(next *config.Config) error {
		before = s.manager.Snapshot()
		if s.apply != nil {
			return s.apply(next)
		}
		return nil
	}, func() { result = s.describe(before, s.manager.Snapshot(), false) })
	if err == nil && !result.Published {
		result = s.Status()
	}
	return result, err
}
