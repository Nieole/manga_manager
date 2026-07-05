// 业务说明：本文件是业务回归测试，覆盖 KOReader 集成的纯函数：密钥哈希/归一化、文档路径指纹、
// 匹配模式归一化与快速指纹的头尾采样特性。这些是跨设备进度匹配的事实来源，重构须保持行为一致。

package koreader

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manga-manager/internal/config"
)

func TestHashKeyTrimsAndMatchesMD5(t *testing.T) {
	sum := md5.Sum([]byte("secret-key"))
	want := hex.EncodeToString(sum[:])
	if got := HashKey("secret-key"); got != want {
		t.Fatalf("HashKey mismatch: got %q want %q", got, want)
	}
	// 前后空白应被裁剪后再哈希。
	if HashKey("  secret-key  ") != want {
		t.Fatalf("HashKey should trim surrounding whitespace")
	}
	if len(HashKey("anything")) != 32 {
		t.Fatalf("HashKey should return 32 hex chars")
	}
}

func TestNormalizeSyncKeyLowersAndTrims(t *testing.T) {
	if got := NormalizeSyncKey("  ABCdef  "); got != "abcdef" {
		t.Fatalf("NormalizeSyncKey = %q, want abcdef", got)
	}
}

func TestIsValidSyncKeyBoundaries(t *testing.T) {
	hex32 := strings.Repeat("a", 32)
	cases := []struct {
		in   string
		want bool
	}{
		{hex32, true},
		{strings.ToUpper(hex32), true},   // 归一化小写后仍合法
		{"  " + hex32 + "  ", true},      // 归一化裁剪空白
		{strings.Repeat("a", 31), false}, // 太短
		{strings.Repeat("a", 33), false}, // 太长
		{strings.Repeat("g", 32), false}, // 含非十六进制字符
		{"", false},                      // 空
	}
	for _, tc := range cases {
		if got := IsValidSyncKey(tc.in); got != tc.want {
			t.Fatalf("IsValidSyncKey(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestFingerprintDocumentPathNormalization 覆盖文档路径归一化：分隔符/大小写/末3段/去扩展名/空值。
func TestFingerprintDocumentPathNormalization(t *testing.T) {
	// 反斜杠与正斜杠等价。
	if FingerprintDocumentPath(`a\b\c.cbz`, false) != FingerprintDocumentPath("a/b/c.cbz", false) {
		t.Fatalf("backslash and slash paths should fingerprint identically")
	}
	// 大小写不敏感。
	if FingerprintDocumentPath("A/B/C.CBZ", false) != FingerprintDocumentPath("a/b/c.cbz", false) {
		t.Fatalf("path fingerprint should be case-insensitive")
	}
	// 仅取末尾 3 段。
	if FingerprintDocumentPath("root/extra/a/b/c.cbz", false) != FingerprintDocumentPath("a/b/c.cbz", false) {
		t.Fatalf("only last 3 path segments should matter")
	}
	// 去扩展名后不同扩展等价，且与保留扩展名不同。
	if FingerprintDocumentPath("a/b/c.cbz", true) != FingerprintDocumentPath("a/b/c.epub", true) {
		t.Fatalf("ignoreExtension should drop differing extensions")
	}
	if FingerprintDocumentPath("a/b/c.cbz", true) == FingerprintDocumentPath("a/b/c.cbz", false) {
		t.Fatalf("ignoreExtension=true should differ from keeping the extension")
	}
	// 空/纯空白/根/点 都归一化为空指纹。
	for _, empty := range []string{"", "   ", "/", "."} {
		if got := FingerprintDocumentPath(empty, false); got != "" {
			t.Fatalf("FingerprintDocumentPath(%q) = %q, want empty", empty, got)
		}
	}
}

// TestFingerprintRelativePathMatchesDocumentPath 确认库根相对路径指纹等价于直接文档路径指纹。
func TestFingerprintRelativePathMatchesDocumentPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lib")
	book := filepath.Join(root, "Series", "Vol01.cbz")
	if FingerprintRelativePath(root, book, false) != FingerprintDocumentPath("Series/Vol01.cbz", false) {
		t.Fatalf("relative-path fingerprint should match document-path fingerprint")
	}
	if FingerprintRelativePath(root, book, true) != FingerprintDocumentPath("Series/Vol01", true) {
		t.Fatalf("relative-path fingerprint (ignoreExt) mismatch")
	}
}

// TestNormalizeDocumentForMatchModes 覆盖匹配模式分支：file_path 走路径指纹，其余走小写原文。
func TestNormalizeDocumentForMatchModes(t *testing.T) {
	// file_path 模式 == 路径指纹。
	if NormalizeDocumentForMatch("A/B.cbz", config.KOReaderMatchModeFilePath, false) != FingerprintDocumentPath("A/B.cbz", false) {
		t.Fatalf("file_path mode should use document-path fingerprint")
	}
	// file_path 模式下 ignoreExtension 影响结果。
	if NormalizeDocumentForMatch("A/B.cbz", config.KOReaderMatchModeFilePath, true) == NormalizeDocumentForMatch("A/B.cbz", config.KOReaderMatchModeFilePath, false) {
		t.Fatalf("ignoreExtension should change file_path match key")
	}
	// binary_hash 模式 == 小写原文。
	if got := NormalizeDocumentForMatch("ABC.CBZ", config.KOReaderMatchModeBinaryHash, false); got != "abc.cbz" {
		t.Fatalf("binary_hash mode = %q, want abc.cbz", got)
	}
	// 未知模式回落到小写原文。
	if got := NormalizeDocumentForMatch("Foo.CBZ", "mystery-mode", false); got != "foo.cbz" {
		t.Fatalf("unknown mode = %q, want lowercased document", got)
	}
	// 空文档 -> 空。
	if got := NormalizeDocumentForMatch("   ", config.KOReaderMatchModeBinaryHash, false); got != "" {
		t.Fatalf("blank document should normalize to empty, got %q", got)
	}
}

func TestKeyPreviewBehaviour(t *testing.T) {
	if got := keyPreview(""); got != "<empty>" {
		t.Fatalf("empty key preview = %q, want <empty>", got)
	}
	if got := keyPreview("abc"); got != "abc" {
		t.Fatalf("short key preview = %q, want abc", got)
	}
	if got := keyPreview("abcdefghijk"); got != "abcdefgh" {
		t.Fatalf("long key preview = %q, want first 8 chars", got)
	}
	// 归一化后再截取前 8。
	if got := keyPreview("  ABCDEFGHXY  "); got != "abcdefgh" {
		t.Fatalf("normalized key preview = %q, want abcdefgh", got)
	}
}

func TestReadableMatchMode(t *testing.T) {
	if got := readableMatchMode(MatchConfig{MatchMode: config.KOReaderMatchModeFilePath}); got != "路径" {
		t.Fatalf("file_path readable = %q, want 路径", got)
	}
	if got := readableMatchMode(MatchConfig{MatchMode: config.KOReaderMatchModeBinaryHash}); got != "二进制哈希" {
		t.Fatalf("binary_hash readable = %q, want 二进制哈希", got)
	}
}

func TestFingerprintFileMatchesMD5AndErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hello.bin")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// md5("hello") 的已知常量。
	const wantHelloMD5 = "5d41402abc4b2a76b9719d911017c592"
	got, err := FingerprintFile(p)
	if err != nil {
		t.Fatalf("FingerprintFile: %v", err)
	}
	if got != wantHelloMD5 {
		t.Fatalf("FingerprintFile = %q, want %q", got, wantHelloMD5)
	}
	// 全量指纹（md5）与快速指纹（sha1+size）算法不同，结果必然不同。
	quick, err := FingerprintQuickFile(p)
	if err != nil {
		t.Fatalf("FingerprintQuickFile: %v", err)
	}
	if quick == got {
		t.Fatalf("quick and full fingerprints should differ")
	}
	if _, err := FingerprintFile(filepath.Join(dir, "does-not-exist.bin")); err == nil {
		t.Fatalf("expected error fingerprinting a missing file")
	}
}

// TestFingerprintQuickFileHeadTailSampling 覆盖 >64KiB 文件的头尾采样：仅头段+尾段+大小参与哈希，
// 中间字节改动不影响快速指纹，而尾部字节改动会改变它；全量指纹对任一改动都敏感。
func TestFingerprintQuickFileHeadTailSampling(t *testing.T) {
	dir := t.TempDir()
	const size = 200 * 1024 // 远大于 64KiB 块，触发 ReadAt 尾部分支
	base := make([]byte, size)
	for i := range base {
		base[i] = byte(i % 251)
	}
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}

	origPath := write("orig.bin", base)
	origQuick, err := FingerprintQuickFile(origPath)
	if err != nil {
		t.Fatalf("quick orig: %v", err)
	}

	// 改动中间区域（超出头 64KiB、未进入尾 64KiB）——快速指纹不应变化。
	midOffset := 100 * 1024
	if midOffset < 64*1024 || midOffset >= size-64*1024 {
		t.Fatalf("test setup: middle offset not in unsampled region")
	}
	mid := make([]byte, size)
	copy(mid, base)
	mid[midOffset] ^= 0xFF
	midPath := write("mid.bin", mid)
	midQuick, err := FingerprintQuickFile(midPath)
	if err != nil {
		t.Fatalf("quick mid: %v", err)
	}
	if midQuick != origQuick {
		t.Fatalf("middle-only change should not affect quick fingerprint (head+tail sampling)")
	}

	// 改动最后一个字节（尾部采样区）——快速指纹必须变化。
	tail := make([]byte, size)
	copy(tail, base)
	tail[size-1] ^= 0xFF
	tailPath := write("tail.bin", tail)
	tailQuick, err := FingerprintQuickFile(tailPath)
	if err != nil {
		t.Fatalf("quick tail: %v", err)
	}
	if tailQuick == origQuick {
		t.Fatalf("tail-byte change should change quick fingerprint")
	}

	// 全量指纹对中间改动敏感（与快速指纹形成对比）。
	origFull, err := FingerprintFile(origPath)
	if err != nil {
		t.Fatalf("full orig: %v", err)
	}
	midFull, err := FingerprintFile(midPath)
	if err != nil {
		t.Fatalf("full mid: %v", err)
	}
	if origFull == midFull {
		t.Fatalf("full fingerprint should detect middle-byte change")
	}
}
