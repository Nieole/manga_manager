package api

import (
	"path/filepath"
	"runtime"
	"strings"
)

func sameNativePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
