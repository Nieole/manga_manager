package api

import (
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/metadata"
	"manga-manager/internal/organize"
)

// assembleIntelligenceUseCases binds external clients and module capabilities before launch inputs open.
func (c *apiFixture) assembleIntelligenceUseCases() {
	if c.providers == nil {
		c.providers = &providerHTTP{store: c.store, proposals: c.proposals, providerClients: &providerClients{currentConfig: c.currentConfig}}
	}
	c.intelligence = intelligence.New(c.taskEngine.runs, c.store, c.proposals, organize.NewAIGrouping(c.store, nil), intelligence.Sources{Metadata: c.providers.getMetadataFetcher, Grouping: func() intelligence.GroupingProvider {
		cfg := c.currentConfig()
		return metadata.NewAIProvider(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)
	}}, c.PublishEvent)
	if c.families.maintenance != nil {
		c.families.maintenance.intelligence = c.intelligence
		c.families.maintenance.getMetadataFetcher = c.providers.getMetadataFetcher
	}
	if c.runtime != nil {
		c.runtime.Intelligence = c.intelligence
	}
}
