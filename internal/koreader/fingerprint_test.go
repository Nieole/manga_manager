// 业务说明：本文件是业务回归测试，属于 KOReader 集成链路，负责识别设备阅读记录并与本地漫画阅读进度对齐。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package koreader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintQuickFileChangesWithTailContent(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.bin")
	second := filepath.Join(dir, "second.bin")

	if err := os.WriteFile(first, []byte("abcdef0123456789"), 0o644); err != nil {
		t.Fatalf("write first file failed: %v", err)
	}
	if err := os.WriteFile(second, []byte("abcdef0123456799"), 0o644); err != nil {
		t.Fatalf("write second file failed: %v", err)
	}

	a, err := FingerprintQuickFile(first)
	if err != nil {
		t.Fatalf("FingerprintQuickFile first failed: %v", err)
	}
	b, err := FingerprintQuickFile(second)
	if err != nil {
		t.Fatalf("FingerprintQuickFile second failed: %v", err)
	}
	if a == b {
		t.Fatal("expected different quick fingerprints for different file content")
	}
}

// TestFingerprintQuickFileSmallFiles 覆盖短读重切路径（文件小于块大小）：空/极小文件必须不 panic 且结果稳定。
// 回归保护：短读曾用 buf[:info.Size()] 重切，文件被截短时会 slice 越界 panic；现按实际读到的字节数重切。
func TestFingerprintQuickFileSmallFiles(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"one", []byte("x")},
		{"tiny", []byte("abcdef")},
	} {
		p := filepath.Join(dir, tc.name+".bin")
		if err := os.WriteFile(p, tc.data, 0o644); err != nil {
			t.Fatalf("%s write: %v", tc.name, err)
		}
		h1, err := FingerprintQuickFile(p)
		if err != nil {
			t.Fatalf("%s fingerprint: %v", tc.name, err)
		}
		h2, err := FingerprintQuickFile(p)
		if err != nil {
			t.Fatalf("%s fingerprint retry: %v", tc.name, err)
		}
		if h1 == "" || h1 != h2 {
			t.Fatalf("%s: expected stable non-empty hash, got %q / %q", tc.name, h1, h2)
		}
	}
}

// TestFingerprintFileContextStopsOnCancel 守卫「整文件哈希可被取消」。
//
// 一本几 GB 的 CBR 读完一遍要几十秒，期间取消信号完全传不进来，
// 任务面板上的「取消」会像是没反应——重建 KOReader 索引与 identity 档位扫描都走这条路径。
func TestFingerprintFileContextStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.cbz")
	// 要明显大过 io.Copy 的 32KiB 缓冲，否则一次 Read 就读完了，取消检查根本没机会介入。
	if err := os.WriteFile(path, make([]byte, 4<<20), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FingerprintFileContext(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消的 ctx 返回 %v，期望 context.Canceled —— 取消打不断整文件读取", err)
	}

	// 未取消时结果必须与不带 ctx 的版本逐字相同：指纹是 KOReader 匹配的主键，
	// 变了就等于全库进度重新对不上。
	want, err := FingerprintFile(path)
	if err != nil {
		t.Fatalf("FingerprintFile: %v", err)
	}
	got, err := FingerprintFileContext(context.Background(), path)
	if err != nil {
		t.Fatalf("FingerprintFileContext: %v", err)
	}
	if got != want {
		t.Fatalf("带 ctx 的指纹 %q 与原实现 %q 不一致 —— 全库 KOReader 进度会重新对不上", got, want)
	}
}
