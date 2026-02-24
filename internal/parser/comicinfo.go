package parser

import (
	"encoding/xml"
	"strings"
)

// ComicInfo 代表了基于 ComicRack 约定的标准内嵌元数据格式
type ComicInfo struct {
	XMLName         xml.Name `xml:"ComicInfo"`
	Title           string   `xml:"Title,omitempty"`
	Series          string   `xml:"Series,omitempty"`
	Summary         string   `xml:"Summary,omitempty"`
	Writer          string   `xml:"Writer,omitempty"`
	Penciller       string   `xml:"Penciller,omitempty"`
	Letterer        string   `xml:"Letterer,omitempty"`
	Translator      string   `xml:"Translator,omitempty"`
	Publisher       string   `xml:"Publisher,omitempty"`
	Genre           string   `xml:"Genre,omitempty"`
	Web             string   `xml:"Web,omitempty"`
	LanguageISO     string   `xml:"LanguageISO,omitempty"`
	Manga           string   `xml:"Manga,omitempty"`           // Yes, No, RightToLeft
	Rating          string   `xml:"Rating,omitempty"`          // Unknown, Rating Pending, Early Childhood, Everyone, Everyone 10+, Teen, Mature 17+, Adults Only 18+  (可选项，数字如 4.0 也常见)
	CommunityRating float32  `xml:"CommunityRating,omitempty"` // 真实分值 0.0 - 5.0
}

// ParseComicInfo 从 XML 字节流中反序列化 ComicInfo
func ParseComicInfo(data []byte) (*ComicInfo, error) {
	var info ComicInfo
	if err := xml.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
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
