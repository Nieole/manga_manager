package koreader

import (
	"context"
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

// FingerprintFile 计算整个文件的 md5。
//
// 仅供无长任务上下文的调用方使用；扫描/重建索引这类可被取消的批量路径请用
// FingerprintFileContext——它们一本可能有几个 GB，读完一遍要几十秒，
// 期间取消信号完全传不进来，任务面板上的「取消」会像是没反应。
func FingerprintFile(path string) (string, error) {
	return FingerprintFileContext(context.Background(), path)
}

// FingerprintFileContext 与 FingerprintFile 相同，但整文件读取可被 ctx 打断。
func FingerprintFileContext(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, &contextReader{ctx: ctx, r: f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// contextReader 在每次 Read 前检查取消。
//
// 只实现 Read（不透传 WriteTo/ReadFrom）是刻意的：那些快路径会把整段拷贝下沉到内核，
// 中途就再没有检查取消的机会了。代价是 io.Copy 的 32KiB 缓冲——取消后最多再多读一块。
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
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
	if n, err := io.ReadFull(f, buf); err != nil {
		if !errors.Is(err, io.ErrUnexpectedEOF) && err != io.EOF {
			return "", err
		}
		// 短读时必须按「实际读到的字节数 n」重切，不能用 info.Size()：文件可能在 Stat 之后
		// 被截短，此时 info.Size() 比缓冲区容量还大，buf[:info.Size()] 会 slice 越界 panic。
		buf = buf[:n]
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
