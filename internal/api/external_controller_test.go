// 业务说明：本文件是业务回归测试，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package api

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCopyFileToExternalLibraryCopiesWithBufferedStaging(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "source.cbz")
	dst := filepath.Join(dstDir, "nested", "target.cbz")

	content := bytes.Repeat([]byte("manga-manager-buffered-copy"), 4096)
	modTime := time.Now().Add(-2 * time.Hour).Round(time.Second)
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("write source failed: %v", err)
	}
	if err := os.Chtimes(src, modTime, modTime); err != nil {
		t.Fatalf("set source mod time failed: %v", err)
	}

	skipped, err := copyFileToExternalLibrary(src, dst, make(map[string]struct{}))
	if err != nil {
		t.Fatalf("copyFileToExternalLibrary failed: %v", err)
	}
	if skipped {
		t.Fatal("expected copy to execute for missing destination")
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination failed: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("destination content mismatch")
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat destination failed: %v", err)
	}
	if !info.ModTime().Equal(modTime) {
		t.Fatalf("expected mod time %v, got %v", modTime, info.ModTime())
	}
}

func TestCopyFileToExternalLibrarySkipsExistingDestination(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "source.cbz")
	dst := filepath.Join(dstDir, "existing.cbz")

	if err := os.WriteFile(src, []byte("new-content"), 0o644); err != nil {
		t.Fatalf("write source failed: %v", err)
	}
	if err := os.WriteFile(dst, []byte("existing-content"), 0o644); err != nil {
		t.Fatalf("write destination failed: %v", err)
	}

	skipped, err := copyFileToExternalLibrary(src, dst, make(map[string]struct{}))
	if err != nil {
		t.Fatalf("copyFileToExternalLibrary failed: %v", err)
	}
	if !skipped {
		t.Fatal("expected existing destination to be skipped")
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination failed: %v", err)
	}
	if string(got) != "existing-content" {
		t.Fatalf("expected existing destination content to remain unchanged, got %q", string(got))
	}
}
