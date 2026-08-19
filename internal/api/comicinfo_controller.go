package api

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/storageio"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
	"manga-manager/internal/taskrun"

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

	// 真实（可能含中文）标题走 filename*=；ASCII 兜底用 series id，永远可解析。
	displayName := sanitizeDownloadFilename(firstNonEmpty(nullString(series.Title), series.Name)) + "-ComicInfo.zip"
	asciiName := fmt.Sprintf("series-%d-ComicInfo.zip", series.ID)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDispositionAttachment(asciiName, displayName))
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

	displayName := sanitizeDownloadFilename(strings.TrimSuffix(book.Name, filepath.Ext(book.Name))) + "-ComicInfo.xml"
	asciiName := fmt.Sprintf("book-%d-ComicInfo.xml", book.ID)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", contentDispositionAttachment(asciiName, displayName))
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

	if err := c.launchWriteSeriesComicInfoTask(series, books, tags, authors); err != nil {
		writeTaskLaunchError(w, err, "ComicInfo write is already running for this series", "Failed to start ComicInfo write")
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]interface{}{
		"status":   "started",
		"task_key": writeComicInfoTaskKey(seriesID),
		"total":    len(books),
	})
}

// writeComicInfoTaskKey 拼回写任务的**任务键**。HTTP 层要把它回给前端，任务声明也要用它，
// 因此它是两处共用的一份实现而不是各拼各的。
func writeComicInfoTaskKey(seriesID int64) string {
	return fmt.Sprintf("write_comicinfo_series_%d", seriesID)
}

// launchWriteSeriesComicInfoTask 是 ComicInfo 回写任务的启动点，走引擎的启动入口。
//
// 回写整个系列要逐本解压重压，大系列可以跑到分钟级，因此必须做成后台任务：同步做会让请求一直
// 挂到最后一本写完、中途无法取消也看不到进度。它**可取消但不可暂停**——每本书都是一次原子替换，
// 停在两本之间没有额外好处，而任务中心的暂停控件正是由 CanPause 决定的。
//
// 系列、书目、标签与作者由调用方备齐后传入：任务声明要一次性落地，其中的作用域显示名来自系列。
func (c *Controller) launchWriteSeriesComicInfoTask(series database.Series, books []database.Book, tags []database.Tag, authors []database.Author) error {
	spec := TaskSpec{
		Key:          writeComicInfoTaskKey(series.ID),
		Type:         "write_comicinfo",
		StartCode:    "task.msg.write_comicinfo.start",
		Total:        len(books),
		CanCancel:    true,
		Metadata:     map[string]string{"series_id": strconv.FormatInt(series.ID, 10)},
		ScopeName:    series.Name,
		CompleteCode: "task.msg.write_comicinfo.complete",
		CancelCode:   "task.msg.write_comicinfo.cancelled",
		FailCode:     "task.msg.write_comicinfo.failed",
	}

	return c.taskEngine.Run(spec, func(ctx context.Context, tp *taskrun.Handle) (TaskResult, error) {
		written, skipped, failed := 0, 0, 0
		for i, book := range books {
			// 聚合与序列化是纯 CPU，留在**磁盘作业**之外：把它们夹进令牌的持有区间只会虚占这块盘
			// 的归档打开额度。序列化失败与回写失败同属这一本书的失败计数。
			info := buildComicInfoForBook(book, series, books, tags, authors)
			data, marshalErr := parser.MarshalComicInfo(info)
			if marshalErr != nil {
				failed++
				continue
			}

			// 走**磁盘作业**入口：归档回写是重 IO（逐本解压重压），不受调度会让阅读器取页明显卡顿。
			// 用 MetadataScan 这一类：它与扫描器读归档同属「后台读写归档」，共用同一档并发上限。
			// 闭包只捕获自己的错误——回写失败是这一本书的结局，中止只由 Do 返回的闸门错误决定。
			var writeErr error
			if _, err := c.diskWork.Do(ctx, diskwork.Work{Kind: storageio.WorkKindMetadataScan, Path: book.Path}, func() error {
				writeErr = parser.WriteComicInfoIntoArchive(book.Path, data)
				return nil
			}); err != nil {
				return TaskResult{}, err
			}

			switch {
			case writeErr == nil:
				written++
			case errors.Is(writeErr, parser.ErrArchiveNotWritable):
				skipped++
			default:
				slog.Error("write ComicInfo into archive failed", "book_id", book.ID, "path", book.Path, "error", writeErr)
				failed++
			}

			// 计数、书名与三个结局计数同属这一本书，必须整帧报出：拆开报会被投递水位撕断，
			// 撕开之后是什么样见 taskrun.Handle.Report。
			current, total := i+1, len(books)
			tp.Report(taskrun.Frame{
				Current: &current,
				Total:   &total,
				Phase:   "writing",
				Item:    book.Name,
				Code:    "task.msg.write_comicinfo.progress",
				Params:  map[string]string{"current": strconv.Itoa(current), "total": strconv.Itoa(total)},
				Metrics: map[string]int64{
					"written": int64(written), "skipped": int64(skipped), "failed": int64(failed),
				},
			})
		}
		return TaskResult{Params: map[string]string{
			"written": strconv.Itoa(written),
			"skipped": strconv.Itoa(skipped),
			"failed":  strconv.Itoa(failed),
		}}, nil
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
		Count:       parser.LenientInt(len(books)),
		Publisher:   nullString(series.Publisher),
		Genre:       joinTagNames(tags),
		LanguageISO: nullString(series.Language),
		PageCount:   parser.LenientInt(book.PageCount),
	}

	if series.Rating.Valid {
		info.CommunityRating = parser.LenientFloat(series.Rating.Float64)
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
