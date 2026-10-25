package api

import (
	"context"
	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/organize"
	"manga-manager/internal/task"
)

func (c *Controller) launchBatchScrapeAllSeriesTask(ctx context.Context, key string, trigger task.Trigger) error {
	_, err := c.intelligence.ScrapeAll(ctx, key, trigger)
	return err
}
func (c *Controller) launchLibraryScrapeTask(ctx context.Context, id int64, key string, trigger task.Trigger) error {
	_, err := c.intelligence.ScrapeLibrary(ctx, id, key, trigger)
	return err
}
func (c *Controller) launchAIGroupingTask(id int64, locale string, trigger task.Trigger) error {
	_, err := c.intelligence.GroupLibrary(context.Background(), id, locale, trigger)
	return err
}
func (c *Controller) createAIGroupingReview(ctx context.Context, id int64, provider string, candidates []metadata.CandidateSeries, groups []metadata.AIGroupCollection) (database.AiGroupingReview, int, error) {
	return organize.NewAIGrouping(c.store, nil).QueueGenerated(ctx, id, provider, candidates, groups)
}
