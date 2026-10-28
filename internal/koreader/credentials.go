package koreader

import (
	"crypto/md5"
	"encoding/hex"
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
