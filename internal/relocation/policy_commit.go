package relocation

import (
	"context"
	"errors"
	"reflect"

	"manga-manager/internal/config"
)

var errPolicyRecoveryConflict = errors.New("relocation_policy_recovery_conflict")

// policyJournal owns only affected directory entries. Unrelated settings and
// credentials never enter durable recovery data or operation responses.
type policyJournal struct {
	Unowned        []config.LibraryStoragePolicy `json:"unowned"`
	Version        int                           `json:"version"`
	Keys           []string                      `json:"keys"`
	Before         []config.LibraryStoragePolicy `json:"before"`
	After          []config.LibraryStoragePolicy `json:"after"`
	DefaultProfile string                        `json:"default_profile"`
	DefaultIO      config.StorageIOPolicy        `json:"default_io"`
}

func (j policyJournal) owns(path string) bool {
	for _, key := range j.Keys {
		if pathKey(path) == key {
			return true
		}
	}
	return false
}

func (j policyJournal) entries(cfg config.Config) []config.LibraryStoragePolicy {
	result := []config.LibraryStoragePolicy{}
	for _, p := range cfg.Library.StoragePolicies {
		if j.owns(p.Path) {
			result = append(result, p)
		}
	}
	return result
}

func (j policyJournal) unowned(cfg config.Config) []config.LibraryStoragePolicy {
	result := []config.LibraryStoragePolicy{}
	for _, p := range cfg.Library.StoragePolicies {
		if !j.owns(p.Path) {
			result = append(result, p)
		}
	}
	return result
}

func (j policyJournal) matches(cfg config.Config, entries []config.LibraryStoragePolicy) bool {
	config.NormalizeLibraryStorageConfig(&cfg)
	return cfg.Library.StorageProfile == j.DefaultProfile && cfg.Library.IOPolicy == j.DefaultIO && reflect.DeepEqual(j.entries(cfg), entries) && reflect.DeepEqual(j.unowned(cfg), j.Unowned)
}

func (j policyJournal) patch(cfg config.Config, entries []config.LibraryStoragePolicy) config.Config {
	cfg = config.CloneConfig(cfg)
	result := make([]config.LibraryStoragePolicy, 0, len(cfg.Library.StoragePolicies)+len(entries))
	inserted := false
	for _, p := range cfg.Library.StoragePolicies {
		if j.owns(p.Path) {
			if !inserted {
				result = append(result, entries...)
				inserted = true
			}
			continue
		}
		result = append(result, p)
	}
	if !inserted {
		result = append(result, entries...)
	}
	cfg.Library.StoragePolicies = result
	return cfg
}

func (s *Service) withCommitConfig(fn func(config.Config, *config.UpdateSession) error) error {
	if s.ConfigPath == "" {
		return s.Config.WithSnapshot(func(cfg config.Config) error { return fn(cfg, nil) })
	}
	return s.Config.Coordinate(s.ConfigPath, s.ApplyConfig, func(session *config.UpdateSession) error { return fn(session.Snapshot(), session) })
}

func (s *Service) preparePolicies(ctx context.Context, op *RelocationOperation, cfg config.Config, session *config.UpdateSession) (*config.Config, error) {
	plan, err := s.Get(ctx, op.PlanID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		if plan.PolicyCount == 0 {
			return nil, nil
		}
		return nil, errors.New("configuration persistence unavailable")
	}
	disk, err := session.Read()
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(policyFacts(disk), policyFacts(cfg)) {
		return nil, ErrStalePlan
	}
	config.NormalizeLibraryStorageConfig(&disk)
	j := policyJournal{Version: 1, Keys: []string{}, Before: []config.LibraryStoragePolicy{}, After: []config.LibraryStoragePolicy{}, DefaultProfile: disk.Library.StorageProfile, DefaultIO: disk.Library.IOPolicy}
	for _, p := range disk.Library.StoragePolicies {
		if mapped, e := MapPath(op.SourceRoot, op.TargetRoot, p.Path); e == nil {
			j.Keys = append(j.Keys, pathKey(p.Path), pathKey(mapped))
		}
	}
	next := config.CloneConfig(disk)
	next.Library.StoragePolicies = MappedPolicies(disk, op.SourceRoot, op.TargetRoot)
	j.Before, j.After = j.entries(disk), j.entries(next)
	j.Unowned = j.unowned(disk)
	if err = s.operations().preparePolicy(ctx, op, j); err != nil {
		return nil, err
	}
	if plan.PolicyCount == 0 {
		return nil, nil
	}
	if err = session.Write(next); err != nil {
		return nil, err
	}
	return &next, nil
}

// reconcilePolicies uses the SQL decision to select compensation or completion.
// A divergent owned entry is preserved and keeps the library inaccessible.
func (s *Service) reconcilePolicies(ctx context.Context, op RelocationOperation, finish func(func() error) error) error {
	j, err := s.operations().policy(ctx, op.ID)
	if err != nil {
		return err
	}
	if j == nil {
		if finish != nil {
			return finish(func() error { return nil })
		}
		return nil
	}
	return s.Config.Coordinate(s.ConfigPath, s.ApplyConfig, func(session *config.UpdateSession) error {
		disk, e := session.Read()
		if e != nil {
			return e
		}
		if !j.matches(disk, j.Before) && !j.matches(disk, j.After) {
			return errPolicyRecoveryConflict
		}
		wanted := j.Before
		if op.Status == "paths_committed" {
			wanted = j.After
		}
		next := j.patch(disk, wanted)
		if !j.matches(disk, wanted) {
			if e = session.Write(next); e != nil {
				return e
			}
		}
		if e = session.Publish(next); e != nil {
			return e
		}
		if finish != nil {
			return finish(session.Verify)
		}
		return nil
	})
}
