// ComicInfo.xml 的节点级合并：本项目确有值的字段覆盖同名元素，其余节点连同顺序、注释、
// 命名空间声明与 <Pages> 子树原样留下。合并而非重建，是因为归档里的字段远多于本项目建模的那些，
// 而回写不可逆也不备份。维护时应关注：Go 的 xml 编码器会写坏 xmlns 属性名，必须还原。

package parser

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrComicInfoNotMergeable 表示归档里已有的 ComicInfo.xml 无法在不损坏原文的前提下改写。
// 触发它的是非 UTF-8 字节或语法错误的文档：本包不带转码器，硬改写会把原有的中日文正文变成
// 替换字符，而整份重建又会抹掉本项目不建模的字段。调用方应把这本书按「跳过」处理并告知用户。
var ErrComicInfoNotMergeable = errors.New("existing ComicInfo.xml cannot be merged safely")

// comicInfoSchemaOrder 是 ComicInfo.xsd 定义的元素顺序。
// 它只用来给**新增**的元素定位置——原文件已有的元素一律保持原顺序。xsd 把子元素声明为
// sequence，乱序会让按 schema 校验的阅读器读不出内容。Translator 不在 ComicRack 2.0 里，
// 按 anansi-project 的 2.1 草案排在 Editor 与 Publisher 之间。
var comicInfoSchemaOrder = []string{
	"Title", "Series", "Number", "Count", "Volume",
	"AlternateSeries", "AlternateNumber", "AlternateCount",
	"Summary", "Notes", "Year", "Month", "Day",
	"Writer", "Penciller", "Inker", "Colorist", "Letterer", "CoverArtist",
	"Editor", "Translator", "Publisher", "Imprint", "Genre", "Web",
	"PageCount", "LanguageISO", "Format", "BlackAndWhite", "Manga",
	"Characters", "Teams", "Locations", "ScanInformation",
	"StoryArc", "SeriesGroup", "AgeRating", "Pages",
	"CommunityRating", "MainCharacterOrTeam", "Review",
}

// comicInfoSchemaRank 给出元素在 xsd sequence 里的位次，未知元素返回 -1。
func comicInfoSchemaRank(name string) int {
	for i, known := range comicInfoSchemaOrder {
		if strings.EqualFold(known, name) {
			return i
		}
	}
	return -1
}

// comicInfoNode 是根元素下的一个直接子节点：一个元素连同其整棵子树，或一条注释。
// 元素节点带 name，注释等非元素节点 name 为空——合并只按 name 找覆盖目标，其余原样搬运。
type comicInfoNode struct {
	name   string
	tokens []xml.Token
}

// comicInfoDocument 是拆开后的 ComicInfo.xml：序言注释、根元素（含其属性）与全部直接子节点。
type comicInfoDocument struct {
	prologue []xml.Token
	root     xml.StartElement
	children []comicInfoNode
}

// MergeComicInfoXML 把 info 里非空的字段并进 original，返回新的 UTF-8 文档。
//
// 空值表示「本项目对这个字段没有可写的内容」，一律**保留归档里的原值**而不是清空：本项目不提供
// 「清除该字段」这个意图，空既可能是没刮到也可能是用户删过，两者不可区分，而误删不可逆。
// original 为空时按新文档处理。original 非 UTF-8 或语法错误时返回 ErrComicInfoNotMergeable。
func MergeComicInfoXML(original []byte, info ComicInfo) ([]byte, error) {
	doc, err := parseComicInfoDocument(original)
	if err != nil {
		return nil, err
	}
	for _, value := range managedComicInfoValues(info) {
		if value.text == "" {
			continue
		}
		doc.children = upsertComicInfoElement(doc.children, value.name, value.text)
	}
	return encodeComicInfoDocument(doc)
}

// parseComicInfoDocument 把文档拆成序言、根与直接子节点；data 为空时给出一份空的 ComicInfo。
func parseComicInfoDocument(data []byte) (comicInfoDocument, error) {
	doc := comicInfoDocument{root: xml.StartElement{Name: xml.Name{Local: comicInfoRootName}}}

	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if len(bytes.TrimSpace(data)) == 0 {
		return doc, nil
	}
	// 先验字节：解码器的 CharsetReader 是尽力透传，GBK 之类的正文会以非法 UTF-8 的形式流过来，
	// 再编码出去就成了一串替换字符。这种文档只能整份不动。
	if !utf8.Valid(data) {
		return comicInfoDocument{}, ErrComicInfoNotMergeable
	}

	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = comicInfoCharsetReader

	depth := 0
	curIdx := -1
	for {
		token, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return comicInfoDocument{}, ErrComicInfoNotMergeable
		}

		switch typed := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				doc.root = localStartElement(typed)
				depth++
				continue
			}
			if depth == 1 {
				doc.children = append(doc.children, comicInfoNode{name: typed.Name.Local})
				curIdx = len(doc.children) - 1
			}
			doc.children[curIdx].tokens = append(doc.children[curIdx].tokens, localStartElement(typed))
			depth++
		case xml.EndElement:
			depth--
			if depth == 0 {
				continue
			}
			end := xml.EndElement{Name: xml.Name{Local: typed.Name.Local}}
			doc.children[curIdx].tokens = append(doc.children[curIdx].tokens, end)
			if depth == 1 {
				curIdx = -1
			}
		case xml.CharData:
			if strings.TrimSpace(string(typed)) == "" || curIdx < 0 {
				continue
			}
			doc.children[curIdx].tokens = append(doc.children[curIdx].tokens, xml.CopyToken(typed))
		case xml.Comment:
			switch {
			case depth == 0:
				doc.prologue = append(doc.prologue, xml.CopyToken(typed))
			case curIdx < 0:
				doc.children = append(doc.children, comicInfoNode{tokens: []xml.Token{xml.CopyToken(typed)}})
			default:
				doc.children[curIdx].tokens = append(doc.children[curIdx].tokens, xml.CopyToken(typed))
			}
		}
	}
	return doc, nil
}

// upsertComicInfoElement 用 text 覆盖同名元素的正文；没有同名元素时按 xsd 顺序插入一个。
// 覆盖保留原元素的名字拼写与属性，并丢掉其后的同名重复元素——重复本就不合 schema，
// 留着会让阅读器取到哪一个变成运气。
func upsertComicInfoElement(children []comicInfoNode, name, text string) []comicInfoNode {
	target := -1
	kept := children[:0]
	for _, node := range children {
		if node.name == "" || !strings.EqualFold(node.name, name) {
			kept = append(kept, node)
			continue
		}
		if target >= 0 {
			continue
		}
		start, ok := node.tokens[0].(xml.StartElement)
		if !ok {
			kept = append(kept, node)
			continue
		}
		node.tokens = []xml.Token{start, xml.CharData(text), xml.EndElement{Name: start.Name}}
		target = len(kept)
		kept = append(kept, node)
	}
	if target >= 0 {
		return kept
	}

	fresh := comicInfoNode{name: name, tokens: []xml.Token{
		xml.StartElement{Name: xml.Name{Local: name}},
		xml.CharData(text),
		xml.EndElement{Name: xml.Name{Local: name}},
	}}
	rank := comicInfoSchemaRank(name)
	at := len(kept)
	if rank >= 0 {
		for i, node := range kept {
			if r := comicInfoSchemaRank(node.name); r > rank {
				at = i
				break
			}
		}
	}
	kept = append(kept, comicInfoNode{})
	copy(kept[at+1:], kept[at:])
	kept[at] = fresh
	return kept
}

// encodeComicInfoDocument 把拆开的文档写回 UTF-8 字节。
// 编码声明由本函数统一写成 UTF-8——写出去的就是 UTF-8，声明必须跟着，否则原文件那句
// encoding="gbk" 会指着一份 UTF-8 正文。
func encodeComicInfoDocument(doc comicInfoDocument) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)

	encoder := xml.NewEncoder(&buffer)
	encoder.Indent("", "  ")
	for _, token := range doc.prologue {
		if err := encoder.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if err := encoder.EncodeToken(doc.root); err != nil {
		return nil, err
	}
	for _, node := range doc.children {
		for _, token := range node.tokens {
			if err := encoder.EncodeToken(token); err != nil {
				return nil, err
			}
		}
	}
	if err := encoder.EncodeToken(xml.EndElement{Name: doc.root.Name}); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	buffer.WriteString("\n")
	return buffer.Bytes(), nil
}

// localStartElement 把解码器解析出的命名空间形态还原成原文的写法。
//
// 解码器把 xmlns:xsd="…" 拆成 Space="xmlns"/Local="xsd"，编码器再照着写会吐出
// xmlns:_xmlns="xmlns" _xmlns:xsd="…"，等于把根元素的命名空间声明改坏。把前缀拼回 Local、
// 清空 Space 后，声明原样保留，带默认命名空间的文档也照旧。
func localStartElement(start xml.StartElement) xml.StartElement {
	attrs := make([]xml.Attr, len(start.Attr))
	for i, attr := range start.Attr {
		name := xml.Name{Local: attr.Name.Local}
		if attr.Name.Space != "" {
			name.Local = attr.Name.Space + ":" + attr.Name.Local
		}
		attrs[i] = xml.Attr{Name: name, Value: attr.Value}
	}
	return xml.StartElement{Name: xml.Name{Local: start.Name.Local}, Attr: attrs}
}

// comicInfoValue 是一个待写入的元素及其正文，空正文表示本项目对该字段没有可写的内容。
type comicInfoValue struct {
	name string
	text string
}

// managedComicInfoValues 列出 ComicInfo 结构体建模的全部字段。
// 清单与结构体一致而不是与某个调用方一致：调用方填不出的字段留空即可，
// 而清单漏掉一个已建模字段，就等于那个字段在合并时无声地失去覆盖能力。
func managedComicInfoValues(info ComicInfo) []comicInfoValue {
	return []comicInfoValue{
		{"Title", info.Title},
		{"Series", info.Series},
		{"Number", info.Number},
		{"Count", formatLenientInt(info.Count)},
		{"Volume", info.Volume},
		{"Summary", info.Summary},
		{"Writer", info.Writer},
		{"Penciller", info.Penciller},
		{"Letterer", info.Letterer},
		{"Translator", info.Translator},
		{"Publisher", info.Publisher},
		{"Genre", info.Genre},
		{"Web", info.Web},
		{"LanguageISO", info.LanguageISO},
		{"Manga", info.Manga},
		{"Rating", info.Rating},
		{"CommunityRating", formatLenientFloat(info.CommunityRating)},
		{"PageCount", formatLenientInt(info.PageCount)},
	}
}

func formatLenientInt(value LenientInt) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(int(value))
}

func formatLenientFloat(value LenientFloat) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatFloat(float64(value), 'g', -1, 32)
}
