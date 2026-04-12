package koreader

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
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

func FingerprintRelativePath(libraryRoot, bookPath string) string {
	rel, err := filepath.Rel(libraryRoot, bookPath)
	if err != nil {
		rel = bookPath
	}
	rel = filepath.ToSlash(strings.ToLower(strings.TrimSpace(rel)))
	return hashMD5(rel)
}

func FingerprintFilename(bookPath string) string {
	name := strings.ToLower(strings.TrimSpace(filepath.Base(bookPath)))
	return hashMD5(name)
}

func hashMD5(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}
