package runtime

import (
	"log/slog"
	"runtime/debug"
	"sync"
)

type background struct {
	mu     sync.Mutex
	wg     sync.WaitGroup
	done   chan struct{}
	once   sync.Once
	sealed bool
}

func newBackground() *background { return &background{done: make(chan struct{})} }
func (b *background) goRun(fn func()) {
	b.mu.Lock()
	if b.sealed {
		b.mu.Unlock()
		return
	}
	b.wg.Add(1)
	b.mu.Unlock()
	go func() {
		defer b.wg.Done()
		defer func() {
			if value := recover(); value != nil {
				slog.Error("Background task panicked", "panic", value, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}
func (b *background) signal()      { b.once.Do(func() { close(b.done) }) }
func (b *background) sealAndWait() { b.mu.Lock(); b.sealed = true; b.mu.Unlock(); b.wg.Wait() }
