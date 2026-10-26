package runtime

import (
	"context"
	"fmt"
	"manga-manager/internal/application/intelligence"
	"manga-manager/internal/application/maintenance"
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/config"
	"manga-manager/internal/task"
	"sort"
)

type restartKey struct {
	kind    task.Type
	variant task.Variant
}

// RestartDefinition is the one retry/resume fact for an assembled use case.
type RestartDefinition struct {
	Type      task.Type
	Variant   task.Variant
	Resumable bool
}
type restartEntry struct {
	definition RestartDefinition
	run        func(context.Context, task.Identity, map[string]string, task.Trigger) error
}

// Registry is immutable after assembly; UI capability and unattended recovery consult the same entries.
type Registry struct{ entries map[restartKey]restartEntry }

// NewRegistry registers complete feature restart declarations without starting any work.
func NewRegistry(scans *scanning.Service, maintenance *maintenance.Service, intelligence *intelligence.Service) *Registry {
	r := &Runtime{Scans: scans, Maintenance: maintenance, Intelligence: intelligence}
	return newRegistry(r)
}
func newRegistry(r *Runtime) *Registry {
	registry := &Registry{entries: make(map[restartKey]restartEntry)}
	add := func(def RestartDefinition, run func(context.Context, task.Identity, map[string]string, task.Trigger) error) {
		registry.entries[restartKey{def.Type, def.Variant}] = restartEntry{def, run}
	}
	for _, def := range r.Scans.Restarts() {
		add(RestartDefinition{def.Type, def.Variant, def.Resumable}, func(ctx context.Context, id task.Identity, args map[string]string, t task.Trigger) error {
			_, err := r.Scans.Restart(ctx, id, args, t)
			return err
		})
	}
	for _, def := range intelligence.Restarts() {
		add(RestartDefinition{def.Type, def.Variant, def.Resumable}, func(ctx context.Context, id task.Identity, args map[string]string, t task.Trigger) error {
			_, err := r.Intelligence.Restart(ctx, id, args, t)
			return err
		})
	}
	for _, def := range maintenance.Restarts() {
		add(RestartDefinition{def.Type, def.Variant, def.Resumable}, func(ctx context.Context, id task.Identity, args map[string]string, t task.Trigger) error {
			return r.Maintenance.Restart(ctx, id, args, t)
		})
	}
	return registry
}

// Definitions returns a stable projection of the complete assembly-time declarations.
func (r *Registry) Definitions() []RestartDefinition {
	out := make([]RestartDefinition, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.definition)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type == out[j].Type {
			return out[i].Variant < out[j].Variant
		}
		return out[i].Type < out[j].Type
	})
	return out
}

// Retryable reports whether this exact type and variant has a restart use case.
func (r *Registry) Retryable(t task.Type, v task.Variant) bool {
	_, ok := r.entries[restartKey{t, v}]
	return ok
}

// Resumable reports whether unattended replay is permitted for this identity.
func (r *Registry) Resumable(t task.Type, v task.Variant) bool {
	entry, ok := r.entries[restartKey{t, v}]
	return ok && entry.definition.Resumable
}

// Restart dispatches persisted identity and original arguments without interpreting feature parameters.
func (r *Registry) Restart(ctx context.Context, id task.Identity, args map[string]string, t task.Trigger) error {
	e, ok := r.entries[restartKey{id.Type, id.Variant}]
	if !ok {
		return fmt.Errorf("no restart for %s/%s", id.Type, id.Variant)
	}
	if t == task.TriggerResumed && !e.definition.Resumable {
		return fmt.Errorf("automatic resume unsupported: %s/%s", id.Type, id.Variant)
	}
	return e.run(ctx, id, args, t)
}

// Policy derives the domain resume policy from these same declarations and the live Manager snapshot.
func (r *Registry) Policy(cfg config.Config) task.ResumePolicy {
	var keys []task.ResumeKey
	for _, e := range r.entries {
		if e.definition.Resumable {
			keys = append(keys, task.ResumeKey{Type: e.definition.Type, Variant: e.definition.Variant})
		}
	}
	policy := task.NewResumePolicy(keys...)
	policy.Disabled = cfg.Tasks.ResumeAfterRestart != nil && !*cfg.Tasks.ResumeAfterRestart
	return policy
}
