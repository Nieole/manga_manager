// 业务说明：本文件是业务回归测试，属于漫画文件解析层，负责识别归档、目录、页序、页数和可读取图片条目。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
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
		arc, release, err := GetArchiveFromPool(path)
		if err != nil {
			t.Fatalf("get archive failed: %v", err)
		}
		if _, err := arc.GetPages(); err != nil {
			t.Fatalf("get pages failed: %v", err)
		}
		// 归还借用，否则后续 resize 只会把 item 标记为待关闭而不真正释放句柄。
		release()
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

// TestPoolEvictionDoesNotCloseBorrowedHandle 锁住归档池的引用计数语义。
//
// 池发放的是共享句柄。淘汰若当场 Close，仍持有它的在途请求就会读到一个已关闭的句柄：
// zip 侧返回 "file already closed"（500），rar 侧更会向 Close 置空的 cache map 写入而 panic。
// 引用计数保证 Close 推迟到最后一个借用方归还之后。
func TestPoolEvictionDoesNotCloseBorrowedHandle(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.cbz")
	pathB := filepath.Join(dir, "b.cbz")
	for _, p := range []string{pathA, pathB} {
		if err := writeParserTestCBZ(p); err != nil {
			t.Fatalf("write cbz failed: %v", err)
		}
	}

	InitPool(1)
	t.Cleanup(ResetArchivePool)

	arcA, releaseA, err := GetArchiveFromPool(pathA)
	if err != nil {
		t.Fatalf("get archive A failed: %v", err)
	}
	pagesA, err := arcA.GetPages()
	if err != nil {
		t.Fatalf("get pages A failed: %v", err)
	}

	// 借用 A 的同时取 B：池容量为 1，A 会被淘汰出 map，但不该被真正关闭。
	_, releaseB, err := GetArchiveFromPool(pathB)
	if err != nil {
		t.Fatalf("get archive B failed: %v", err)
	}
	defer releaseB()

	if _, err := arcA.ReadPage(pagesA[0].Name); err != nil {
		t.Fatalf("borrowed handle became unusable after eviction: %v", err)
	}

	releaseA()

	// 归还后句柄才真正关闭；再读应报错而不是 panic。
	if _, err := arcA.ReadPage(pagesA[0].Name); err == nil {
		t.Fatal("expected read on fully released handle to fail")
	}
}

// TestRarReadAfterCloseReturnsErrorNotPanic 锁住 RAR 句柄的关闭终态。
// 旧实现 Close 把 cache 置 nil 而 reopenLocked 只重建 seen，关闭后再读会向 nil map 赋值而 panic。
func TestRarReadAfterCloseReturnsErrorNotPanic(t *testing.T) {
	arc, err := OpenRar(filepath.Join(t.TempDir(), "missing.cbr"))
	if err != nil {
		t.Fatalf("open rar failed: %v", err)
	}
	if err := arc.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if _, err := arc.ReadPage("001.png"); !errors.Is(err, ErrArchiveClosed) {
		t.Fatalf("expected ErrArchiveClosed after Close, got %v", err)
	}
	if _, err := arc.ReadMetadataFile("ComicInfo.xml"); !errors.Is(err, ErrArchiveClosed) {
		t.Fatalf("expected ErrArchiveClosed from ReadMetadataFile, got %v", err)
	}
}

// TestCoverKeywordMatchingIsWholeToken 锁住封面启发式的整词匹配。
//
// 短关键字 ad / bc / fc 在裸 strings.Contains 下会误伤大量正常文件名：
// "Loaded.jpg" 含 ad、"abc.jpg" 含 bc、"specfic_01.jpg" 含 fc。一旦误判，
// 该页会被当成广告页排到全卷最后、或被当成封面顶到最前，整卷页序就错了。
func TestCoverKeywordMatchingIsWholeToken(t *testing.T) {
	falsePositives := []struct{ name, keyword string }{
		{"Loaded.jpg", "ad"},
		{"abc.jpg", "bc"},
		{"specfic_01.jpg", "fc"},
		{"broadcast.png", "ad"},
		{"indexed_page.jpg", "index"},
	}
	for _, tc := range falsePositives {
		if matchesFilenameKeyword(strings.ToLower(tc.name), tc.keyword) {
			t.Fatalf("%q must not match keyword %q as a bare substring", tc.name, tc.keyword)
		}
	}

	truePositives := []struct{ name, keyword string }{
		{"cover.jpg", "cover"},
		{"cover01.jpg", "cover"},
		{"fc.jpg", "fc"},
		{"fc02.png", "fc"},
		{"page-ad.jpg", "ad"},
		{"01_bc.jpg", "bc"},
		{"封面.jpg", "封面"},
	}
	for _, tc := range truePositives {
		if !matchesFilenameKeyword(strings.ToLower(tc.name), tc.keyword) {
			t.Fatalf("%q should match keyword %q", tc.name, tc.keyword)
		}
	}
}

// TestNaturalCompareDoesNotMisclassifyOrdinaryNames 是上面那条的端到端版本：
// 名字里恰好含 "ad" 的正常页不应被排到普通页之后。
func TestNaturalCompareDoesNotMisclassifyOrdinaryNames(t *testing.T) {
	// "Loaded.jpg" 曾因含 "ad" 被判为广告页而排到最后；按页序它应排在 002 之前。
	if !naturalCompare("001_Loaded.jpg", "002_Normal.jpg") {
		t.Fatal("a page whose name merely contains \"ad\" must not be demoted below later pages")
	}
}

// TestParseComicInfoToleratesDirtyNumericFields 锁住脏数值字段不再拖垮整份元数据。
// 各路打包工具生成的 ComicInfo.xml 里，数值字段出现 "N/A"、"" 这类内容很常见；
// 用裸 int/float 时 xml.Unmarshal 会整份失败，而调用方两层都吞掉了这个错误，
// 用户只会看到「这本书没有内嵌元数据」。
func TestParseComicInfoToleratesDirtyNumericFields(t *testing.T) {
	raw := []byte(`<?xml version="1.0"?>
<ComicInfo>
  <Title>Chapter One</Title>
  <Series>My Series</Series>
  <PageCount>N/A</PageCount>
  <Count></Count>
  <CommunityRating>unrated</CommunityRating>
</ComicInfo>`)

	info, err := ParseComicInfo(raw)
	if err != nil {
		t.Fatalf("dirty numeric fields must not fail the whole document: %v", err)
	}
	if info.Title != "Chapter One" || info.Series != "My Series" {
		t.Fatalf("text fields lost: %+v", info)
	}
	if info.PageCount != 0 || info.Count != 0 || info.CommunityRating != 0 {
		t.Fatalf("unparseable numerics should fall back to zero, got %+v", info)
	}
}

func TestParseComicInfoStripsBOMAndTolerantOfCharsetDecl(t *testing.T) {
	// 带 BOM：xml.Decoder 会在根元素之前遇到非法字符而报错。
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`<ComicInfo><Title>T</Title></ComicInfo>`)...)
	if info, err := ParseComicInfo(withBOM); err != nil || info.Title != "T" {
		t.Fatalf("BOM-prefixed document should parse, got info=%+v err=%v", info, err)
	}

	// 非 UTF-8 编码声明：没有 CharsetReader 时 xml 包直接拒绝解析，整份元数据丢失。
	withCharset := []byte(`<?xml version="1.0" encoding="gbk"?><ComicInfo><Title>T</Title></ComicInfo>`)
	if info, err := ParseComicInfo(withCharset); err != nil || info.Title != "T" {
		t.Fatalf("non-UTF-8 charset declaration should not lose the document, got info=%+v err=%v", info, err)
	}
}
