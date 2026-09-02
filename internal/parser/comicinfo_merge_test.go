// 这些测试守的是 ComicInfo 回写的**无损**不变量：本项目不建模的字段、已建模但这次没值的字段、
// 元素顺序、注释与命名空间声明都必须活过一次回写。破了就是用户用别的工具打好的标被静默抹掉，
// 而回写是原子替换且不留备份，只能从原始文件重新拿回。

package parser

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// taggedComicInfoXML 是一份 ComicTagger/ComicRack 打过标的真实形状文档：
// 除本项目建模的字段外，还带出版日期、其余署名、阅读方向、分级等本项目不建模的元素。
const taggedComicInfoXML = `<?xml version="1.0" encoding="utf-8"?>
<ComicInfo xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <Title>旧标题</Title>
  <Series>旧系列</Series>
  <Number>1</Number>
  <Count>20</Count>
  <Volume>1</Volume>
  <Summary>旧简介</Summary>
  <Notes>Tagged with ComicTagger 1.6.0</Notes>
  <Year>1995</Year>
  <Month>2</Month>
  <Day>15</Day>
  <Writer>旧作者</Writer>
  <Penciller>旧作画</Penciller>
  <Inker>某上墨</Inker>
  <Colorist>某上色</Colorist>
  <Letterer>某嵌字</Letterer>
  <CoverArtist>某封面</CoverArtist>
  <Publisher>旧出版社</Publisher>
  <Genre>旧标签</Genre>
  <Web>https://example.com/monster</Web>
  <PageCount>200</PageCount>
  <LanguageISO>ja</LanguageISO>
  <BlackAndWhite>Yes</BlackAndWhite>
  <Manga>YesAndRightToLeft</Manga>
  <Characters>Kenzo Tenma, Johan Liebert</Characters>
  <ScanInformation>v2 scan</ScanInformation>
  <StoryArc>Monster</StoryArc>
  <AgeRating>Mature 17+</AgeRating>
  <Rating>4.5</Rating>
</ComicInfo>`

// comicInfoElements 把根元素下的直接子元素读成「元素名 -> 正文」，用来逐字段断言。
func comicInfoElements(t *testing.T, data []byte) map[string]string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	out := make(map[string]string)
	depth := 0
	current := ""
	for {
		token, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("解析回写后的 ComicInfo.xml 失败: %v", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				current = typed.Name.Local
				out[current] = ""
			}
		case xml.CharData:
			if depth == 2 && current != "" {
				out[current] += string(typed)
			}
		case xml.EndElement:
			if depth == 2 {
				current = ""
			}
			depth--
		}
	}
	return out
}

// readArchiveComicInfo 取出归档里的 ComicInfo.xml 原始字节。
func readArchiveComicInfo(t *testing.T, path string) []byte {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("打开归档失败: %v", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if !strings.EqualFold(f.Name, "ComicInfo.xml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("打开 ComicInfo.xml 条目失败: %v", err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("读取 ComicInfo.xml 失败: %v", err)
		}
		return data
	}
	t.Fatalf("归档里没有 ComicInfo.xml")
	return nil
}

// libraryComicInfo 是本项目今天能从库里聚合出来的那些字段——与 api.buildComicInfoForBook 同形。
func libraryComicInfo() ComicInfo {
	return ComicInfo{
		Title:           "新标题",
		Series:          "新系列",
		Number:          "1",
		Volume:          "1",
		Summary:         "新简介",
		Writer:          "新作者",
		Penciller:       "新作画",
		Publisher:       "新出版社",
		Genre:           "悬疑, 剧情",
		LanguageISO:     "zh",
		PageCount:       LenientInt(200),
		CommunityRating: LenientFloat(4.5),
	}
}

func TestWriteComicInfoIntoArchiveKeepsUnmodeledFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vol01.cbz")
	writeTestCBZ(t, path, map[string]string{
		"001.jpg":       "imgdata",
		"ComicInfo.xml": taggedComicInfoXML,
	})

	if err := WriteComicInfoIntoArchive(path, libraryComicInfo()); err != nil {
		t.Fatalf("回写失败: %v", err)
	}
	got := comicInfoElements(t, readArchiveComicInfo(t, path))

	// 本项目既不建模、也无从编辑的字段，必须逐字保留。
	unmodeled := map[string]string{
		"Year":            "1995",
		"Month":           "2",
		"Day":             "15",
		"Inker":           "某上墨",
		"Colorist":        "某上色",
		"CoverArtist":     "某封面",
		"Characters":      "Kenzo Tenma, Johan Liebert",
		"StoryArc":        "Monster",
		"Notes":           "Tagged with ComicTagger 1.6.0",
		"ScanInformation": "v2 scan",
		"BlackAndWhite":   "Yes",
		"AgeRating":       "Mature 17+",
	}
	for name, want := range unmodeled {
		if got[name] != want {
			t.Errorf("未建模字段 %s 应原样保留 %q，实得 %q", name, want, got[name])
		}
	}

	// 结构体已建模、但本项目填不出值的字段，同样不该被 omitempty 抹掉。
	modeledButUnfilled := map[string]string{
		"Web":      "https://example.com/monster",
		"Manga":    "YesAndRightToLeft",
		"Rating":   "4.5",
		"Letterer": "某嵌字",
	}
	for name, want := range modeledButUnfilled {
		if got[name] != want {
			t.Errorf("已建模但本次没值的字段 %s 应保留 %q，实得 %q", name, want, got[name])
		}
	}

	// 本项目管的字段按库里的值更新。
	updated := map[string]string{
		"Title":       "新标题",
		"Series":      "新系列",
		"Summary":     "新简介",
		"Writer":      "新作者",
		"Penciller":   "新作画",
		"Publisher":   "新出版社",
		"Genre":       "悬疑, 剧情",
		"LanguageISO": "zh",
		"PageCount":   "200",
	}
	for name, want := range updated {
		if got[name] != want {
			t.Errorf("本项目管的字段 %s 应更新为 %q，实得 %q", name, want, got[name])
		}
	}
	if got["CommunityRating"] != "4.5" {
		t.Errorf("CommunityRating 应写入 4.5，实得 %q", got["CommunityRating"])
	}

	// Count 是「本系列共几卷」，本项目没有这个数据，不得改写。
	if got["Count"] != "20" {
		t.Errorf("Count 应保留归档原值 20，实得 %q", got["Count"])
	}

	// 页条目不受影响。
	if entries := readArchiveEntries(t, path); entries["001.jpg"] != "imgdata" {
		t.Errorf("页条目被破坏: %q", entries["001.jpg"])
	}
}

func TestMergeComicInfoXMLKeepsOriginalWhenValueEmpty(t *testing.T) {
	merged, err := MergeComicInfoXML([]byte(taggedComicInfoXML), ComicInfo{Title: "只改标题"})
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	got := comicInfoElements(t, merged)
	if got["Title"] != "只改标题" {
		t.Errorf("Title 应更新，实得 %q", got["Title"])
	}
	// 空值表示「本项目没有可写的内容」，不是「清空」：归档原值必须留下。
	if got["Summary"] != "旧简介" {
		t.Errorf("空 Summary 应保留归档原值，实得 %q", got["Summary"])
	}
	if got["Publisher"] != "旧出版社" {
		t.Errorf("空 Publisher 应保留归档原值，实得 %q", got["Publisher"])
	}
}

func TestMergeComicInfoXMLPreservesNamespacesCommentsAndPages(t *testing.T) {
	const withExtras = `<?xml version="1.0" encoding="utf-8"?>
<ComicInfo xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <!-- 手工加的备注 -->
  <Title>旧标题</Title>
  <Pages>
    <Page Image="0" ImageSize="1234" Type="FrontCover" />
    <Page Image="1" ImageSize="2345" />
  </Pages>
</ComicInfo>`

	merged, err := MergeComicInfoXML([]byte(withExtras), ComicInfo{Title: "新标题"})
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	text := string(merged)
	for _, want := range []string{
		`xmlns:xsd="http://www.w3.org/2001/XMLSchema"`,
		`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`,
		`<!-- 手工加的备注 -->`,
		`<Page Image="0" ImageSize="1234" Type="FrontCover">`,
		`<Page Image="1" ImageSize="2345">`,
		`<Title>新标题</Title>`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("合并结果应包含 %q，实得:\n%s", want, text)
		}
	}
	if strings.Contains(text, "_xmlns") {
		t.Errorf("命名空间声明被编码器写坏:\n%s", text)
	}
	if !strings.HasPrefix(text, xml.Header) {
		t.Errorf("应写出 UTF-8 编码声明，实得开头 %q", text[:40])
	}
}

func TestMergeComicInfoXMLInsertsNewElementsInSchemaOrder(t *testing.T) {
	const sparse = `<ComicInfo><Title>T</Title><PageCount>10</PageCount></ComicInfo>`

	merged, err := MergeComicInfoXML([]byte(sparse), ComicInfo{Series: "S", Summary: "abstract", LanguageISO: "zh"})
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	text := string(merged)
	// xsd 把子元素声明为 sequence：Series 在 Title 之后、Summary 之前，LanguageISO 在 PageCount 之后。
	order := []string{"<Title>", "<Series>", "<Summary>", "<PageCount>", "<LanguageISO>"}
	at := -1
	for _, tag := range order {
		idx := strings.Index(text, tag)
		if idx < 0 {
			t.Fatalf("合并结果缺少 %s:\n%s", tag, text)
		}
		if idx < at {
			t.Fatalf("元素 %s 未落在 xsd 顺序上:\n%s", tag, text)
		}
		at = idx
	}
}

func TestMergeComicInfoXMLCollapsesDuplicateManagedElements(t *testing.T) {
	const duplicated = `<ComicInfo><Title>甲</Title><Title>乙</Title></ComicInfo>`

	merged, err := MergeComicInfoXML([]byte(duplicated), ComicInfo{Title: "丙"})
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	if n := strings.Count(string(merged), "<Title>"); n != 1 {
		t.Fatalf("同名重复元素应收敛成一个，实得 %d 个:\n%s", n, merged)
	}
}

func TestMergeComicInfoXMLRefusesUnsafeOriginal(t *testing.T) {
	// GBK 正文：本包不带转码器，硬改写会把中文变成替换字符，只能整份不动。
	gbk := append([]byte(`<?xml version="1.0" encoding="gbk"?><ComicInfo><Title>`), 0xBA, 0xBA, 0xD7, 0xD6)
	gbk = append(gbk, []byte(`</Title></ComicInfo>`)...)
	if _, err := MergeComicInfoXML(gbk, ComicInfo{Series: "S"}); !errors.Is(err, ErrComicInfoNotMergeable) {
		t.Fatalf("非 UTF-8 文档应返回 ErrComicInfoNotMergeable，实得 %v", err)
	}
	if _, err := MergeComicInfoXML([]byte(`<ComicInfo><Title>没闭合`), ComicInfo{Series: "S"}); !errors.Is(err, ErrComicInfoNotMergeable) {
		t.Fatalf("语法错误的文档应返回 ErrComicInfoNotMergeable，实得 %v", err)
	}
}

// TestWriteComicInfoIntoArchiveKeepsArchiveOnUnsafeOriginal 守的是「拒绝写入时归档原样不动」。
func TestWriteComicInfoIntoArchiveKeepsArchiveOnUnsafeOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vol01.cbz")
	broken := `<ComicInfo><Title>没闭合`
	writeTestCBZ(t, path, map[string]string{"001.jpg": "imgdata", "ComicInfo.xml": broken})

	if err := WriteComicInfoIntoArchive(path, libraryComicInfo()); !errors.Is(err, ErrComicInfoNotMergeable) {
		t.Fatalf("应返回 ErrComicInfoNotMergeable，实得 %v", err)
	}
	if got := string(readArchiveComicInfo(t, path)); got != broken {
		t.Fatalf("拒绝写入后归档应原样不动，实得 %q", got)
	}
	// 临时文件不得残留。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读目录失败: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("拒绝写入后不该留下临时文件，目录里有 %d 个条目", len(entries))
	}
}

func TestWriteComicInfoIntoArchiveCreatesDocumentWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vol01.cbz")
	writeTestCBZ(t, path, map[string]string{"001.jpg": "imgdata"})

	if err := WriteComicInfoIntoArchive(path, ComicInfo{Series: "新系列", Number: "3"}); err != nil {
		t.Fatalf("回写失败: %v", err)
	}
	got := comicInfoElements(t, readArchiveComicInfo(t, path))
	if got["Series"] != "新系列" || got["Number"] != "3" {
		t.Fatalf("新建文档字段不对: %+v", got)
	}
	// 本项目没有「本系列共几卷」这个数据，新建时也不得凭空写一个 Count。
	if _, ok := got["Count"]; ok {
		t.Fatalf("不该写出 Count，实得 %q", got["Count"])
	}
}
