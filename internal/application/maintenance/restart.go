package maintenance

import (
	"context"
	"errors"
	"fmt"
	"manga-manager/internal/task"
	"strconv"
	"strings"
)

// RestartDefinition declares the only supported retry and automatic replay identities.
type RestartDefinition struct {
	Type      task.Type
	Variant   task.Variant
	Resumable bool
}

// Restarts preserves explicit retry and resume restrictions, including the index exception.
func Restarts() []RestartDefinition {
	return []RestartDefinition{
		{Type: "rebuild_index"},
		{Type: "rebuild_thumbnails", Resumable: true}, {Type: "cleanup_thumbnails", Resumable: true},
		{Type: "rebuild_book_hashes", Variant: "foreground", Resumable: true}, {Type: "rebuild_book_hashes", Variant: "low_priority_backfill", Resumable: true},
		{Type: "rebuild_file_identities", Resumable: true}, {Type: "reconcile_koreader_progress", Resumable: true}, {Type: "refresh_koreader_matching", Resumable: true},
	}
}

// ErrAutomaticResumeUnsupported protects maintenance that requires an attended restart.
var ErrAutomaticResumeUnsupported = errors.New("maintenance cannot automatically resume")

// Restart replays the original variant and launch reason; older runs use the compatibility reason.
func (c *Service) Restart(_ context.Context, id task.Identity, args map[string]string, trigger task.Trigger) error {
	if id.Scope != task.ScopeSystem {
		return fmt.Errorf("unsupported maintenance scope: %s", id.Scope)
	}
	for _, definition := range Restarts() {
		if definition.Type != id.Type || definition.Variant != id.Variant {
			continue
		}
		if trigger == task.TriggerResumed && !definition.Resumable {
			return ErrAutomaticResumeUnsupported
		}
		cfg := c.config()
		if mode := args["match_mode"]; mode != "" {
			cfg.KOReader.MatchMode = mode
		}
		if value, err := strconv.ParseBool(args["path_ignore_extension"]); err == nil {
			cfg.KOReader.PathIgnoreExtension = value
		}
		switch id.Type {
		case "rebuild_index":
			_, err := c.RebuildIndex(trigger)
			return err
		case "rebuild_thumbnails":
			return c.RebuildThumbnails(trigger)
		case "cleanup_thumbnails":
			return c.CleanupThumbnails(trigger)
		case "rebuild_file_identities":
			return c.RebuildFileIdentities(trigger)
		case "reconcile_koreader_progress":
			return c.reconcileKOReaderProgress(trigger, cfg)
		case "refresh_koreader_matching":
			return c.refreshKOReaderMatching(trigger, cfg)
		case "rebuild_book_hashes":
			if id.Variant == "foreground" {
				return c.rebuildBookHashes(trigger, cfg)
			}
			reason := strings.TrimSpace(args["reason"])
			if reason == "" {
				reason = "manual_retry"
			}
			return c.BackfillBookHashes(reason, trigger)
		}
	}
	return fmt.Errorf("unsupported maintenance identity: %+v", id)
}
