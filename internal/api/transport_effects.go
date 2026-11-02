package api

import (
	"context"
	"log/slog"
	"manga-manager/internal/dashboard"
	"manga-manager/internal/recommendation"
	"time"
)

// transportEffects shares query caches without retaining feature services.
type transportEffects struct {
	stats           *dashboard.Service
	recommendations *recommendation.Service
	store           dashboard.Store
	background      func(func())
}

func (e *transportEffects) warm(reason string) {
	e.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := e.stats.LoadGlobal(ctx, e.store); err != nil {
			slog.Debug("Failed to warm dashboard stats cache", "reason", reason, "error", err)
		}
	})
}
