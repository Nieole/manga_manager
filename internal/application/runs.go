// Package application owns cross-feature orchestration using the shared task engine.
package application

import (
	"context"
	"log/slog"

	"manga-manager/internal/logger"
	"manga-manager/internal/task"
)

// Runs is the application launch boundary. It never owns a second scheduler.
type Runs struct {
	engine  *task.Engine
	prepare func(context.Context, task.Identity) error
}

// NewRuns binds the existing engine and an optional adapter identity projection.
// Restart capabilities remain registered at assembly time by the composition root.
func NewRuns(engine *task.Engine, prepare func(context.Context, task.Identity) error) *Runs {
	if engine == nil {
		panic("application: task engine is required")
	}
	return &Runs{engine: engine, prepare: prepare}
}

// Start shares admission, library coordination, context and completion with legacy callers.
func (r *Runs) Start(ctx context.Context, spec task.RunSpec, body task.Body) (task.Launched, error) {
	if r.prepare != nil {
		if err := r.prepare(ctx, spec.Identity); err != nil {
			return task.Launched{}, err
		}
	}
	launched, err := r.engine.Start(ctx, spec, body)
	if launched.Stalled != task.StallNone {
		slog.InfoContext(ctx, "Automatic launch suppressed", "task_key", spec.Key, "trigger", spec.Trigger, "reason", launched.Stalled)
	}
	return launched, err
}

// DecorateRunContext attaches the run identity and diagnostic task key to every body.
func DecorateRunContext(ctx context.Context, run task.Run, spec task.RunSpec) context.Context {
	return logger.WithRunID(logger.WithTaskKey(ctx, spec.Key), run.ID)
}
