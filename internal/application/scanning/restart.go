package scanning

import (
	"context"
	"fmt"
	"manga-manager/internal/task"
)

// RestartDefinition declares retry and unattended replay for a scanning use case.
type RestartDefinition struct {
	Type      task.Type
	Variant   task.Variant
	Resumable bool
}

// Restarts returns the scanning capabilities registered before any launch.
func (*Service) Restarts() []RestartDefinition {
	return []RestartDefinition{{Type: "scan_library", Resumable: true}, {Type: "scan_series", Resumable: true}, {Type: "generate_covers", Resumable: true}, {Type: "cleanup_library"}}
}

// Restart replays the original force input and rediscovers missing covers after process loss.
func (c *Service) Restart(ctx context.Context, id task.Identity, args map[string]string, t task.Trigger) (task.Launched, error) {
	if id.Variant != "" {
		return task.Launched{}, fmt.Errorf("unsupported scanning variant: %s", id.Variant)
	}
	switch {
	case id.Type == "scan_library" && id.Scope == task.ScopeLibrary && id.ScopeID > 0:
		lib, err := c.store.GetLibrary(ctx, id.ScopeID)
		if err != nil {
			return task.Launched{}, err
		}
		return c.ScanLibrary(lib, args["force"] == "true", t)
	case id.Type == "scan_series" && id.Scope == task.ScopeSeries && id.ScopeID > 0:
		return task.Launched{}, c.ScanSeries(id.ScopeID, args["force"] == "true", t)
	case id.Type == "generate_covers" && id.Scope == task.ScopeLibrary && id.ScopeID > 0:
		return c.BackfillCovers(id.ScopeID, t)
	case id.Type == "cleanup_library" && id.Scope == task.ScopeLibrary && id.ScopeID > 0 && t != task.TriggerResumed:
		return c.CleanupLibrary(id.ScopeID, t)
	}
	return task.Launched{}, fmt.Errorf("unsupported scanning restart: %+v", id)
}
