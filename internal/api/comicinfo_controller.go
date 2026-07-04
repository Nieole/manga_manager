// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"

	"github.com/go-chi/chi/v5"
)

func (c *Controller) exportSeriesComicInfoArchive(w http.ResponseWriter, r *http.Request) {
	seriesID, err := strconv.ParseInt(chi.URLParam(r, "seriesId"), 10, 64)
	if err != nil || seriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Series not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to get series")
		return
	}

	books, err := c.store.ListBooksBySeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list series books")
		return
	}
	if len(books) == 0 {
		jsonError(w, http.StatusNotFound, "Series has no books")
		return
	}

	tags, err := c.store.GetTagsForSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get series tags")
		return
	}

	authors, err := c.store.GetAuthorsForSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get series authors")
		return
	}

	data, err := buildSeriesComicInfoArchive(series, books, tags, authors)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to export ComicInfo archive")
		return
	}

	filename := sanitizeDownloadFilename(firstNonEmpty(nullString(series.Title), series.Name))
	if filename == "" {
		filename = fmt.Sprintf("series-%d", series.ID)
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-ComicInfo.zip"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (c *Controller) exportBookComicInfo(w http.ResponseWriter, r *http.Request) {
	bookID, err := strconv.ParseInt(chi.URLParam(r, "bookId"), 10, 64)
	if err != nil || bookID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	book, err := c.store.GetBook(r.Context(), bookID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Book not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to get book")
		return
	}

	series, err := c.store.GetSeries(r.Context(), book.SeriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get series")
		return
	}

	books, err := c.store.ListBooksBySeries(r.Context(), book.SeriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list series books")
		return
	}

	tags, err := c.store.GetTagsForSeries(r.Context(), book.SeriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get series tags")
		return
	}

	authors, err := c.store.GetAuthorsForSeries(r.Context(), book.SeriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get series authors")
		return
	}

	info := buildComicInfoForBook(book, series, books, tags, authors)
	data, err := parser.MarshalComicInfo(info)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to export ComicInfo")
		return
	}

	filename := sanitizeDownloadFilename(strings.TrimSuffix(book.Name, filepath.Ext(book.Name)))
	if filename == "" {
		filename = fmt.Sprintf("book-%d", book.ID)
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-ComicInfo.xml"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// writeBookComicInfo 把单本书的 ComicInfo.xml 写回其 cbz/zip 归档（原子替换、不备份）。
// rar/cbr 无法写入，返回 415；这是修改用户原始文件的敏感操作，由前端二次确认后触发。
func (c *Controller) writeBookComicInfo(w http.ResponseWriter, r *http.Request) {
	bookID, err := strconv.ParseInt(chi.URLParam(r, "bookId"), 10, 64)
	if err != nil || bookID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	book, err := c.store.GetBook(r.Context(), bookID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Book not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to get book")
		return
	}

	info, err := c.buildBookComicInfo(r, book)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to build ComicInfo")
		return
	}
	data, err := parser.MarshalComicInfo(info)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to build ComicInfo")
		return
	}

	if err := parser.WriteComicInfoIntoArchive(book.Path, data); err != nil {
		if errors.Is(err, parser.ErrArchiveNotWritable) {
			jsonError(w, http.StatusUnsupportedMediaType, apiText(requestLocale(r), "comicinfo.write.unsupported"))
			return
		}
		slog.Error("write ComicInfo into archive failed", "book_id", bookID, "path", book.Path, "error", err)
		jsonError(w, http.StatusInternalServerError, apiText(requestLocale(r), "comicinfo.write.failed"))
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{"status": "ok"})
}

// writeSeriesComicInfo 把整个系列所有可写归档（cbz/zip）的 ComicInfo.xml 写回，返回写入/跳过计数。
// rar/cbr 条目按“跳过”处理，不视为失败。
func (c *Controller) writeSeriesComicInfo(w http.ResponseWriter, r *http.Request) {
	seriesID, err := strconv.ParseInt(chi.URLParam(r, "seriesId"), 10, 64)
	if err != nil || seriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Series not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to get series")
		return
	}

	books, err := c.store.ListBooksBySeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list series books")
		return
	}
	tags, err := c.store.GetTagsForSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get series tags")
		return
	}
	authors, err := c.store.GetAuthorsForSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get series authors")
		return
	}

	written, skipped, failed := 0, 0, 0
	for _, book := range books {
		info := buildComicInfoForBook(book, series, books, tags, authors)
		data, marshalErr := parser.MarshalComicInfo(info)
		if marshalErr != nil {
			failed++
			continue
		}
		if err := parser.WriteComicInfoIntoArchive(book.Path, data); err != nil {
			if errors.Is(err, parser.ErrArchiveNotWritable) {
				skipped++
				continue
			}
			slog.Error("write ComicInfo into archive failed", "book_id", book.ID, "path", book.Path, "error", err)
			failed++
			continue
		}
		written++
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"written": written,
		"skipped": skipped,
		"failed":  failed,
	})
}

// buildBookComicInfo 复用导出路径的构造逻辑，从数据库聚合出单本书的 ComicInfo。
func (c *Controller) buildBookComicInfo(r *http.Request, book database.Book) (parser.ComicInfo, error) {
	series, err := c.store.GetSeries(r.Context(), book.SeriesID)
	if err != nil {
		return parser.ComicInfo{}, err
	}
	books, err := c.store.ListBooksBySeries(r.Context(), book.SeriesID)
	if err != nil {
		return parser.ComicInfo{}, err
	}
	tags, err := c.store.GetTagsForSeries(r.Context(), book.SeriesID)
	if err != nil {
		return parser.ComicInfo{}, err
	}
	authors, err := c.store.GetAuthorsForSeries(r.Context(), book.SeriesID)
	if err != nil {
		return parser.ComicInfo{}, err
	}
	return buildComicInfoForBook(book, series, books, tags, authors), nil
}

func buildSeriesComicInfoArchive(series database.Series, books []database.Book, tags []database.Tag, authors []database.Author) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	seen := make(map[string]int, len(books))

	for _, book := range books {
		info := buildComicInfoForBook(book, series, books, tags, authors)
		data, err := parser.MarshalComicInfo(info)
		if err != nil {
			_ = writer.Close()
			return nil, err
		}

		base := sanitizeDownloadFilename(strings.TrimSuffix(book.Name, filepath.Ext(book.Name)))
		if base == "" {
			base = fmt.Sprintf("book-%d", book.ID)
		}
		entryName := uniqueComicInfoArchiveEntry(base, seen)
		entry, err := writer.Create(entryName)
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		if _, err := entry.Write(data); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func buildComicInfoForBook(book database.Book, series database.Series, books []database.Book, tags []database.Tag, authors []database.Author) parser.ComicInfo {
	info := parser.ComicInfo{
		Title:       firstNonEmpty(nullString(book.Title), book.Name),
		Series:      firstNonEmpty(nullString(series.Title), series.Name),
		Summary:     firstNonEmpty(nullString(book.Summary), nullString(series.Summary)),
		Number:      firstNonEmpty(nullString(book.Number), formatNullableFloat(book.SortNumber)),
		Volume:      book.Volume,
		Count:       len(books),
		Publisher:   nullString(series.Publisher),
		Genre:       joinTagNames(tags),
		LanguageISO: nullString(series.Language),
		PageCount:   int(book.PageCount),
	}

	if series.Rating.Valid {
		info.CommunityRating = float32(series.Rating.Float64)
	}

	for _, author := range authors {
		switch strings.ToLower(strings.TrimSpace(author.Role)) {
		case "writer", "author", "story":
			info.Writer = appendCommaValue(info.Writer, author.Name)
		case "penciller", "artist", "illustrator":
			info.Penciller = appendCommaValue(info.Penciller, author.Name)
		case "letterer":
			info.Letterer = appendCommaValue(info.Letterer, author.Name)
		case "translator":
			info.Translator = appendCommaValue(info.Translator, author.Name)
		}
	}

	return info
}

func nullString(value sql.NullString) string {
	if value.Valid {
		return strings.TrimSpace(value.String)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func formatNullableFloat(value sql.NullFloat64) string {
	if !value.Valid {
		return ""
	}
	return strconv.FormatFloat(value.Float64, 'f', -1, 64)
}

func joinTagNames(tags []database.Tag) string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		if name := strings.TrimSpace(tag.Name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func appendCommaValue(current, next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	return current + ", " + next
}

func uniqueComicInfoArchiveEntry(base string, seen map[string]int) string {
	count := seen[base] + 1
	seen[base] = count
	if count == 1 {
		return base + "/ComicInfo.xml"
	}
	return fmt.Sprintf("%s-%d/ComicInfo.xml", base, count)
}

func sanitizeDownloadFilename(name string) string {
	name = strings.TrimSpace(name)
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		default:
			return r
		}
	}, name)
}
