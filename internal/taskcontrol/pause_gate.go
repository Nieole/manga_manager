package taskcontrol

import (
	"context"
	"sync"
	"time"
)

type contextKey struct{}

type PauseGate struct {
	mu       sync.Mutex
	resumeCh chan struct{}
	paused   bool
	pausedAt time.Time
}

func NewPauseGate() *PauseGate {
	return &PauseGate{resumeCh: make(chan struct{})}
}

func WithPauseGate(ctx context.Context, gate *PauseGate) context.Context {
	if gate == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, gate)
}

func FromContext(ctx context.Context) *PauseGate {
	if ctx == nil {
		return nil
	}
	gate, _ := ctx.Value(contextKey{}).(*PauseGate)
	return gate
}

func Wait(ctx context.Context) error {
	if gate := FromContext(ctx); gate != nil {
		return gate.Wait(ctx)
	}
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (g *PauseGate) Pause() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused {
		return
	}
	g.paused = true
	g.pausedAt = time.Now()
	g.resumeCh = make(chan struct{})
}

func (g *PauseGate) Resume() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.paused {
		return
	}
	g.paused = false
	g.pausedAt = time.Time{}
	close(g.resumeCh)
}

func (g *PauseGate) IsPaused() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

func (g *PauseGate) PausedAt() time.Time {
	if g == nil {
		return time.Time{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pausedAt
}

func (g *PauseGate) Wait(ctx context.Context) error {
	if g == nil {
		if ctx == nil {
			return nil
		}
		return ctx.Err()
	}
	for {
		g.mu.Lock()
		if !g.paused {
			g.mu.Unlock()
			if ctx == nil {
				return nil
			}
			return ctx.Err()
		}
		resumeCh := g.resumeCh
		g.mu.Unlock()

		if ctx == nil {
			<-resumeCh
			continue
		}
		select {
		case <-resumeCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
