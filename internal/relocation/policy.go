package relocation

import (
	"context"
	"strconv"

	"manga-manager/internal/config"
)

type PolicyFacts struct {
	StorageProfile  string                        `json:"storage_profile"`
	IOPolicy        config.StorageIOPolicy        `json:"io_policy"`
	StoragePolicies []config.LibraryStoragePolicy `json:"storage_policies"`
}

func policyFacts(cfg config.Config) PolicyFacts {
	return PolicyFacts{StorageProfile: cfg.Library.StorageProfile, IOPolicy: cfg.Library.IOPolicy, StoragePolicies: cfg.Library.StoragePolicies}
}

func samePolicy(a, b config.LibraryStoragePolicy) bool {
	return a.StorageProfile == b.StorageProfile && a.IOPolicy == b.IOPolicy
}

// MappedPolicies returns only directory policy configuration. Ancestors and
// unrelated policies retain their existing order and values.
func MappedPolicies(cfg config.Config, source, target string) []config.LibraryStoragePolicy {
	cfg = config.CloneConfig(cfg)
	config.NormalizeLibraryStorageConfig(&cfg)
	result := make([]config.LibraryStoragePolicy, 0, len(cfg.Library.StoragePolicies))
	moved := make([]config.LibraryStoragePolicy, 0)
	for _, policy := range cfg.Library.StoragePolicies {
		mapped, err := MapPath(source, target, policy.Path)
		if err != nil {
			result = append(result, policy)
			continue
		}
		policy.Path = mapped
		moved = append(moved, policy)
	}
	for _, policy := range moved {
		duplicate := false
		for _, existing := range result {
			if pathKey(existing.Path) == pathKey(policy.Path) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, policy)
		}
	}
	return result
}

func (s *Service) checkPolicies(ctx context.Context, plan *RelocationPlan, cfg config.Config) error {
	config.NormalizeLibraryStorageConfig(&cfg)
	for _, policy := range cfg.Library.StoragePolicies {
		mapped, err := MapPath(plan.SourceRoot, plan.TargetRoot, policy.Path)
		if err != nil {
			continue
		}
		detail := RelocationDetail{Kind: "policy", RecordType: "policy", SourcePath: policy.Path, TargetPath: mapped, Reason: policy.StorageProfile}
		if err = s.add(ctx, plan, detail); err != nil {
			return err
		}
		for _, existing := range cfg.Library.StoragePolicies {
			if pathKey(existing.Path) == pathKey(mapped) && pathKey(existing.Path) != pathKey(policy.Path) && !samePolicy(existing, policy) {
				detail.Kind, detail.Reason = "blocker", "policy_conflict"
				if err = s.add(ctx, plan, detail); err != nil {
					return err
				}
			}
		}
	}
	after := config.CloneConfig(cfg)
	after.Library.StoragePolicies = MappedPolicies(cfg, plan.SourceRoot, plan.TargetRoot)
	// The roots catch inheritance changes even in empty nested libraries. Stored
	// record paths also catch descendants with more specific policy overrides.
	rows, err := s.Store.DB().QueryContext(ctx, `SELECT id,path FROM libraries WHERE id<>?
UNION SELECT library_id,path FROM series WHERE library_id<>?
UNION SELECT library_id,path FROM books WHERE library_id<>?`, plan.LibraryID, plan.LibraryID, plan.LibraryID)
	if err != nil {
		return err
	}
	type affected struct {
		id   int64
		path string
	}
	changes := []affected{}
	seen := map[string]bool{}
	consider := func(item affected) {
		key := strconv.FormatInt(item.id, 10) + ":" + pathKey(item.path)
		if seen[key] {
			return
		}
		seen[key] = true
		old := config.ResolveStoragePolicy(cfg, item.path)
		next := config.ResolveStoragePolicy(after, item.path)
		if old.StorageProfile != next.StorageProfile || old.IOPolicy != next.IOPolicy {
			changes = append(changes, item)
		}
	}
	for rows.Next() {
		var item affected
		if err = rows.Scan(&item.id, &item.path); err != nil {
			rows.Close()
			return err
		}
		consider(item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	libraries, err := s.Store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	boundaries := append(append([]config.LibraryStoragePolicy{}, cfg.Library.StoragePolicies...), after.Library.StoragePolicies...)
	for _, library := range libraries {
		if library.ID == plan.LibraryID {
			continue
		}
		// Policy functions are piecewise constant. Root and each intersecting
		// policy boundary cover even descendants which have no stored records.
		consider(affected{id: library.ID, path: library.Path})
		for _, policy := range boundaries {
			if _, mapErr := MapPath(library.Path, library.Path, policy.Path); mapErr == nil {
				consider(affected{id: library.ID, path: policy.Path})
			}
		}
	}
	for _, item := range changes {
		if err = s.add(ctx, plan, RelocationDetail{Kind: "blocker", RecordType: "library", RecordID: item.id, TargetPath: item.path, Reason: "policy_affects_other_library"}); err != nil {
			return err
		}
	}
	return nil
}
