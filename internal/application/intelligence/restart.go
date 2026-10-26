package intelligence

import (
	"context"
	"errors"
	"fmt"
	"manga-manager/internal/metadata"
	"manga-manager/internal/task"
	"strings"
)

// ErrAutomaticResumeUnsupported protects paid generation from unattended replay.
var ErrAutomaticResumeUnsupported = errors.New("intelligence work cannot automatically resume")

// RestartDefinition is the single assembly-time retry and resume declaration.
type RestartDefinition struct {
	Type      task.Type
	Variant   task.Variant
	Resumable bool
}

// Restarts declares supported manual retry variants; none resume automatically.
func Restarts() []RestartDefinition {
	return []RestartDefinition{{Type: "scrape", Variant: "all_libraries"}, {Type: "scrape", Variant: "one_library"}, {Type: "ai_grouping"}}
}

// Restart replays persisted source and locale, using the request locale only for older runs.
func (c *Service) Restart(ctx context.Context, identity task.Identity, args map[string]string, trigger task.Trigger) (task.Launched, error) {
	for _, definition := range Restarts() {
		if definition.Type != identity.Type || definition.Variant != identity.Variant {
			continue
		}
		if trigger == task.TriggerResumed && !definition.Resumable {
			return task.Launched{}, ErrAutomaticResumeUnsupported
		}
		locale := strings.TrimSpace(args["locale"])
		if locale == "" {
			locale = metadata.LocaleFromContext(ctx)
		}
		ctx = metadata.WithLocale(ctx, locale)
		switch {
		case identity.Type == "ai_grouping" && identity.Scope == task.ScopeLibrary && identity.ScopeID > 0:
			return c.GroupLibrary(ctx, identity.ScopeID, locale, trigger)
		case identity.Type == "scrape" && identity.Variant == "all_libraries" && identity.Scope == task.ScopeSystem:
			return c.ScrapeAll(ctx, args["provider"], trigger)
		case identity.Type == "scrape" && identity.Variant == "one_library" && identity.Scope == task.ScopeLibrary && identity.ScopeID > 0:
			return c.ScrapeLibrary(ctx, identity.ScopeID, args["provider"], trigger)
		}
	}
	return task.Launched{}, fmt.Errorf("unsupported generation identity: %+v", identity)
}
