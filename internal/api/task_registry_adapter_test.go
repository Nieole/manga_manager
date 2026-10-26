package api

import (
	"context"
	"manga-manager/internal/application/runtime"
	"manga-manager/internal/task"
)

func (c *Controller) buildTaskDispatch() map[taskDispatchKey]taskDispatch {
	registry := runtime.NewRegistry(c.scans, c.maintenance, c.intelligence)
	if c.runtime != nil {
		registry = c.runtime.Registry
	}
	out := make(map[taskDispatchKey]taskDispatch)
	for _, def := range registry.Definitions() {
		out[taskDispatchKey{Type: string(def.Type), Variant: TaskVariant(def.Variant)}] = taskDispatch{Resumable: def.Resumable, Relaunch: func(ctx context.Context, run RunSnapshot, t task.Trigger) error {
			return taskLaunchError(registry.Restart(ctx, runDomainIdentity(run), run.Params, t))
		}}
	}
	return out
}
