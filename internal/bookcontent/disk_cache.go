package bookcontent

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func processedImageCacheFileName(cacheKey, contentType string) string {
	sum := fmt.Sprintf("%x", sha1.Sum([]byte(cacheKey)))
	return sum + extensionFromContentType(contentType)
}

func extensionFromContentType(contentType string) string {
	switch {
	case strings.Contains(contentType, "webp"):
		return ".webp"
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "avif"):
		return ".avif"
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return ".jpg"
	default:
		return ".bin"
	}
}

// contentTypeFromExtension 是 extensionFromContentType 的逆：由缓存文件扩展名精确复原写入时的 MIME。
func contentTypeFromExtension(ext string) string {
	switch ext {
	case ".webp":
		return "image/webp"
	case ".png":
		return "image/png"
	case ".avif":
		return "image/avif"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return ""
	}
}

// detectImageContentType 探测图片字节 MIME。标准库 http.DetectContentType 无 AVIF 签名，会把
// AVIF 误判为 application/octet-stream；此处补一条 AVIF ftyp 探测，保证缓存命中与首次响应一致。
func detectImageContentType(data []byte) string {
	ct := http.DetectContentType(data)
	if ct == "application/octet-stream" && len(data) >= 12 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "avif", "avis":
			return "image/avif"
		}
	}
	return ct
}

func readDiskImageCache(ctx context.Context, dir, cacheKey string) ([]byte, string, bool) {
	sum := fmt.Sprintf("%x", sha1.Sum([]byte(cacheKey)))

	for _, ext := range []string{".webp", ".png", ".avif", ".jpg", ".bin"} {
		path := filepath.Join(dir, sum[:2], sum+ext)
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			// 优先按扩展名精确复原 MIME（写入时扩展名即由权威 finalContentType 推导），
			// 仅 .bin 未知格式才回退字节探测。
			ct := contentTypeFromExtension(ext)
			if ct == "" {
				ct = detectImageContentType(data)
			}
			return data, ct, true
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
			slog.WarnContext(ctx, "Failed to read processed page disk cache", "path", path, "error", err)
		}
	}
	return nil, "", false
}

func writeDiskImageCache(cacheDir, cacheKey string, data []byte, contentType string) error {
	if len(data) == 0 {
		return nil
	}
	fileName := processedImageCacheFileName(cacheKey, contentType)
	subDir := fileName[:2]
	dir := filepath.Join(cacheDir, subDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// 原子写：直接 WriteFile 时，磁盘写满（ENOSPC）或进程被杀会留下半截文件，
	// 而读取侧只检查「存在且非空」，于是这个截断的字节流会被当成有效缓存长期下发——
	// 用户看到的是某一页永远显示不出来，清缓存才好。先写临时文件再 rename 即可避免。
	target := filepath.Join(dir, fileName)
	tmp, err := os.CreateTemp(dir, "."+fileName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	committed = true
	return nil
}
