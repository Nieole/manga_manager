package api

import (
	"manga-manager/internal/application/relocating"
	"manga-manager/internal/runtimecfg"
)

// assembleRelocationUseCases binds application effects to the existing domain and engine.
func (c *Controller) assembleRelocationUseCases() {
	c.moves = relocating.New(c.relocation, c.taskEngine.runs, c.taskEngine.engine, c.taskEngine.runStore, c.store, relocating.Effects{
		PurgeReadingPaths:  c.purgeReadingPathCaches,
		InvalidateStats:    c.invalidateDashboardStatsCache,
		InvalidateExternal: c.external.InvalidateLibrary,
		Watcher: func() (relocating.Watcher, error) {
			if c.watcher == nil {
				return nil, c.watcherInitErr
			}
			return c.watcher, c.watcherInitErr
		},
	})
	c.relocation.ConfigPath = c.configPath
	c.relocation.ApplyConfig = runtimecfg.Apply
}
