package parser

import (
	"fmt"
	"path/filepath"
	"strings"
)

// OpenArchive 根据后缀自动分发归档解压驱动
func OpenArchive(path string) (Archive, error) {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".cbz", ".zip":
		return OpenZip(path)
	case ".cbr", ".rar":
		return OpenRar(path)
	default:
		return nil, fmt.Errorf("unsupported archive format: %s", ext)
	}
}
