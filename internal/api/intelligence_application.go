package api

import (
	"context"
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/metadata"
	"manga-manager/internal/organize"
	"manga-manager/internal/task"
)

// assembleIntelligenceUseCases binds external clients and module capabilities before launch inputs open.
func (c *Controller) assembleIntelligenceUseCases() {
	c.intelligence = intelligence.New(c.taskEngine.runs, c.store, c.proposals, organize.NewAIGrouping(c.store, nil), intelligence.Sources{Metadata: c.getProvider, Grouping: func() intelligence.GroupingProvider {
		cfg := c.currentConfig()
		return metadata.NewAIProvider(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)
	}}, c.PublishEvent)
}
func (c *Controller) restartIntelligenceRun(ctx context.Context, run RunSnapshot, trigger task.Trigger) error {
	identity := task.Identity{Type: task.Type(run.Type), Scope: task.Scope(run.Scope), Variant: task.Variant(run.Variant)}
	if run.ScopeID != nil {
		identity.ScopeID = *run.ScopeID
	}
	_, err := c.intelligence.Restart(ctx, identity, run.Params, trigger)
	return err
}
