package parser

import (
	"bytes"
	"encoding/xml"
	"io"
	"log/slog"
	"strconv"
	"strings"
)

// ComicInfo 代表了基于 ComicRack 约定的标准内嵌元数据格式
type ComicInfo struct {
	XMLName         xml.Name     `xml:"ComicInfo"`
	Title           string       `xml:"Title,omitempty"`
	Series          string       `xml:"Series,omitempty"`
	Number          string       `xml:"Number,omitempty"`
	Count           LenientInt   `xml:"Count,omitempty"`
	Volume          string       `xml:"Volume,omitempty"`
	Summary         string       `xml:"Summary,omitempty"`
	Writer          string       `xml:"Writer,omitempty"`
	Penciller       string       `xml:"Penciller,omitempty"`
	Letterer        string       `xml:"Letterer,omitempty"`
	Translator      string       `xml:"Translator,omitempty"`
	Publisher       string       `xml:"Publisher,omitempty"`
	Genre           string       `xml:"Genre,omitempty"`
	Web             string       `xml:"Web,omitempty"`
	LanguageISO     string       `xml:"LanguageISO,omitempty"`
	Manga           string       `xml:"Manga,omitempty"`           // Yes, No, RightToLeft
	Rating          string       `xml:"Rating,omitempty"`          // Unknown, Rating Pending, Early Childhood, Everyone, Everyone 10+, Teen, Mature 17+, Adults Only 18+  (可选项，数字如 4.0 也常见)
	CommunityRating LenientFloat `xml:"CommunityRating,omitempty"` // 真实分值 0.0 - 5.0
	PageCount       LenientInt   `xml:"PageCount,omitempty"`
}

// LenientInt / LenientFloat 是能容忍脏值的数值类型。
//
// ComicInfo.xml 由各路打包工具生成，数值字段里出现 ""、"N/A"、"unknown"、"5 issues"
// 这类内容很常见。用裸 int/float32 时，xml.Unmarshal 会因为单个脏字段直接失败，
// 整份元数据（标题、简介、作者……）随之全部丢失——而调用方两层都把这个错误吞掉了，
// 用户只会看到"这本书没有内嵌元数据"，无从判断原因。
type LenientInt int

func (v *LenientInt) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var raw string
	if err := d.DecodeElement(&raw, &start); err != nil {
		return err
	}
	// 解析失败保持零值即可：单个脏字段不该让整份文档失败。
	if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		*v = LenientInt(n)
	}
	return nil
}

type LenientFloat float32

func (v *LenientFloat) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var raw string
	if err := d.DecodeElement(&raw, &start); err != nil {
		return err
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(raw), 32); err == nil {
		*v = LenientFloat(f)
	}
	return nil
}

// comicInfoCharsetReader 处理非 UTF-8 的编码声明。
//
// 没有它时，一份 <?xml version="1.0" encoding="gbk"?> 的 ComicInfo.xml 会让
// xml.Unmarshal 直接报 "encoding declared but Decoder.CharsetReader is nil"，
// 整份元数据丢失。本项目不引入 x/net/html/charset（为一个边缘场景加依赖不划算），
// 改为尽力透传：ASCII 兼容编码（GBK/Shift_JIS/windows-125x/ISO-8859-x）下，
// 数字与西文字段仍能正确解析，非 ASCII 文本可能乱码——但这远好过整份丢失。
func comicInfoCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	slog.Debug("ComicInfo declares a non-UTF-8 charset; decoding best-effort", "charset", charset)
	return input, nil
}

// ParseComicInfo 从 XML 字节流中反序列化 ComicInfo。
// 对编码声明与脏数值字段都做了容错，尽量不让单点问题吞掉整份元数据。
func ParseComicInfo(data []byte) (*ComicInfo, error) {
	// 剥掉 UTF-8 BOM：带 BOM 的文档会让 xml.Decoder 在根元素之前遇到非法字符而报错。
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var info ComicInfo
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = comicInfoCharsetReader
	if err := dec.Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func MarshalComicInfo(info ComicInfo) ([]byte, error) {
	data, err := xml.MarshalIndent(info, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}

// GetTags 从 Genre 中提取标签（通常是逗号分隔）
func (c *ComicInfo) GetTags() []string {
	if c.Genre == "" {
		return nil
	}
	parts := strings.Split(c.Genre, ",")
	var tags []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

// AuthorRole 定义作者及其角色
type AuthorRole struct {
	Name string
	Role string
}

// GetAuthors 提取并拼接各个工种的参与者
func (c *ComicInfo) GetAuthors() []AuthorRole {
	var authors []AuthorRole

	addRole := func(names, role string) {
		if names == "" {
			return
		}
		parts := strings.Split(names, ",")
		for _, p := range parts {
			name := strings.TrimSpace(p)
			if name != "" {
				authors = append(authors, AuthorRole{Name: name, Role: role})
			}
		}
	}

	addRole(c.Writer, "writer")
	addRole(c.Penciller, "penciller")
	addRole(c.Letterer, "letterer")
	addRole(c.Translator, "translator")

	return authors
}
