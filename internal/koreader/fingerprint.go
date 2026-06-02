package koreader

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var syncKeyPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func HashKey(raw string) string {
	sum := md5.Sum([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func NormalizeSyncKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func IsValidSyncKey(raw string) bool {
	return syncKeyPattern.MatchString(NormalizeSyncKey(raw))
}

func FingerprintFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func FingerprintQuickFile(path string) (string, error) {
	const chunkSize = 64 * 1024

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha1.New()
	if _, err := fmt.Fprintf(h, "%d|", info.Size()); err != nil {
		return "", err
	}

	buf := make([]byte, chunkSize)
	if _, err := io.ReadFull(f, buf); err != nil {
		if !errors.Is(err, io.ErrUnexpectedEOF) && err != io.EOF {
			return "", err
		}
		buf = buf[:maxInt(0, int(info.Size()))]
	}
	if len(buf) > 0 {
		if _, err := h.Write(buf); err != nil {
			return "", err
		}
	}

	if info.Size() > chunkSize {
		tail := make([]byte, chunkSize)
		if _, err := f.ReadAt(tail, info.Size()-chunkSize); err != nil {
			return "", err
		}
		if _, err := h.Write(tail); err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func FingerprintRelativePath(libraryRoot, bookPath string, ignoreExtension bool) string {
	rel, err := filepath.Rel(libraryRoot, bookPath)
	if err != nil {
		rel = bookPath
	}
	return FingerprintDocumentPath(rel, ignoreExtension)
}

func FingerprintDocumentPath(documentPath string, ignoreExtension bool) string {
	normalized := normalizePathFragment(documentPath, ignoreExtension)
	if normalized == "" {
		return ""
	}
	return hashMD5(normalized)
}

func normalizePathFragment(raw string, ignoreExtension bool) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	standard := strings.ReplaceAll(raw, "\\", "/")
	standard = path.Clean(standard)
	standard = strings.TrimPrefix(standard, "./")
	parts := strings.Split(standard, "/")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		filtered = append(filtered, strings.ToLower(part))
	}
	if len(filtered) == 0 {
		return ""
	}

	start := len(filtered) - 3
	if start < 0 {
		start = 0
	}
	filtered = filtered[start:]
	if ignoreExtension && len(filtered) > 0 {
		last := filtered[len(filtered)-1]
		ext := path.Ext(last)
		filtered[len(filtered)-1] = strings.TrimSuffix(last, ext)
	}

	return strings.Join(filtered, "/")
}

func hashMD5(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
