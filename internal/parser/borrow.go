package parser

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var archiveBorrows = struct {
	sync.Mutex
	active map[string]int
}{active: make(map[string]int)}

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

func retainArchiveBorrow(path string) func() {
	key := archiveBorrowKey(path)
	archiveBorrows.Lock()
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
		})
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
