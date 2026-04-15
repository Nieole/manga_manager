package koreader

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func HashKey(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
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
