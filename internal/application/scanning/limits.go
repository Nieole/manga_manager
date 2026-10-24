package scanning

import (
	"manga-manager/internal/config"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
)

// LimitsForPath describes the real scanner concurrency for a library path.
func LimitsForPath(cfg config.Config, path string) task.Limits {
	profile := scanner.NormalizeScanProfile(cfg.Scanner.ScanProfile)
	policy := config.ResolveStoragePolicy(cfg, path)
	return task.Limits{
		ScanProfile:                string(profile),
		ScannerWorkersConfigured:   cfg.Scanner.Workers,
		ScannerWorkersEffective:    scanner.WorkerCount(cfg, path, scanner.ScanOptions{Profile: profile}),
		StorageProfile:             policy.StorageProfile,
		VolumeKey:                  policy.VolumeKey,
		ScanConcurrency:            policy.IOPolicy.ScanConcurrency,
		ArchiveOpenConcurrency:     policy.IOPolicy.ArchiveOpenConcurrency,
		CoverConcurrency:           policy.IOPolicy.CoverConcurrency,
		HashConcurrency:            policy.IOPolicy.HashConcurrency,
		PauseBackgroundWhenReading: policy.IOPolicy.PauseBackgroundWhenReading,
		IdleOnlyHeavyTasks:         policy.IOPolicy.IdleOnlyHeavyTasks,
		DisableSameDiskPageCache:   policy.IOPolicy.DisableSameDiskPageCache,
	}
}
