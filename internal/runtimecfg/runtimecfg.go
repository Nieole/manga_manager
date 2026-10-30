package runtimecfg

import (
	"errors"
	"manga-manager/internal/config"
	"manga-manager/internal/images"
	"manga-manager/internal/logger"
	"manga-manager/internal/parser"
	"sync"
)

// ErrClosed rejects configuration publication after the owning runtime has stopped.
var ErrClosed = errors.New("runtime resources are closed")

// Resources serializes publication and retirement of the process-singleton resources.
// Borrowed archives and image permits retain their original return destination.
type Resources struct {
	mu     sync.Mutex
	closed bool
}

// NewResources binds one runtime to the process resource publication lifecycle.
func NewResources() *Resources { return &Resources{} }

// Apply serializes resource changes and rejects publication after retirement.
func (r *Resources) Apply(cfg *config.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	return Apply(cfg)
}

// Close retires admission after process users have been drained. Existing borrows
// close their own archive on final return; permit channels are never closed.
func (r *Resources) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	Close()
}

// Apply initializes process resources for embedded callers. Application runtimes
// publish through their Resources owner so late callbacks cannot reopen retired resources.
func Apply(cfg *config.Config) error {
	if err := logger.SetLevel(cfg.Logging.Level); err != nil {
		return err
	}
	parser.InitPool(cfg.Scanner.ArchivePoolSize)
	limit := cfg.Scanner.MaxAiConcurrency
	if limit < 1 {
		limit = 1
	}
	if images.AIConcurrency() != limit {
		images.InitProcessor(limit)
	}
	return nil
}

// Close releases process configuration resources after request and task owners have exited.
func Close() { parser.CloseArchivePool(); images.CloseProcessor() }
