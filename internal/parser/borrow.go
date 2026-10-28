package parser

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var archiveBorrows = struct {
	sync.Mutex
	active      map[string]int
	publication map[string]bool
	changed     chan struct{}
}{active: make(map[string]int), publication: make(map[string]bool), changed: make(chan struct{})}

// ErrArchivePublishing means a short publication window has closed read admission.
var ErrArchivePublishing = errors.New("archive publication in progress")

func archiveBorrowKey(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func retainArchiveBorrow(path string) (func(), error) {
	key := archiveBorrowKey(path)
	archiveBorrows.Lock()
	if archiveBorrows.publication[key] {
		archiveBorrows.Unlock()
		return nil, ErrArchivePublishing
	}
	archiveBorrows.active[key]++
	archiveBorrows.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			archiveBorrows.Lock()
			defer archiveBorrows.Unlock()
			archiveBorrows.active[key]--
			if archiveBorrows.active[key] == 0 {
				delete(archiveBorrows.active, key)
			}
			close(archiveBorrows.changed)
			archiveBorrows.changed = make(chan struct{})
		})
	}, nil
}

// AcquireArchivePublication closes admission and waits for actual borrowers, including
// evicted pool handles and whole-book streams. The caller must bound ctx and release.
func AcquireArchivePublication(ctx context.Context, path string) (func(), error) {
	key := archiveBorrowKey(path)
	archiveBorrows.Lock()
	if err := ctx.Err(); err != nil {
		archiveBorrows.Unlock()
		return nil, err
	}
	if archiveBorrows.publication[key] {
		archiveBorrows.Unlock()
		return nil, ErrArchivePublishing
	}
	archiveBorrows.publication[key] = true
	archiveBorrows.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			archiveBorrows.Lock()
			defer archiveBorrows.Unlock()
			delete(archiveBorrows.publication, key)
			close(archiveBorrows.changed)
			archiveBorrows.changed = make(chan struct{})
		})
	}
	for {
		archiveBorrows.Lock()
		active, changed := archiveBorrows.active[key], archiveBorrows.changed
		archiveBorrows.Unlock()
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		if active == 0 {
			return release, nil
		}
		select {
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

// ActiveArchiveBorrows counts direct readers, original-content streams and live pool borrows
// across pool eviction or replacement.
// Idle cached handles are not borrows. This observation alone does not reserve publication access.
func ActiveArchiveBorrows(path string) int {
	key := archiveBorrowKey(path)
	archiveBorrows.Lock()
	defer archiveBorrows.Unlock()
	return archiveBorrows.active[key]
}
