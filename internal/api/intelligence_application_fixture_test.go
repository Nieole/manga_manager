package api

import (
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/metadata"
	"manga-manager/internal/organize"
)

// assembleIntelligenceUseCases binds external clients and module capabilities before launch inputs open.
func (c *Controller) assembleIntelligenceUseCases() {
	c.intelligence = intelligence.New(c.taskEngine.runs, c.store, c.proposals, organize.NewAIGrouping(c.store, nil), intelligence.Sources{Metadata: c.getProvider, Grouping: func() intelligence.GroupingProvider {
		cfg := c.currentConfig()
		return metadata.NewAIProvider(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)
	}}, c.PublishEvent)
	if c.runtime != nil {
		c.runtime.Intelligence = c.intelligence
	}
}
