package api

import (
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/booksort"
	"manga-manager/internal/database"

	"github.com/go-chi/chi/v5"
)

// opdsThumbnailMIME 按缩略图文件扩展名推导 MIME。缩略图默认生成 webp（可配置 avif/jpg），
// 返回的 MIME 必须与实际字节格式一致，不能对所有格式都报 image/jpeg。
func opdsThumbnailMIME(coverPath string) string {
	switch strings.ToLower(filepath.Ext(coverPath)) {
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}

// ============================================
// [#3] OPDS 1.2 标准分发协议
// ============================================

// OPDSFeed OPDS Atom Feed 结构定义
type OPDSFeed struct {
	XMLName  xml.Name    `xml:"feed"`
	XMLNS    string      `xml:"xmlns,attr"`
	XMLNSPSE string      `xml:"xmlns:pse,attr,omitempty"`
	Title    string      `xml:"title"`
	ID       string      `xml:"id"`
	Updated  string      `xml:"updated"`
	Author   *OPDSAuthor `xml:"author,omitempty"`
	Links    []OPDSLink  `xml:"link"`
	Entries  []OPDSEntry `xml:"entry"`
}

type OPDSAuthor struct {
	Name string `xml:"name"`
}

type OPDSLink struct {
	Rel         string `xml:"rel,attr,omitempty"`
	Href        string `xml:"href,attr"`
	Type        string `xml:"type,attr,omitempty"`
	Count       int64  `xml:"count,attr,omitempty"`
	PSECount    int64  `xml:"-"`
	PSELastRead int64  `xml:"-"`
}

type OPDSEntry struct {
	Title   string     `xml:"title"`
	ID      string     `xml:"id"`
	Updated string     `xml:"updated"`
	Content string     `xml:"content,omitempty"`
	Links   []OPDSLink `xml:"link"`
}

type OpenSearchDescription struct {
	XMLName        xml.Name        `xml:"OpenSearchDescription"`
	XMLNS          string          `xml:"xmlns,attr"`
	ShortName      string          `xml:"ShortName"`
	Description    string          `xml:"Description"`
	InputEncoding  string          `xml:"InputEncoding"`
	OutputEncoding string          `xml:"OutputEncoding"`
	URLs           []OpenSearchURL `xml:"Url"`
}

type OpenSearchURL struct {
	Type     string `xml:"type,attr"`
	Template string `xml:"template,attr"`
}

func (link OPDSLink) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	start.Name.Local = "link"
	if link.Rel != "" {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "rel"}, Value: link.Rel})
	}
	start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "href"}, Value: link.Href})
	if link.Type != "" {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "type"}, Value: link.Type})
	}
	if link.Count > 0 {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "count"}, Value: strconv.FormatInt(link.Count, 10)})
	}
	if link.PSECount > 0 {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "pse:count"}, Value: strconv.FormatInt(link.PSECount, 10)})
	}
	if link.PSELastRead > 0 {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "pse:lastRead"}, Value: strconv.FormatInt(link.PSELastRead, 10)})
	}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	return e.EncodeToken(start.End())
}

const (
	opdsPSEXMLNS     = "http://vaemendis.net/opds-pse/ns"
	opdsPSEStreamRel = "http://vaemendis.net/opds-pse/stream"
)

// SetupOPDSRoutes 注册 OPDS 路由
func (c *Controller) SetupOPDSRoutes(r chi.Router) {
	r.Route("/opds/v1.2", func(r chi.Router) {
		r.Use(c.requireProtocolEnabled("opds"))
		// 阅读协议按站点用户 HTTP Basic 鉴权（多用户）；进度显示随之按当前用户。
		r.Use(c.requireBasicAuth)
		r.Get("/", c.opdsRoot)
		r.Get("/continue", c.opdsContinueReading)
		r.Get("/recent", c.opdsRecentAdded)
		r.Get("/collections", c.opdsCollections)
		r.Get("/collections/{collectionId}", c.opdsStaticCollectionSeries)
		r.Get("/reading-lists", c.opdsReadingLists)
		r.Get("/reading-lists/{listId}", c.opdsReadingListSeries)
		r.Get("/smart-collections/{filterId}", c.opdsSmartCollectionSeries)
		r.Get("/opensearch.xml", c.opdsOpenSearch)
		r.Get("/search", c.opdsSearch)
		r.Get("/libraries", c.opdsLibraries)
		r.Get("/libraries/{libraryId}", c.opdsLibrarySeries)
		r.Get("/series/{seriesId}", c.opdsSeriesBooks)
		r.Get("/books/{bookId}/pages/{pageNumber}", c.opdsStreamPageImage)
		// 协议内的资源路由。feed 里的整卷下载/封面/缩略图链接必须指向这里，不能指向 /api：
		// /api 组由 authGate 守卫、只认 session cookie，而 OPDS 客户端带的是 HTTP Basic 凭据，
		// 请求过去必然 401——整卷下载与封面在真实阅读器上完全不可用。
		// 这几条复用与 /api 相同的 handler，区别只在于走的是本组的 requireBasicAuth。
		r.Get("/books/{bookId}/file", c.serveBookFile)
		r.Get("/books/{bookId}/cover", c.serveCoverImage)
		r.Get("/thumbnails/*", c.serveThumbnailImage)
	})
}

// opdsResourceBase 是 OPDS feed 内所有资源链接的前缀。
const opdsResourceBase = "/opds/v1.2"

func xmlResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/atom+xml;charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(data)
}

func openSearchResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/opensearchdescription+xml;charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(data)
}

// opdsRoot 根目录导航
// opdsMessages 按 locale 提供 OPDS feed 的用户可见文案。OPDS 输出是给电子阅读器的 XML，
// 前端无法翻译，故按 Accept-Language 在后端选择中/英文案。含 %d/%s 占位的格式串因 go vet 的
// 非常量格式检查不入表，改由各 handler 内联的 locale 分支处理。
var opdsMessages = map[string]map[string]string{
	"zh-CN": {
		"catalog.libraries.title":      "所有资源库",
		"catalog.libraries.content":    "浏览所有资源库中的漫画系列",
		"catalog.recent.title":         "最近添加",
		"catalog.recent.content":       "按入库时间浏览最近添加的系列",
		"catalog.continue.title":       "继续阅读",
		"catalog.continue.content":     "从最近阅读的卷册继续",
		"catalog.collections.title":    "合集",
		"catalog.collections.content":  "浏览手工合集、AI 分组合集、智能快照和动态规则合集",
		"catalog.readingLists.title":   "阅读清单",
		"catalog.readingLists.content": "浏览有序阅读清单",
		"feed.libraries.title":         "资源库",
		"feed.seriesList.title":        "系列列表",
	},
	"en-US": {
		"catalog.libraries.title":      "All Libraries",
		"catalog.libraries.content":    "Browse manga series across all libraries",
		"catalog.recent.title":         "Recently Added",
		"catalog.recent.content":       "Browse recently added series by import time",
		"catalog.continue.title":       "Continue Reading",
		"catalog.continue.content":     "Resume from your recently read volumes",
		"catalog.collections.title":    "Collections",
		"catalog.collections.content":  "Browse manual, AI-grouped, smart-snapshot and dynamic-rule collections",
		"catalog.readingLists.title":   "Reading Lists",
		"catalog.readingLists.content": "Browse ordered reading lists",
		"feed.libraries.title":         "Libraries",
		"feed.seriesList.title":        "Series",
	},
}

// opdsText 返回给定 locale 的 OPDS 文案；未知 locale/key 回退到 zh-CN，再回退到 key 本身。
func opdsText(locale, key string) string {
	if m, ok := opdsMessages[locale]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if s, ok := opdsMessages["zh-CN"][key]; ok {
		return s
	}
	return key
}

// 以下三个 helper 用常量格式串按 locale 生成带占位的 OPDS 文案，满足 go vet 的常量格式要求
// （若把格式串放进 opdsMessages 表再传给 fmt.Sprintf，vet 会报 non-constant format string）。
func opdsSearchTitle(locale, query string) string {
	if locale == "en-US" {
		return fmt.Sprintf("Search: %s", query)
	}
	return fmt.Sprintf("搜索：%s", query)
}

func opdsSeriesCountText(locale string, n int64) string {
	if locale == "en-US" {
		return fmt.Sprintf("%d series", n)
	}
	return fmt.Sprintf("%d 个系列", n)
}

func opdsContinueProgress(locale, seriesName string, page, total int64) string {
	if locale == "en-US" {
		return fmt.Sprintf("%s · Page %d / %d", seriesName, page, total)
	}
	return fmt.Sprintf("%s · 第 %d / %d 页", seriesName, page, total)
}

func (c *Controller) opdsRoot(w http.ResponseWriter, r *http.Request) {
	locale := requestLocale(r)
	now := time.Now().Format(time.RFC3339)
	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   "Manga Manager OPDS Catalog",
		ID:      "urn:manga-manager:opds:root",
		Updated: now,
		Author:  &OPDSAuthor{Name: "Manga Manager"},
		Links: []OPDSLink{
			{Rel: "self", Href: "/opds/v1.2/", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
			{Rel: "start", Href: "/opds/v1.2/", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
			{Rel: "search", Href: "/opds/v1.2/opensearch.xml", Type: "application/opensearchdescription+xml"},
		},
		Entries: []OPDSEntry{
			{
				Title:   opdsText(locale, "catalog.libraries.title"),
				ID:      "urn:manga-manager:opds:libraries",
				Updated: now,
				Content: opdsText(locale, "catalog.libraries.content"),
				Links: []OPDSLink{
					{Href: "/opds/v1.2/libraries", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
				},
			},
			{
				Title:   opdsText(locale, "catalog.recent.title"),
				ID:      "urn:manga-manager:opds:recent",
				Updated: now,
				Content: opdsText(locale, "catalog.recent.content"),
				Links: []OPDSLink{
					{Href: "/opds/v1.2/recent", Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
				},
			},
			{
				Title:   opdsText(locale, "catalog.continue.title"),
				ID:      "urn:manga-manager:opds:continue",
				Updated: now,
				Content: opdsText(locale, "catalog.continue.content"),
				Links: []OPDSLink{
					{Href: "/opds/v1.2/continue", Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
				},
			},
			{
				Title:   opdsText(locale, "catalog.collections.title"),
				ID:      "urn:manga-manager:opds:collections",
				Updated: now,
				Content: opdsText(locale, "catalog.collections.content"),
				Links: []OPDSLink{
					{Href: "/opds/v1.2/collections", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
				},
			},
			{
				Title:   opdsText(locale, "catalog.readingLists.title"),
				ID:      "urn:manga-manager:opds:reading-lists",
				Updated: now,
				Content: opdsText(locale, "catalog.readingLists.content"),
				Links: []OPDSLink{
					{Href: "/opds/v1.2/reading-lists", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
				},
			},
		},
	}
	xmlResponse(w, feed)
}

func (c *Controller) opdsOpenSearch(w http.ResponseWriter, r *http.Request) {
	description := OpenSearchDescription{
		XMLNS:          "http://a9.com/-/spec/opensearch/1.1/",
		ShortName:      "Manga Manager",
		Description:    "Search Manga Manager series",
		InputEncoding:  "UTF-8",
		OutputEncoding: "UTF-8",
		URLs: []OpenSearchURL{
			{
				Type:     "application/atom+xml;profile=opds-catalog;kind=acquisition",
				Template: "/opds/v1.2/search?q={searchTerms}&page={startPage?}&limit={count?}",
			},
		},
	}
	openSearchResponse(w, description)
}

func opdsPositiveQueryInt(r *http.Request, key string, fallback, max int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || value <= 0 {
		value = fallback
	}
	// max<=0 时钳到 maxPageNumber，不当作「不设上限」：page 无上界会让 (page-1)*limit
	// 溢出 int 变成负数，opdsSliceBounds 拿负 start 去切片直接 panic。
	if max <= 0 {
		max = maxPageNumber
	}
	if value > max {
		value = max
	}
	return value
}

// opdsSliceBounds 计算内存分页的切片区间，对任何入参都返回 0 <= start <= end <= total。
// 防御性钳制而非信任调用方：这里的返回值直接用于切片下标，算错一次就是 panic。
func opdsSliceBounds(total, page, limit int) (int, int) {
	if total < 0 {
		total = 0
	}
	start64 := pageOffset(page, limit)
	if start64 > int64(total) {
		start64 = int64(total)
	}
	start := int(start64)
	end := start
	if limit > 0 {
		end = start + limit
	}
	if end > total {
		end = total
	}
	if end < start {
		end = start
	}
	return start, end
}

func opdsPageHref(base string, page, limit int) string {
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%spage=%d&limit=%d", base, separator, page, limit)
}

func opdsPaginationLinks(base string, page, limit, total int) []OPDSLink {
	links := []OPDSLink{
		{Rel: "self", Href: opdsPageHref(base, page, limit), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
		{Rel: "start", Href: "/opds/v1.2/", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
	}
	if page > 1 {
		links = append(links, OPDSLink{Rel: "previous", Href: opdsPageHref(base, page-1, limit), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"})
	}
	if page*limit < total {
		links = append(links, OPDSLink{Rel: "next", Href: opdsPageHref(base, page+1, limit), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"})
	}
	return links
}

// OPDS 页图路由 `/books/{bookId}/pages/{pageNumber}` 的页码是 OPDS-PSE 的 0 基下标，而服务端取页
// （servePageImageByNumber）是 1 基。两套口径只在这三个符号里换算：feed 写死的页链接经
// opdsPageImageHref，PSE 客户端自填页码的模板经 opdsPageImageHrefTemplate，收到的请求经
// opdsImagePageNumber。别处不得再对这个路由的页码写 ±1，也不得直接拼页码字面量。
const opdsFirstPageNumber = 0

func opdsPageImageHref(bookID, opdsPageNumber int64) string {
	return fmt.Sprintf("%s/books/%d/pages/%d", opdsResourceBase, bookID, opdsPageNumber)
}

func opdsPageImageHrefTemplate(bookID int64) string {
	return fmt.Sprintf("%s/books/%d/pages/{pageNumber}?format=jpeg&w={maxWidth}", opdsResourceBase, bookID)
}

func opdsImagePageNumber(opdsPageNumber int64) int64 {
	return opdsPageNumber + 1
}

func opdsBookAcquisitionLinks(bookID, pageCount int64, lastReadPage sql.NullInt64, bookPath string) []OPDSLink {
	links := []OPDSLink{
		// 整卷下载：非 PSE 的桌面/传统 OPDS 客户端据此拉取原始 CBZ/CBR/PDF 整包；type 反映真实归档
		// MIME（下载路由本身再以权威 Content-Type 下发）。放在首位，令整卷下载成为主获取项。
		{Rel: "http://opds-spec.org/acquisition", Href: fmt.Sprintf(opdsResourceBase+"/books/%d/file", bookID), Type: bookDownloadContentType(bookPath)},
		// 首页 JPEG：作为封面/预览补充，兼容只取第一页的旧客户端。页号走本路由的 0 基口径。
		{Rel: "http://opds-spec.org/acquisition", Href: opdsPageImageHref(bookID, opdsFirstPageNumber), Type: "image/jpeg"},
	}
	if pageCount <= 0 {
		return links
	}
	stream := OPDSLink{
		Rel:      opdsPSEStreamRel,
		Href:     opdsPageImageHrefTemplate(bookID),
		Type:     "image/jpeg",
		Count:    pageCount,
		PSECount: pageCount,
	}
	if lastReadPage.Valid && lastReadPage.Int64 > 0 {
		stream.PSELastRead = lastReadPage.Int64
		if stream.PSELastRead > pageCount {
			stream.PSELastRead = pageCount
		}
	}
	return append(links, stream)
}

func opdsSeriesEntryFromListItem(item collectionSeriesListItem) OPDSEntry {
	title := firstNonEmpty(item.Title, item.Name)
	links := []OPDSLink{
		{Href: fmt.Sprintf("/opds/v1.2/series/%d", item.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
	}
	if item.CoverPath != "" {
		links = append(links, OPDSLink{
			Rel:  "http://opds-spec.org/image/thumbnail",
			Href: fmt.Sprintf(opdsResourceBase+"/thumbnails/%s", item.CoverPath),
			Type: opdsThumbnailMIME(item.CoverPath),
		})
	}
	return OPDSEntry{
		Title:   title,
		ID:      fmt.Sprintf("urn:manga-manager:opds:series:%d", item.ID),
		Updated: item.UpdatedAt.Format(time.RFC3339),
		Content: item.Summary,
		Links:   links,
	}
}

func opdsSeriesEntryFromSearchRow(row database.SearchSeriesPagedRow) OPDSEntry {
	title := row.Name
	if row.Title.Valid && row.Title.String != "" {
		title = row.Title.String
	}
	summary := ""
	if row.Summary.Valid {
		summary = row.Summary.String
	}
	links := []OPDSLink{
		{Href: fmt.Sprintf("/opds/v1.2/series/%d", row.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
	}
	if row.CoverPath.Valid && row.CoverPath.String != "" {
		links = append(links, OPDSLink{
			Rel:  "http://opds-spec.org/image/thumbnail",
			Href: fmt.Sprintf(opdsResourceBase+"/thumbnails/%s", row.CoverPath.String),
			Type: opdsThumbnailMIME(row.CoverPath.String),
		})
	}
	return OPDSEntry{
		Title:   title,
		ID:      fmt.Sprintf("urn:manga-manager:opds:series:%d", row.ID),
		Updated: row.UpdatedAt.Format(time.RFC3339),
		Content: summary,
		Links:   links,
	}
}

func opdsSeriesEntryFromRecentAddedRow(row database.ListRecentAddedSeriesRow) OPDSEntry {
	title := firstNonEmpty(row.Title, row.Name)
	links := []OPDSLink{
		{Href: fmt.Sprintf("/opds/v1.2/series/%d", row.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
	}
	if row.CoverPath != "" {
		links = append(links, OPDSLink{
			Rel:  "http://opds-spec.org/image/thumbnail",
			Href: fmt.Sprintf(opdsResourceBase+"/thumbnails/%s", row.CoverPath),
			Type: opdsThumbnailMIME(row.CoverPath),
		})
	}
	return OPDSEntry{
		Title:   title,
		ID:      fmt.Sprintf("urn:manga-manager:opds:series:%d", row.ID),
		Updated: row.UpdatedAt.Format(time.RFC3339),
		Content: row.Summary,
		Links:   links,
	}
}

func opdsSeriesEntryFromProtocolRow(row database.ProtocolSeriesRow) OPDSEntry {
	title := firstNonEmpty(row.Title, row.Name)
	links := []OPDSLink{
		{Href: fmt.Sprintf("/opds/v1.2/series/%d", row.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
	}
	if row.CoverPath != "" {
		links = append(links, OPDSLink{
			Rel:  "http://opds-spec.org/image/thumbnail",
			Href: fmt.Sprintf(opdsResourceBase+"/thumbnails/%s", row.CoverPath),
			Type: opdsThumbnailMIME(row.CoverPath),
		})
	}
	return OPDSEntry{
		Title:   title,
		ID:      fmt.Sprintf("urn:manga-manager:opds:search:series:%d", row.ID),
		Updated: row.UpdatedAt.Format(time.RFC3339),
		Content: row.Summary,
		Links:   links,
	}
}

func opdsSeriesEntryFromReadingListRow(row database.ListReadingListSeriesPageRow) OPDSEntry {
	title := firstNonEmpty(row.Title, row.Name)
	content := row.Summary
	if row.Note != "" {
		content = firstNonEmpty(content, row.Note)
		if row.Summary != "" {
			content = row.Note + " · " + row.Summary
		}
	}
	links := []OPDSLink{
		{Href: fmt.Sprintf("/opds/v1.2/series/%d", row.SeriesID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
	}
	if row.CoverPath != "" {
		links = append(links, OPDSLink{
			Rel:  "http://opds-spec.org/image/thumbnail",
			Href: fmt.Sprintf(opdsResourceBase+"/thumbnails/%s", row.CoverPath),
			Type: opdsThumbnailMIME(row.CoverPath),
		})
	}
	return OPDSEntry{
		Title:   title,
		ID:      fmt.Sprintf("urn:manga-manager:opds:reading-list:%d:series:%d", row.ReadingListID, row.SeriesID),
		Updated: row.UpdatedAt.Format(time.RFC3339),
		Content: content,
		Links:   links,
	}
}

func (c *Controller) opdsRecentAdded(w http.ResponseWriter, r *http.Request) {
	locale := requestLocale(r)
	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", 30, 100)
	libraryID := int64(opdsPositiveQueryInt(r, "libraryId", 0, 0))

	total, err := c.store.CountRecentAddedSeries(r.Context(), libraryID)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	rows, err := c.store.ListRecentAddedSeries(r.Context(), database.ListRecentAddedSeriesParams{
		LibraryID: libraryID,
		Limit:     int64(limit),
		Offset:    pageOffset(page, limit),
	})
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	entries := make([]OPDSEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, opdsSeriesEntryFromRecentAddedRow(row))
	}
	now := time.Now().Format(time.RFC3339)
	base := "/opds/v1.2/recent"
	if libraryID > 0 {
		base = fmt.Sprintf("%s?libraryId=%d", base, libraryID)
	}
	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   opdsText(locale, "catalog.recent.title"),
		ID:      "urn:manga-manager:opds:recent",
		Updated: now,
		Links:   opdsPaginationLinks(base, page, limit, int(total)),
		Entries: entries,
	}
	xmlResponse(w, feed)
}

func (c *Controller) opdsCollections(w http.ResponseWriter, r *http.Request) {
	locale := requestLocale(r)
	views, err := c.loadCollectionViews(r.Context(), c.currentUserID(r))
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().Format(time.RFC3339)
	entries := make([]OPDSEntry, 0, len(views))
	for _, view := range views {
		href := fmt.Sprintf("/opds/v1.2/collections/%d", view.NumericID)
		urnKind := "collection"
		if view.Kind == "smart" {
			href = fmt.Sprintf("/opds/v1.2/smart-collections/%d", view.NumericID)
			urnKind = "smart-collection"
		}
		contentParts := []string{view.SourceType, opdsSeriesCountText(locale, int64(view.SeriesCount))}
		if view.LibraryName != "" {
			contentParts = append(contentParts, view.LibraryName)
		}
		if view.Description != "" {
			contentParts = append(contentParts, view.Description)
		}
		entries = append(entries, OPDSEntry{
			Title:   view.Name,
			ID:      fmt.Sprintf("urn:manga-manager:opds:%s:%d", urnKind, view.NumericID),
			Updated: view.UpdatedAt.Format(time.RFC3339),
			Content: strings.Join(contentParts, " · "),
			Links: []OPDSLink{
				{Href: href, Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
			},
		})
	}

	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   opdsText(locale, "catalog.collections.title"),
		ID:      "urn:manga-manager:opds:collections",
		Updated: now,
		Links: []OPDSLink{
			{Rel: "self", Href: "/opds/v1.2/collections", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
			{Rel: "start", Href: "/opds/v1.2/", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
		},
		Entries: entries,
	}
	xmlResponse(w, feed)
}

func (c *Controller) opdsReadingLists(w http.ResponseWriter, r *http.Request) {
	locale := requestLocale(r)
	lists, err := c.store.ListReadingLists(r.Context())
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().Format(time.RFC3339)
	entries := make([]OPDSEntry, 0, len(lists))
	for _, list := range lists {
		entries = append(entries, OPDSEntry{
			Title:   list.Name,
			ID:      fmt.Sprintf("urn:manga-manager:opds:reading-list:%d", list.ID),
			Updated: list.UpdatedAt.Format(time.RFC3339),
			Content: opdsSeriesCountText(locale, list.ItemCount),
			Links: []OPDSLink{
				{Href: fmt.Sprintf("/opds/v1.2/reading-lists/%d", list.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
			},
		})
	}
	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   opdsText(locale, "catalog.readingLists.title"),
		ID:      "urn:manga-manager:opds:reading-lists",
		Updated: now,
		Links: []OPDSLink{
			{Rel: "self", Href: "/opds/v1.2/reading-lists", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
			{Rel: "start", Href: "/opds/v1.2/", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
		},
		Entries: entries,
	}
	xmlResponse(w, feed)
}

func (c *Controller) opdsReadingListSeries(w http.ResponseWriter, r *http.Request) {
	listID, err := strconv.ParseInt(chi.URLParam(r, "listId"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid reading list ID", http.StatusBadRequest)
		return
	}
	list, err := c.store.GetReadingList(r.Context(), listID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Reading list not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", 50, 200)
	total, err := c.store.CountReadingListSeries(r.Context(), listID)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	rows, err := c.store.ListReadingListSeriesPage(r.Context(), database.ListReadingListSeriesPageParams{
		ReadingListID: listID,
		Limit:         int64(limit),
		Offset:        pageOffset(page, limit),
	})
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	entries := make([]OPDSEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, opdsSeriesEntryFromReadingListRow(row))
	}
	now := time.Now().Format(time.RFC3339)
	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   list.Name,
		ID:      fmt.Sprintf("urn:manga-manager:opds:reading-list:%d:series", listID),
		Updated: now,
		Links:   opdsPaginationLinks(fmt.Sprintf("/opds/v1.2/reading-lists/%d", listID), page, limit, int(total)),
		Entries: entries,
	}
	xmlResponse(w, feed)
}

func (c *Controller) opdsStaticCollectionSeries(w http.ResponseWriter, r *http.Request) {
	collectionID, err := strconv.ParseInt(chi.URLParam(r, "collectionId"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid collection ID", http.StatusBadRequest)
		return
	}
	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", 50, 200)
	view, rows, total, err := c.loadStaticCollectionSeries(r.Context(), collectionID, limit, int(pageOffset(page, limit)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Collection not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	entries := make([]OPDSEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, opdsSeriesEntryFromListItem(row))
	}
	now := time.Now().Format(time.RFC3339)
	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   view.Name,
		ID:      fmt.Sprintf("urn:manga-manager:opds:collection:%d:series", collectionID),
		Updated: now,
		Links:   opdsPaginationLinks(fmt.Sprintf("/opds/v1.2/collections/%d", collectionID), page, limit, total),
		Entries: entries,
	}
	xmlResponse(w, feed)
}

func (c *Controller) opdsSmartCollectionSeries(w http.ResponseWriter, r *http.Request) {
	filterID, err := strconv.ParseInt(chi.URLParam(r, "filterId"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid smart collection ID", http.StatusBadRequest)
		return
	}
	filter, err := c.getSmartFilterByID(r, filterID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Smart collection not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", filter.PageSize, 200)
	rows, total, err := c.loadSmartCollectionSeries(r.Context(), filter, limit, int(pageOffset(page, limit)), c.currentUserID(r))
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	entries := make([]OPDSEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, opdsSeriesEntryFromSearchRow(row))
	}
	now := time.Now().Format(time.RFC3339)
	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   filter.Name,
		ID:      fmt.Sprintf("urn:manga-manager:opds:smart-collection:%d:series", filterID),
		Updated: now,
		Links:   opdsPaginationLinks(fmt.Sprintf("/opds/v1.2/smart-collections/%d", filterID), page, limit, total),
		Entries: entries,
	}
	xmlResponse(w, feed)
}

// opdsSearch 搜索系列，供 OPDS 客户端通过 OpenSearch 调用。
func (c *Controller) opdsSearch(w http.ResponseWriter, r *http.Request) {
	locale := requestLocale(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", 30, 100)
	now := time.Now().Format(time.RFC3339)

	total := 0
	entries := []OPDSEntry{}
	if query != "" {
		// query 已在上方 TrimSpace，故 searchProtocolSeries 的 usedEngine 恒为 true：搜索统一走
		// SearchProtocolSeries（>=3 rune 命中 series_search_fts、<3 rune CJK 回退子串），与 Web/Mihon
		// 一致，避免对系列表逐行 lower()+instr 的双重全表扫。
		rows, searchTotal, _, err := c.searchProtocolSeries(r.Context(), query, page, limit)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		total = searchTotal
		for _, row := range rows {
			entries = append(entries, opdsSeriesEntryFromProtocolRow(row))
		}
	}

	base := "/opds/v1.2/search?q=" + url.QueryEscape(query)
	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   opdsSearchTitle(locale, query),
		ID:      "urn:manga-manager:opds:search:" + query,
		Updated: now,
		Links:   opdsPaginationLinks(base, page, limit, total),
		Entries: entries,
	}
	xmlResponse(w, feed)
}

// opdsLibraries 资源库列表。
//
// 每个条目的 content 是给人看的描述，填系列数而不是 lib.Path：OPDS 只有阅读这一种用法，
// 任何站点账号都能打开这张列表，宿主机绝对路径对读者没有用处，却把服务器目录结构全交了出去。
// 系列数对管理员同样是这张列表上最有用的信息，因此两种角色不分叉。
func (c *Controller) opdsLibraries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := requestLocale(r)
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().Format(time.RFC3339)
	entries := make([]OPDSEntry, 0, len(libs))
	for _, lib := range libs {
		seriesCount, err := c.store.CountSeriesByLibrary(ctx, lib.ID)
		if err != nil {
			slog.Error("OPDS: count series for library failed", "library_id", lib.ID, "error", err)
		}
		entries = append(entries, OPDSEntry{
			Title:   lib.Name,
			ID:      fmt.Sprintf("urn:manga-manager:opds:library:%d", lib.ID),
			Updated: now,
			Content: opdsSeriesCountText(locale, seriesCount),
			Links: []OPDSLink{
				{Href: fmt.Sprintf("/opds/v1.2/libraries/%d", lib.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
			},
		})
	}

	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   opdsText(locale, "feed.libraries.title"),
		ID:      "urn:manga-manager:opds:libraries",
		Updated: now,
		Links: []OPDSLink{
			{Rel: "self", Href: "/opds/v1.2/libraries", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
			{Rel: "start", Href: "/opds/v1.2/", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
		},
		Entries: entries,
	}
	xmlResponse(w, feed)
}

// opdsLibrarySeries 某资源库下的系列列表
func (c *Controller) opdsLibrarySeries(w http.ResponseWriter, r *http.Request) {
	locale := requestLocale(r)
	libID, err := strconv.ParseInt(chi.URLParam(r, "libraryId"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid library ID", http.StatusBadRequest)
		return
	}

	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", 50, 200)

	// 在数据库层分页（LIMIT/OFFSET + COUNT），避免整库系列一次性加载进内存再切片——
	// 大型库下内存与延迟开销会随库大小线性增长。
	total, err := c.store.CountSeriesByLibrary(r.Context(), libID)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	rows, err := c.store.ListOPDSLibrarySeriesPaged(r.Context(), database.ListOPDSLibrarySeriesPagedParams{
		LibraryID: libID,
		Limit:     int64(limit),
		Offset:    pageOffset(page, limit),
	})
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().Format(time.RFC3339)
	entries := make([]OPDSEntry, 0, len(rows))
	for _, s := range rows {
		title := s.Name
		if s.Title != "" {
			title = s.Title
		}

		links := []OPDSLink{
			{Href: fmt.Sprintf("/opds/v1.2/series/%d", s.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
		}
		if s.CoverPath != "" {
			links = append(links, OPDSLink{
				Rel:  "http://opds-spec.org/image/thumbnail",
				Href: fmt.Sprintf(opdsResourceBase+"/thumbnails/%s", s.CoverPath),
				Type: opdsThumbnailMIME(s.CoverPath),
			})
		}

		entries = append(entries, OPDSEntry{
			Title:   title,
			ID:      fmt.Sprintf("urn:manga-manager:opds:series:%d", s.ID),
			Updated: s.UpdatedAt.Format(time.RFC3339),
			Content: s.Summary,
			Links:   links,
		})
	}

	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   opdsText(locale, "feed.seriesList.title"),
		ID:      fmt.Sprintf("urn:manga-manager:opds:library:%d:series", libID),
		Updated: now,
		Links:   opdsPaginationLinks(fmt.Sprintf("/opds/v1.2/libraries/%d", libID), page, limit, int(total)),
		Entries: entries,
	}
	xmlResponse(w, feed)
}

// opdsSeriesBooks 某系列下的书籍列表
func (c *Controller) opdsSeriesBooks(w http.ResponseWriter, r *http.Request) {
	seriesID, err := strconv.ParseInt(chi.URLParam(r, "seriesId"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid series ID", http.StatusBadRequest)
		return
	}

	books, err := c.store.ListBooksBySeries(r.Context(), seriesID)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	// 与全站阅读顺序口径对齐：SQL 的 ORDER BY volume, sort_number, name 在 sort_number 缺失时
	// 会退化为按名称的字典序（第 10 话排到第 2 话之前）。用 booksort 规范化处理中文卷话等格式。
	slices.SortStableFunc(books, booksort.CompareBooks)
	c.overlayUserProgress(r.Context(), c.currentUserID(r), books)

	// 不存在的系列必须回 404 而不是 200 + 空 feed：吞掉这个错误会让阅读器把
	// 「系列已被删除」显示成「这个系列一本书都没有」，用户无从判断。
	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Series not found", http.StatusNotFound)
			return
		}
		slog.Error("opds: failed to load series", "series_id", seriesID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	seriesTitle := series.Name
	if series.Title.Valid && series.Title.String != "" {
		seriesTitle = series.Title.String
	}

	now := time.Now().Format(time.RFC3339)
	total := len(books)
	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", 50, 200)
	start, end := opdsSliceBounds(total, page, limit)
	books = books[start:end]

	entries := make([]OPDSEntry, 0, len(books))
	for _, b := range books {
		title := b.Name
		if b.Title.Valid && b.Title.String != "" {
			title = b.Title.String
		}

		links := opdsBookAcquisitionLinks(b.ID, b.PageCount, b.LastReadPage, b.Path)
		if b.CoverPath.Valid && b.CoverPath.String != "" {
			links = append(links, OPDSLink{
				Rel:  "http://opds-spec.org/image/thumbnail",
				Href: fmt.Sprintf(opdsResourceBase+"/thumbnails/%s", b.CoverPath.String),
				Type: opdsThumbnailMIME(b.CoverPath.String),
			})
		}

		entries = append(entries, OPDSEntry{
			Title:   title,
			ID:      fmt.Sprintf("urn:manga-manager:opds:book:%d", b.ID),
			Updated: b.UpdatedAt.Format(time.RFC3339),
			Links:   links,
		})
	}

	feed := OPDSFeed{
		XMLNS:    "http://www.w3.org/2005/Atom",
		XMLNSPSE: opdsPSEXMLNS,
		Title:    seriesTitle,
		ID:       fmt.Sprintf("urn:manga-manager:opds:series:%d:books", seriesID),
		Updated:  now,
		Links:    opdsPaginationLinks(fmt.Sprintf("/opds/v1.2/series/%d", seriesID), page, limit, total),
		Entries:  entries,
	}
	xmlResponse(w, feed)
}

func (c *Controller) opdsStreamPageImage(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	pageNumber, err := strconv.ParseInt(chi.URLParam(r, "pageNumber"), 10, 64)
	if err != nil || pageNumber < 0 {
		jsonError(w, http.StatusBadRequest, "Invalid page number")
		return
	}
	c.servePageImageByNumber(w, r, bookID, opdsImagePageNumber(pageNumber))
}

func (c *Controller) opdsContinueReading(w http.ResponseWriter, r *http.Request) {
	locale := requestLocale(r)
	limit := int64(opdsPositiveQueryInt(r, "limit", 30, 100))
	var (
		items []database.GetRecentReadAllRow
		err   error
	)
	if uid := c.currentUserID(r); uid > 0 {
		items, err = c.store.GetUserRecentReadAll(r.Context(), uid, limit)
	} else {
		items, err = c.store.GetRecentReadAll(r.Context(), limit)
	}
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().Format(time.RFC3339)
	entries := make([]OPDSEntry, 0, len(items))
	for _, item := range items {
		title := item.BookName
		if item.BookTitle.Valid && item.BookTitle.String != "" {
			title = item.BookTitle.String
		}
		content := item.SeriesName
		if item.LastReadPage.Valid && item.PageCount > 0 {
			content = opdsContinueProgress(locale, item.SeriesName, item.LastReadPage.Int64, item.PageCount)
		}
		// 必须传真实归档路径：整卷下载的 MIME 靠它推导，传空串会让 MIME 恒为
		// application/octet-stream，部分阅读器据此拒绝或按错误类型处理。
		links := opdsBookAcquisitionLinks(item.BookID, item.PageCount, item.LastReadPage, item.BookPath)
		if item.CoverPath != "" {
			links = append(links, OPDSLink{
				Rel:  "http://opds-spec.org/image/thumbnail",
				Href: fmt.Sprintf(opdsResourceBase+"/thumbnails/%s", item.CoverPath),
				Type: opdsThumbnailMIME(item.CoverPath),
			})
		}
		updated := now
		if item.LastReadAt.Valid {
			updated = item.LastReadAt.Time.Format(time.RFC3339)
		}
		entries = append(entries, OPDSEntry{
			Title:   title,
			ID:      fmt.Sprintf("urn:manga-manager:opds:continue:%d", item.BookID),
			Updated: updated,
			Content: content,
			Links:   links,
		})
	}

	feed := OPDSFeed{
		XMLNS:    "http://www.w3.org/2005/Atom",
		XMLNSPSE: opdsPSEXMLNS,
		Title:    opdsText(locale, "catalog.continue.title"),
		ID:       "urn:manga-manager:opds:continue",
		Updated:  now,
		Links: []OPDSLink{
			{Rel: "self", Href: fmt.Sprintf("/opds/v1.2/continue?limit=%d", limit), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
			{Rel: "start", Href: "/opds/v1.2/", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
		},
		Entries: entries,
	}
	xmlResponse(w, feed)
}
