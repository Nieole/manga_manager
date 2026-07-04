// 业务说明：本文件是业务回归测试，属于漫画文件解析层，负责识别归档、目录、页序、页数和可读取图片条目。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadEntryLimited 覆盖归档单页读取的字节上限保护（L90）。
func TestReadEntryLimited(t *testing.T) {
	// 正常小项：原样返回。
	if got, err := readEntryLimited(bytes.NewReader([]byte("hello")), 5, "ok.jpg"); err != nil || string(got) != "hello" {
		t.Fatalf("normal read failed: got=%q err=%v", got, err)
	}
	// 声明尺寸超限：应在任何拷贝/预分配前直接拒绝。
	if _, err := readEntryLimited(bytes.NewReader([]byte("x")), maxPageUncompressedBytes+1, "bomb.jpg"); err == nil {
		t.Fatal("expected error for oversized declared size, got nil")
	}
	// declared 为负（未知）时仍能正常读小项。
	if got, err := readEntryLimited(bytes.NewReader([]byte("hi")), -1, "unknown.jpg"); err != nil || string(got) != "hi" {
		t.Fatalf("negative-declared read failed: got=%q err=%v", got, err)
	}
}

func TestNaturalCompare(t *testing.T) {
	tests := []struct {
		a, b     string
		expected bool
	}{
		// 1. 自然序测试
		{"1.jpg", "2.jpg", true},
		{"2.jpg", "10.jpg", true},
		{"01.jpg", "1.jpg", true},

		// 2. 封面关键字优先测试 (跨目录层级)
		{"cover.jpg", "001.jpg", true},
		{"封面.jpg", "001.jpg", true},
		{"001.jpg", "front.png", false},
		{"Cover/001.jpg", "Ad.jpg", true},    // 子目录封面优于根目录非封面
		{"Scans/00.jpg", "01.jpg", true},     // Scans 目录优于根目录
		{"A/Cover/01.jpg", "B/01.jpg", true}, // 同级目录，Cover 优先

		// 3. 排除关键字测试
		{"cover_back.jpg", "001.jpg", false},
		{"001.jpg", "ad.jpg", true},

		// 4. 深度优先 (同为封面或同非封面)
		{"cover.jpg", "data/cover.jpg", true},
		{"a/001.jpg", "b/c/001.jpg", true},

		// 5. 综合场景
		{"p000.jpg", "001.jpg", false}, // p000 应该排在 001 之后（如果没有 cover 关键字的情况下按文件名排）
	}

	for _, tt := range tests {
		got := naturalCompare(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("naturalCompare(%q, %q) = %v; want %v", tt.a, tt.b, got, tt.expected)
		}
	}
}

func TestMarshalComicInfo(t *testing.T) {
	data, err := MarshalComicInfo(ComicInfo{
		Title:       "Book Title",
		Series:      "Series Title",
		Number:      "1",
		Volume:      "1",
		Count:       3,
		PageCount:   188,
		Genre:       "Action, Drama",
		LanguageISO: "zh",
	})
	if err != nil {
		t.Fatalf("MarshalComicInfo failed: %v", err)
	}
	if !strings.HasPrefix(string(data), xml.Header) {
		t.Fatalf("expected XML header, got %q", string(data[:min(len(data), len(xml.Header))]))
	}

	var info ComicInfo
	if err := xml.Unmarshal(data, &info); err != nil {
		t.Fatalf("unmarshal marshaled ComicInfo failed: %v", err)
	}
	if info.Title != "Book Title" || info.Series != "Series Title" || info.Number != "1" || info.PageCount != 188 {
		t.Fatalf("unexpected ComicInfo roundtrip: %+v", info)
	}
}

func TestArchivePoolInitResizesExistingPool(t *testing.T) {
	ResetArchivePool()

	root := t.TempDir()
	t.Cleanup(ResetArchivePool)
	paths := []string{
		filepath.Join(root, "one.cbz"),
		filepath.Join(root, "two.cbz"),
		filepath.Join(root, "three.cbz"),
	}
	for _, path := range paths {
		if err := writeParserTestCBZ(path); err != nil {
			t.Fatalf("write cbz failed: %v", err)
		}
	}

	InitPool(3)
	for _, path := range paths {
		arc, err := GetArchiveFromPool(path)
		if err != nil {
			t.Fatalf("get archive failed: %v", err)
		}
		if _, err := arc.GetPages(); err != nil {
			t.Fatalf("get pages failed: %v", err)
		}
	}
	if len(globalPool.items) != 3 {
		t.Fatalf("expected 3 cached archives, got %d", len(globalPool.items))
	}

	InitPool(1)
	if globalPool.maxSize != 1 {
		t.Fatalf("expected resized max size 1, got %d", globalPool.maxSize)
	}
	if len(globalPool.items) > 1 {
		t.Fatalf("expected cache to be trimmed to 1 item, got %d", len(globalPool.items))
	}
}

func writeParserTestCBZ(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create("001.png")
	if err != nil {
		_ = zw.Close()
		return err
	}
	if _, err := w.Write([]byte("not a real image")); err != nil {
		_ = zw.Close()
		return err
	}
	return zw.Close()
}
