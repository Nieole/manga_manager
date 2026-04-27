package api

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"

	"github.com/go-chi/chi/v5"
)

// ============================================
// [#3] OPDS 1.2 标准分发协议
// ============================================

// OPDS Atom Feed 结构定义
type OPDSFeed struct {
	XMLName xml.Name    `xml:"feed"`
	XMLNS   string      `xml:"xmlns,attr"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Author  *OPDSAuthor `xml:"author,omitempty"`
	Links   []OPDSLink  `xml:"link"`
	Entries []OPDSEntry `xml:"entry"`
}

type OPDSAuthor struct {
	Name string `xml:"name"`
}

type OPDSLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr,omitempty"`
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

// SetupOPDSRoutes 注册 OPDS 路由
func (c *Controller) SetupOPDSRoutes(r chi.Router) {
	r.Route("/opds/v1.2", func(r chi.Router) {
		r.Get("/", c.opdsRoot)
		r.Get("/continue", c.opdsContinueReading)
		r.Get("/opensearch.xml", c.opdsOpenSearch)
		r.Get("/search", c.opdsSearch)
		r.Get("/libraries", c.opdsLibraries)
		r.Get("/libraries/{libraryId}", c.opdsLibrarySeries)
		r.Get("/series/{seriesId}", c.opdsSeriesBooks)
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
				Title:   "继续阅读",
				ID:      "urn:manga-manager:opds:continue",
				Updated: now,
				Content: "从最近阅读的卷册继续",
				Links: []OPDSLink{
					{Href: "/opds/v1.2/continue", Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
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

// opdsSearch 搜索系列，供 OPDS 客户端通过 OpenSearch 调用。
func (c *Controller) opdsSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", 30, 100)
	now := time.Now().Format(time.RFC3339)

	total := 0
	entries := []OPDSEntry{}
	if query != "" {
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
					Type: "image/jpeg",
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

	seriesList, err := c.store.ListSeriesByLibrary(r.Context(), libID)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().Format(time.RFC3339)
	total := len(seriesList)
	page := opdsPositiveQueryInt(r, "page", 1, 0)
	limit := opdsPositiveQueryInt(r, "limit", 50, 200)
	start, end := opdsSliceBounds(total, page, limit)
	seriesList = seriesList[start:end]

	entries := make([]OPDSEntry, 0, len(seriesList))
	for _, s := range seriesList {
		title := s.Name
		if s.Title.Valid && s.Title.String != "" {
			title = s.Title.String
		}

		links := []OPDSLink{
			{Href: fmt.Sprintf("/opds/v1.2/series/%d", s.ID), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
		}
		if s.CoverPath.Valid && s.CoverPath.String != "" {
			links = append(links, OPDSLink{
				Rel:  "http://opds-spec.org/image/thumbnail",
				Href: fmt.Sprintf("/api/thumbnails/%s", s.CoverPath.String),
				Type: "image/jpeg",
			})
		}

		summary := ""
		if s.Summary.Valid {
			summary = s.Summary.String
		}

		entries = append(entries, OPDSEntry{
			Title:   title,
			ID:      fmt.Sprintf("urn:manga-manager:opds:series:%d", s.ID),
			Updated: s.UpdatedAt.Format(time.RFC3339),
			Content: summary,
			Links:   links,
		})
	}

	feed := OPDSFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   "系列列表",
		ID:      fmt.Sprintf("urn:manga-manager:opds:library:%d:series", libID),
		Updated: now,
		Links:   opdsPaginationLinks(fmt.Sprintf("/opds/v1.2/libraries/%d", libID), page, limit, total),
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

		links := []OPDSLink{
			{Rel: "http://opds-spec.org/acquisition", Href: fmt.Sprintf("/api/pages/%d/1", b.ID), Type: "image/jpeg"},
		}
		if b.CoverPath.Valid && b.CoverPath.String != "" {
			links = append(links, OPDSLink{
				Rel:  "http://opds-spec.org/image/thumbnail",
				Href: fmt.Sprintf("/api/thumbnails/%s", b.CoverPath.String),
				Type: "image/jpeg",
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
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   seriesTitle,
		ID:      fmt.Sprintf("urn:manga-manager:opds:series:%d:books", seriesID),
		Updated: now,
		Links:   opdsPaginationLinks(fmt.Sprintf("/opds/v1.2/series/%d", seriesID), page, limit, total),
		Entries: entries,
	}
	xmlResponse(w, feed)
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
		links := []OPDSLink{
			{Rel: "http://opds-spec.org/acquisition", Href: fmt.Sprintf("/api/pages/%d/1", item.BookID), Type: "image/jpeg"},
		}
		if item.CoverPath.Valid && item.CoverPath.String != "" {
			links = append(links, OPDSLink{
				Rel:  "http://opds-spec.org/image/thumbnail",
				Href: fmt.Sprintf("/api/thumbnails/%s", item.CoverPath.String),
				Type: "image/jpeg",
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
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   "继续阅读",
		ID:      "urn:manga-manager:opds:continue",
		Updated: now,
		Links: []OPDSLink{
			{Rel: "self", Href: fmt.Sprintf("/opds/v1.2/continue?limit=%d", limit), Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
			{Rel: "start", Href: "/opds/v1.2/", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
		},
		Entries: entries,
	}
	xmlResponse(w, feed)
}
