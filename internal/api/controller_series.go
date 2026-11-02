package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
	"manga-manager/internal/seriescontext"
)

// parseReadState 归一化阅读状态查询参数，未识别值按“不筛选”处理。
func parseReadState(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "unread", "reading", "completed":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

// parseOptionalFloat 把可选的浮点查询参数解析为 *float64；空串或非法值返回 nil（不筛选）。
func parseOptionalFloat(v string) *float64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

// parseNonNegativeInt 解析非负整数查询参数；空串或非法/负值返回 0（不筛选）。
func parseNonNegativeInt(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// maxSeriesPageLimit 是 /api/series/search 单页返回的硬上限，防止超大 limit 拉全库导致 OOM/超时。
const maxSeriesPageLimit = 200

// getRecentReadSeries 返回该资源库下含有书籍最新阅读记录的系列

func (c *Controller) openSeriesDirectory(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	release, ok := c.guardLibraryRead(w, series.LibraryID)
	if !ok {
		return
	}
	defer release()
	series, err = c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	path := strings.TrimSpace(series.Path)
	if path == "" {
		jsonError(w, http.StatusBadRequest, "Series directory is not available")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Series directory does not exist")
		return
	}
	if !info.IsDir() {
		jsonError(w, http.StatusBadRequest, "Series path is not a directory")
		return
	}

	opener := c.openPath
	if opener == nil {
		opener = openPathInDefaultFileManager
	}
	if err := opener(path); err != nil {
		slog.Error("OpenSeriesDirectory Failed", "series_id", seriesID, "path", path, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to open series directory")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{"success": true})
}

type SeriesContextResponse struct {
	Series            database.Series         `json:"series"`
	Books             []database.Book         `json:"books"`
	Tags              []database.Tag          `json:"tags"`
	Authors           []database.Author       `json:"authors"`
	Links             []database.SeriesLink   `json:"links"`
	Volumes           []SeriesVolumeSummary   `json:"volumes"`
	Relations         []SeriesRelation        `json:"relations"`
	MetadataReview    metadataReviewResponse  `json:"metadata_review"`
	MetadataSummary   SeriesMetadataSummary   `json:"metadata_summary"`
	FailedTasks       []RunSnapshot           `json:"failed_tasks"`
	FailedTaskSummary SeriesFailedTaskSummary `json:"failed_task_summary"`
	Continue          SeriesContinue          `json:"continue"`
	// MetadataVersion 是本次下发的元数据版本，用户按下保存时原样带回（见 seriesmeta.Version）。
	MetadataVersion string `json:"metadata_version"`
}

// SeriesContinue is the shared subject reading summary exposed over HTTP.
type SeriesContinue = reading.Continue

type SeriesVolumeSummary struct {
	Name        string          `json:"name"`
	BookCount   int             `json:"book_count"`
	TotalPages  int64           `json:"total_pages"`
	ReadPages   int64           `json:"read_pages"`
	CoverBookID int64           `json:"cover_book_id,omitempty"`
	CoverPath   sql.NullString  `json:"cover_path"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Books       []database.Book `json:"books,omitempty"`
}

type SeriesFailedTaskSummary struct {
	Count    int        `json:"count"`
	LatestAt *time.Time `json:"latest_at,omitempty"`
}

type SeriesMetadataSummary struct {
	PendingReviewCount int `json:"pending_review_count"`
	ProvenanceCount    int `json:"provenance_count"`
}

func buildSeriesVolumeSummaries(books []reading.BookView, includeBooks bool) []SeriesVolumeSummary {
	volumes := seriescontext.Volumes(books, includeBooks)
	out := make([]SeriesVolumeSummary, 0, len(volumes))
	for _, v := range volumes {
		out = append(out, SeriesVolumeSummary{Name: v.Name, BookCount: v.BookCount, TotalPages: v.TotalPages, ReadPages: v.ReadPages, CoverBookID: v.CoverBookID, CoverPath: v.CoverPath, UpdatedAt: v.UpdatedAt, Books: readerBookResponses(v.Books)})
	}
	return out
}

// getSeriesCustomFields 返回某系列的自定义字段列表。

// replaceSeriesCustomFields 整体替换某系列的自定义字段。
func (c *Controller) replaceSeriesCustomFields(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	var req struct {
		Fields []database.SeriesCustomField `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if err := c.store.ReplaceSeriesCustomFields(r.Context(), seriesID, req.Fields); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save custom fields")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// renameTag 重命名标签；与已有标签重名会因 UNIQUE 约束失败，前端据此提示改用合并。
func (c *Controller) renameTag(w http.ResponseWriter, r *http.Request) {
	tagID, err := parseID(r, "tagId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid tag ID")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		jsonError(w, http.StatusBadRequest, "Tag name cannot be empty")
		return
	}
	if err := c.store.RenameTag(r.Context(), tagID, req.Name); err != nil {
		jsonError(w, http.StatusConflict, apiText(requestLocale(r), "tag.rename.conflict"))
		return
	}
	c.invalidateDashboardStatsCache("tag_rename")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// mergeTag 把 {tagId} 标签并入 target 标签（迁移全部系列关联后删除源标签）。
func (c *Controller) mergeTag(w http.ResponseWriter, r *http.Request) {
	tagID, err := parseID(r, "tagId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid tag ID")
		return
	}
	var req struct {
		TargetID int64 `json:"target_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if err := c.store.MergeTags(r.Context(), tagID, req.TargetID); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to merge tags")
		return
	}
	c.invalidateDashboardStatsCache("tag_merge")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (c *Controller) deleteTag(w http.ResponseWriter, r *http.Request) {
	tagID, err := parseID(r, "tagId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid tag ID")
		return
	}
	if err := c.store.DeleteTag(r.Context(), tagID); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete tag")
		return
	}
	c.invalidateDashboardStatsCache("tag_delete")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func parseFacetSearchLimit(r *http.Request) int {
	limit := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		return 30
	}
	if limit > 100 {
		return 100
	}
	return limit
}
