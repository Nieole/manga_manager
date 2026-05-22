package config

import (
	"path/filepath"
	"runtime"
	"strings"
)

const (
	StorageProfileAuto        = "auto"
	StorageProfileSSD         = "ssd"
	StorageProfileHDDExternal = "hdd_external"
	StorageProfileNetwork     = "network"
	StorageProfileCustom      = "custom"
)

var SupportedStorageProfiles = []string{
	StorageProfileAuto,
	StorageProfileSSD,
	StorageProfileHDDExternal,
	StorageProfileNetwork,
	StorageProfileCustom,
}

type StorageIOPolicy struct {
	ScanConcurrency            int  `yaml:"scan_concurrency" json:"scan_concurrency"`
	ArchiveOpenConcurrency     int  `yaml:"archive_open_concurrency" json:"archive_open_concurrency"`
	CoverConcurrency           int  `yaml:"cover_concurrency" json:"cover_concurrency"`
	HashConcurrency            int  `yaml:"hash_concurrency" json:"hash_concurrency"`
	PauseBackgroundWhenReading bool `yaml:"pause_background_when_reading" json:"pause_background_when_reading"`
	IdleOnlyHeavyTasks         bool `yaml:"idle_only_heavy_tasks" json:"idle_only_heavy_tasks"`
	DisableSameDiskPageCache   bool `yaml:"disable_same_disk_page_cache" json:"disable_same_disk_page_cache"`
}

type LibraryStoragePolicy struct {
	Path           string          `yaml:"path" json:"path"`
	StorageProfile string          `yaml:"storage_profile" json:"storage_profile"`
	IOPolicy       StorageIOPolicy `yaml:"io_policy" json:"io_policy"`
}

type ResolvedStoragePolicy struct {
	Path           string          `json:"path"`
	StorageProfile string          `json:"storage_profile"`
	IOPolicy       StorageIOPolicy `json:"io_policy"`
	VolumeKey      string          `json:"volume_key"`
}

func NormalizeStorageProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case StorageProfileSSD:
		return StorageProfileSSD
	case StorageProfileHDDExternal:
		return StorageProfileHDDExternal
	case StorageProfileNetwork:
		return StorageProfileNetwork
	case StorageProfileCustom:
		return StorageProfileCustom
	default:
		return StorageProfileAuto
	}
}

func NormalizeLibraryStorageConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Library.StorageProfile = NormalizeStorageProfile(cfg.Library.StorageProfile)
	cfg.Library.IOPolicy = NormalizeStorageIOPolicy(cfg.Library.StorageProfile, cfg.Library.IOPolicy)
	for i := range cfg.Library.StoragePolicies {
		cfg.Library.StoragePolicies[i].Path = strings.TrimSpace(cfg.Library.StoragePolicies[i].Path)
		cfg.Library.StoragePolicies[i].StorageProfile = NormalizeStorageProfile(cfg.Library.StoragePolicies[i].StorageProfile)
		cfg.Library.StoragePolicies[i].IOPolicy = NormalizeStorageIOPolicy(
			cfg.Library.StoragePolicies[i].StorageProfile,
			cfg.Library.StoragePolicies[i].IOPolicy,
		)
	}
}

func NormalizeStorageIOPolicy(profile string, policy StorageIOPolicy) StorageIOPolicy {
	profile = NormalizeStorageProfile(profile)
	defaults := DefaultStorageIOPolicy(profile)

	if policy.ScanConcurrency <= 0 {
		policy.ScanConcurrency = defaults.ScanConcurrency
	}
	if policy.ArchiveOpenConcurrency <= 0 {
		policy.ArchiveOpenConcurrency = defaults.ArchiveOpenConcurrency
	}
	if policy.CoverConcurrency <= 0 {
		policy.CoverConcurrency = defaults.CoverConcurrency
	}
	if policy.HashConcurrency <= 0 {
		policy.HashConcurrency = defaults.HashConcurrency
	}

	if profile == StorageProfileHDDExternal || profile == StorageProfileNetwork {
		policy.PauseBackgroundWhenReading = true
		policy.IdleOnlyHeavyTasks = true
		policy.DisableSameDiskPageCache = true
	}

	return policy
}

func DefaultStorageIOPolicy(profile string) StorageIOPolicy {
	switch NormalizeStorageProfile(profile) {
	case StorageProfileHDDExternal, StorageProfileNetwork:
		return StorageIOPolicy{
			ScanConcurrency:            1,
			ArchiveOpenConcurrency:     1,
			CoverConcurrency:           1,
			HashConcurrency:            1,
			PauseBackgroundWhenReading: true,
			IdleOnlyHeavyTasks:         true,
			DisableSameDiskPageCache:   true,
		}
	case StorageProfileSSD:
		return StorageIOPolicy{
			ScanConcurrency:        0,
			ArchiveOpenConcurrency: 0,
			CoverConcurrency:       0,
			HashConcurrency:        0,
		}
	default:
		return StorageIOPolicy{
			ScanConcurrency:        0,
			ArchiveOpenConcurrency: 0,
			CoverConcurrency:       0,
			HashConcurrency:        0,
		}
	}
}

func ResolveStoragePolicy(cfg Config, path string) ResolvedStoragePolicy {
	NormalizeLibraryStorageConfig(&cfg)
	best := LibraryStoragePolicy{
		Path:           "",
		StorageProfile: cfg.Library.StorageProfile,
		IOPolicy:       cfg.Library.IOPolicy,
	}
	bestLen := -1
	for _, candidate := range cfg.Library.StoragePolicies {
		if candidate.Path == "" || !pathWithinRoot(path, candidate.Path) {
			continue
		}
		if l := len(filepath.Clean(candidate.Path)); l > bestLen {
			best = candidate
			bestLen = l
		}
	}

	if best.StorageProfile == "" {
		best.StorageProfile = StorageProfileAuto
	}
	best.StorageProfile = NormalizeStorageProfile(best.StorageProfile)
	best.IOPolicy = NormalizeStorageIOPolicy(best.StorageProfile, best.IOPolicy)
	return ResolvedStoragePolicy{
		Path:           best.Path,
		StorageProfile: best.StorageProfile,
		IOPolicy:       best.IOPolicy,
		VolumeKey:      VolumeKey(path),
	}
}

func SameVolume(a, b string) bool {
	av := VolumeKey(a)
	bv := VolumeKey(b)
	return av != "" && bv != "" && strings.EqualFold(av, bv)
}

func VolumeKey(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	abs, err := filepath.Abs(trimmed)
	if err == nil {
		trimmed = abs
	}
	cleaned := filepath.Clean(trimmed)
	volume := filepath.VolumeName(cleaned)
	if volume != "" {
		return strings.ToLower(volume)
	}
	if runtime.GOOS != "windows" && strings.HasPrefix(cleaned, string(filepath.Separator)) {
		parts := strings.Split(strings.TrimPrefix(cleaned, string(filepath.Separator)), string(filepath.Separator))
		if len(parts) > 0 && parts[0] != "" {
			return string(filepath.Separator) + parts[0]
		}
		return string(filepath.Separator)
	}
	return strings.ToLower(cleaned)
}

func pathWithinRoot(path, root string) bool {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(root) == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = filepath.Clean(path)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = filepath.Clean(root)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return strings.EqualFold(absPath, absRoot)
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
