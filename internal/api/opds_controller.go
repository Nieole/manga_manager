// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
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
// 此前 OPDS 链接一律硬编码 image/jpeg，与实际字节不符。
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
	})
}

func xmlResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/atom+xml;charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(data)
}

func openSearchResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/opensearchdescription+xml;charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(data)
}

// opdsRoot 根目录导航
func (c *Controller) opdsRoot(w http.ResponseWriter, r *http.Request) {
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
				Title:   "所有资源库",
				ID:      "urn:manga-manager:opds:libraries",
				Updated: now,
				Content: "浏览所有资源库中的漫画系列",
				Links: []OPDSLink{
					{Href: "/opds/v1.2/libraries", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
				},
			},
			{
				Title:   "最近添加",
				ID:      "urn:manga-manager:opds:recent",
				Updated: now,
				Content: "按入库时间浏览最近添加的系列",
				Links: []OPDSLink{
					{Href: "/opds/v1.2/recent", Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
				},
			},
			{
				Title:   "继续阅读",
				ID:      "urn:manga-manager:opds:continue",
				Updated: now,
				Content: "从最近阅读的卷册继续",
				Links: []OPDSLink{
					{Href: "/opds/v1.2/continue", Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
				},
			},
			{
				Title:   "合集",
				ID:      "urn:manga-manager:opds:collections",
				Updated: now,
				Content: "浏览手工合集、AI 分组合集、智能快照和动态规则合集",
				Links: []OPDSLink{
					{Href: "/opds/v1.2/collections", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
				},
			},
			{
				Title:   "阅读清单",
				ID:      "urn:manga-manager:opds:reading-lists",
				Updated: now,
				Content: "浏览有序阅读清单",
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
	if max > 0 && value > max {
		value = max
	}
	return value
}

func opdsSliceBounds(total, page, limit int) (int, int) {
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
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

func opdsBookAcquisitionLinks(bookID, pageCount int64, lastReadPage sql.NullInt64) []OPDSLink {
	links := []OPDSLink{
		{Rel: "http://opds-spec.org/acquisition", Href: fmt.Sprintf("/api/pages/%d/1", bookID), Type: "image/jpeg"},
	}
	if pageCount <= 0 {
		return links
	}
	stream := OPDSLink{
		Rel:      opdsPSEStreamRel,
		Href:     fmt.Sprintf("/opds/v1.2/books/%d/pages/{pageNumber}?format=jpeg&w={maxWidth}", bookID),
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
			Href: fmt.Sprintf("/api/thumbnails/%s", item.CoverPath),
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
			Href: fmt.Sprintf("/api/thumbnails/%s", row.CoverPath.String),
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
			Href: fmt.Sprintf("/api/thumbnails/%s", row.CoverPath),
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
			Href: fmt.Sprintf("/api/thumbnails/%s", row.CoverPath),
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
			Href: fmt.Sprintf("/api/thumbnails/%s", row.CoverPath),
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
		Offset:    int64((page - 1) * limit),
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
		Title:   "最近添加",
		ID:      "urn:manga-manager:opds:recent",
		Updated: now,
		Links:   opdsPaginationLinks(base, page, limit, int(total)),
		Entries: entries,
	}
	xmlResponse(w, feed)
}

func (c *Controller) opdsCollections(w http.ResponseWriter, r *http.Request) {
	views, err := c.loadCollectionViews(r.Context())
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
		contentParts := []string{view.SourceType, fmt.Sprintf("%d 个系列", view.SeriesCount)}
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
		Title:   "合集",
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
			Content: fmt.Sprintf("%d 个系列", list.ItemCount),
			Links: []OPDSLink{
				{Href: fmt.Sprintf("/opds/v1.2/reading-lists/%d", list.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
			},
		})
	}
	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   "阅读清单",
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
		Offset:        int64((page - 1) * limit),
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
	view, rows, total, err := c.loadStaticCollectionSeries(r.Context(), collectionID, limit, (page-1)*limit)
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
	rows, total, err := c.loadSmartCollectionSeries(r.Context(), filter, limit, (page-1)*limit)
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
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", 30, 100)
	now := time.Now().Format(time.RFC3339)

	total := 0
	entries := []OPDSEntry{}
	if query != "" {
		if rows, searchTotal, usedEngine, err := c.searchProtocolSeries(r.Context(), query, page, limit); usedEngine {
			if err != nil {
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
			total = searchTotal
			for _, row := range rows {
				entries = append(entries, opdsSeriesEntryFromProtocolRow(row))
			}
		} else {
			searchTotal, err := c.store.CountOPDSSeriesSearch(r.Context(), query)
			if err != nil {
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
			total = int(searchTotal)

			rows, err := c.store.SearchOPDSSeries(r.Context(), database.SearchOPDSSeriesParams{
				Query:  query,
				Limit:  int64(limit),
				Offset: int64((page - 1) * limit),
			})
			if err != nil {
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}

			for _, row := range rows {
				title := row.Title
				if title == "" {
					title = row.Name
				}
				links := []OPDSLink{
					{Href: fmt.Sprintf("/opds/v1.2/series/%d", row.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
				}
				if row.CoverPath != "" {
					links = append(links, OPDSLink{
						Rel:  "http://opds-spec.org/image/thumbnail",
						Href: fmt.Sprintf("/api/thumbnails/%s", row.CoverPath),
						Type: opdsThumbnailMIME(row.CoverPath),
					})
				}
				entries = append(entries, OPDSEntry{
					Title:   title,
					ID:      fmt.Sprintf("urn:manga-manager:opds:search:series:%d", row.ID),
					Updated: row.UpdatedAt.Format(time.RFC3339),
					Content: row.Summary,
					Links:   links,
				})
			}
		}
	}

	base := "/opds/v1.2/search?q=" + url.QueryEscape(query)
	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   fmt.Sprintf("搜索：%s", query),
		ID:      "urn:manga-manager:opds:search:" + query,
		Updated: now,
		Links:   opdsPaginationLinks(base, page, limit, total),
		Entries: entries,
	}
	xmlResponse(w, feed)
}

// opdsLibraries 资源库列表
func (c *Controller) opdsLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := c.store.ListLibraries(r.Context())
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().Format(time.RFC3339)
	entries := make([]OPDSEntry, 0, len(libs))
	for _, lib := range libs {
		entries = append(entries, OPDSEntry{
			Title:   lib.Name,
			ID:      fmt.Sprintf("urn:manga-manager:opds:library:%d", lib.ID),
			Updated: now,
			Content: lib.Path,
			Links: []OPDSLink{
				{Href: fmt.Sprintf("/opds/v1.2/libraries/%d", lib.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
			},
		})
	}

	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   "资源库",
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
	libID, err := strconv.ParseInt(chi.URLParam(r, "libraryId"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid library ID", http.StatusBadRequest)
		return
	}

	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", 50, 200)

	// 在数据库层分页（LIMIT/OFFSET + COUNT），不再一次性加载整库系列再内存切片。
	total, err := c.store.CountSeriesByLibrary(r.Context(), libID)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	rows, err := c.store.ListOPDSLibrarySeriesPaged(r.Context(), database.ListOPDSLibrarySeriesPagedParams{
		LibraryID: libID,
		Limit:     int64(limit),
		Offset:    int64((page - 1) * limit),
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
				Href: fmt.Sprintf("/api/thumbnails/%s", s.CoverPath),
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
		Title:   "系列列表",
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

	series, _ := c.store.GetSeries(r.Context(), seriesID)
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

		links := opdsBookAcquisitionLinks(b.ID, b.PageCount, b.LastReadPage)
		if b.CoverPath.Valid && b.CoverPath.String != "" {
			links = append(links, OPDSLink{
				Rel:  "http://opds-spec.org/image/thumbnail",
				Href: fmt.Sprintf("/api/thumbnails/%s", b.CoverPath.String),
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
	c.servePageImageByNumber(w, r, bookID, pageNumber+1)
}

func (c *Controller) opdsContinueReading(w http.ResponseWriter, r *http.Request) {
	limit := int64(opdsPositiveQueryInt(r, "limit", 30, 100))
	items, err := c.store.GetRecentReadAll(r.Context(), limit)
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
			content = fmt.Sprintf("%s · 第 %d / %d 页", item.SeriesName, item.LastReadPage.Int64, item.PageCount)
		}
		links := opdsBookAcquisitionLinks(item.BookID, item.PageCount, item.LastReadPage)
		if item.CoverPath != "" {
			links = append(links, OPDSLink{
				Rel:  "http://opds-spec.org/image/thumbnail",
				Href: fmt.Sprintf("/api/thumbnails/%s", item.CoverPath),
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
		Title:    "继续阅读",
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
