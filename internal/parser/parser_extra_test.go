// 业务说明：本文件是业务回归测试，覆盖归档格式分发、ComicInfo 序列化/解析与作者标签提取、
// zip 页过滤与自然排序、媒体类型判定，以及 ComicInfo 写回归档后的端到端读回。

package parser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenArchiveDispatch 覆盖按后缀分发到 zip/rar 驱动与不支持格式的拒绝。
func TestOpenArchiveDispatch(t *testing.T) {
	dir := t.TempDir()

	// 真实 cbz -> 走 zip 驱动，可列页。
	cbz := filepath.Join(dir, "book.cbz")
	writeTestCBZ(t, cbz, map[string]string{"001.jpg": "img"})
	arc, err := OpenArchive(cbz)
	if err != nil {
		t.Fatalf("open cbz failed: %v", err)
	}
	if _, ok := arc.(*ZipArchive); !ok {
		t.Fatalf("expected *ZipArchive, got %T", arc)
	}
	_ = arc.Close()

	// 大写后缀也应识别（后缀比较大小写不敏感）。
	cbzUpper := filepath.Join(dir, "book.CBZ")
	writeTestCBZ(t, cbzUpper, map[string]string{"001.jpg": "img"})
	if arc2, err := OpenArchive(cbzUpper); err != nil {
		t.Fatalf("open .CBZ failed: %v", err)
	} else {
		_ = arc2.Close()
	}

	// cbr 走 rar 驱动，惰性打开：即使文件不存在也不报错（延迟到读取时）。
	rarArc, err := OpenArchive(filepath.Join(dir, "missing.cbr"))
	if err != nil {
		t.Fatalf("rar open should be lazy, got err %v", err)
	}
	if _, ok := rarArc.(*RarArchive); !ok {
		t.Fatalf("expected *RarArchive, got %T", rarArc)
	}
	_ = rarArc.Close()

	// 不支持的后缀 -> 明确错误。
	if _, err := OpenArchive(filepath.Join(dir, "doc.pdf")); err == nil {
		t.Fatalf("expected unsupported-format error for .pdf")
	}

	// zip 驱动打开不存在的文件 -> 错误。
	if _, err := OpenArchive(filepath.Join(dir, "nope.cbz")); err == nil {
		t.Fatalf("expected error opening non-existent cbz")
	}
}

// TestComicInfoRoundTripFull 覆盖完整字段的 marshal/unmarshal 往返与 omitempty。
func TestComicInfoRoundTripFull(t *testing.T) {
	in := ComicInfo{
		Title:           "Book Title",
		Series:          "Series Title",
		Number:          "3",
		Count:           12,
		Volume:          "2",
		Summary:         "a summary",
		Writer:          "Alice, Bob",
		Penciller:       "Carol",
		Translator:      "Dan, Eve",
		Publisher:       "Pub",
		Genre:           "Action, Drama",
		LanguageISO:     "zh",
		Manga:           "YesAndRightToLeft",
		CommunityRating: 4.5,
		PageCount:       188,
	}
	data, err := MarshalComicInfo(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := ParseComicInfo(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.Title != in.Title || out.Series != in.Series || out.Number != in.Number ||
		out.Count != in.Count || out.Volume != in.Volume || out.PageCount != in.PageCount ||
		out.CommunityRating != in.CommunityRating || out.LanguageISO != in.LanguageISO {
		t.Fatalf("round-trip mismatch: %+v", out)
	}

	// omitempty：空结构体不应写出零值标签。
	emptyData, err := MarshalComicInfo(ComicInfo{})
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if strings.Contains(string(emptyData), "<Count>") || strings.Contains(string(emptyData), "<PageCount>") {
		t.Fatalf("empty ComicInfo should omit zero-value elements: %s", emptyData)
	}
}

func TestParseComicInfoRejectsInvalidXML(t *testing.T) {
	if _, err := ParseComicInfo([]byte("<ComicInfo><Title>unclosed")); err == nil {
		t.Fatalf("expected error parsing malformed XML")
	}
}

func TestComicInfoGetTags(t *testing.T) {
	c := ComicInfo{Genre: "Action, Drama ,, Sci-Fi "}
	tags := c.GetTags()
	want := []string{"Action", "Drama", "Sci-Fi"}
	if len(tags) != len(want) {
		t.Fatalf("GetTags = %v, want %v", tags, want)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("GetTags[%d] = %q, want %q", i, tags[i], want[i])
		}
	}
	if (&ComicInfo{}).GetTags() != nil {
		t.Fatalf("empty genre should yield nil tags")
	}
}

func TestComicInfoGetAuthors(t *testing.T) {
	c := ComicInfo{Writer: "Alice, Bob", Penciller: "Carol", Translator: "Dan, Eve"}
	authors := c.GetAuthors()
	// 顺序：writer, writer, penciller, translator, translator。
	want := []AuthorRole{
		{"Alice", "writer"},
		{"Bob", "writer"},
		{"Carol", "penciller"},
		{"Dan", "translator"},
		{"Eve", "translator"},
	}
	if len(authors) != len(want) {
		t.Fatalf("GetAuthors count = %d, want %d (%+v)", len(authors), len(want), authors)
	}
	for i := range want {
		if authors[i] != want[i] {
			t.Fatalf("GetAuthors[%d] = %+v, want %+v", i, authors[i], want[i])
		}
	}
	if got := (&ComicInfo{}).GetAuthors(); len(got) != 0 {
		t.Fatalf("no author fields should yield empty slice, got %+v", got)
	}
}

func TestGetMediaType(t *testing.T) {
	cases := map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".webp": "image/webp",
		".avif": "image/avif",
		".txt":  "application/octet-stream",
	}
	for ext, want := range cases {
		if got := getMediaType(ext); got != want {
			t.Fatalf("getMediaType(%q) = %q, want %q", ext, got, want)
		}
	}
}

// TestZipArchiveGetPagesFiltersAndSorts 覆盖 GetPages 的过滤（隐藏/非图片/目录/元数据）与自然排序（封面优先）。
func TestZipArchiveGetPagesFiltersAndSorts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vol.cbz")
	writeTestCBZ(t, path, map[string]string{
		"10.jpg":        "j10",
		"2.jpg":         "j2",
		"1.jpg":         "j1",
		"cover.jpg":     "cov",
		".hidden.jpg":   "hid",  // 隐藏文件应被过滤
		"readme.txt":    "note", // 非图片应被过滤
		"ComicInfo.xml": "<ComicInfo/>",
	})

	arc, err := OpenArchive(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer arc.Close()

	pages, err := arc.GetPages()
	if err != nil {
		t.Fatalf("GetPages: %v", err)
	}
	gotNames := make([]string, len(pages))
	for i, p := range pages {
		gotNames[i] = p.Name
	}
	want := []string{"cover.jpg", "1.jpg", "2.jpg", "10.jpg"}
	if len(gotNames) != len(want) {
		t.Fatalf("GetPages returned %v, want %v", gotNames, want)
	}
	for i := range want {
		if gotNames[i] != want[i] {
			t.Fatalf("page order[%d] = %q, want %q (all=%v)", i, gotNames[i], want[i], gotNames)
		}
	}
	if pages[0].MediaType != "image/jpeg" {
		t.Fatalf("cover media type = %q, want image/jpeg", pages[0].MediaType)
	}

	// ReadPage 精确命中；缺失页报错。
	if got, err := arc.ReadPage("1.jpg"); err != nil || string(got) != "j1" {
		t.Fatalf("ReadPage(1.jpg) = %q,%v", got, err)
	}
	if _, err := arc.ReadPage("nope.jpg"); err == nil {
		t.Fatalf("expected error reading missing page")
	}
	// ReadMetadataFile 能读到非页条目 ComicInfo.xml。
	if got, err := arc.ReadMetadataFile("ComicInfo.xml"); err != nil || string(got) != "<ComicInfo/>" {
		t.Fatalf("ReadMetadataFile(ComicInfo.xml) = %q,%v", got, err)
	}
}

// TestWriteComicInfoEndToEnd 覆盖：写回 ComicInfo 后归档仍可打开、页数不变、元数据可解析回读。
func TestWriteComicInfoEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vol01.cbz")
	writeTestCBZ(t, path, map[string]string{"001.jpg": "a", "002.jpg": "b"})

	xmlData, err := MarshalComicInfo(ComicInfo{Series: "MySeries", Number: "7", PageCount: 2})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := WriteComicInfoIntoArchive(path, xmlData); err != nil {
		t.Fatalf("write comicinfo: %v", err)
	}

	arc, err := OpenArchive(path)
	if err != nil {
		t.Fatalf("reopen archive: %v", err)
	}
	defer arc.Close()

	pages, err := arc.GetPages()
	if err != nil {
		t.Fatalf("GetPages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages preserved (ComicInfo.xml not counted), got %d", len(pages))
	}

	raw, err := arc.ReadMetadataFile("ComicInfo.xml")
	if err != nil {
		t.Fatalf("read ComicInfo.xml: %v", err)
	}
	info, err := ParseComicInfo(raw)
	if err != nil {
		t.Fatalf("parse written ComicInfo: %v", err)
	}
	if info.Series != "MySeries" || info.Number != "7" || info.PageCount != 2 {
		t.Fatalf("round-trip metadata mismatch: %+v", info)
	}
}

// TestWriteComicInfoRejectsUnknownExtension 非 zip/cbz 后缀应拒绝写入。
func TestWriteComicInfoRejectsUnknownExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.7z")
	if err := os.WriteFile(path, []byte("not a zip"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := WriteComicInfoIntoArchive(path, []byte("<ComicInfo/>")); !errors.Is(err, ErrArchiveNotWritable) {
		t.Fatalf("expected ErrArchiveNotWritable for .7z, got %v", err)
	}
}
