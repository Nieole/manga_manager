package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/application/seriesediting"
	"manga-manager/internal/database"
	"manga-manager/internal/reading"
	"manga-manager/internal/seriescontext"
	"manga-manager/internal/seriesmeta"
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

type UpdateAuthorRequest = seriesmeta.Author

type UpdateLinkRequest = seriesmeta.Link

type UpdateSeriesRequest struct {
	Title        string                `json:"title"`
	Summary      string                `json:"summary"`
	Publisher    string                `json:"publisher"`
	Status       string                `json:"status"`
	Rating       float64               `json:"rating"`
	Language     string                `json:"language"`
	LockedFields string                `json:"locked_fields"`
	Tags         []string              `json:"tags"`
	Authors      []UpdateAuthorRequest `json:"authors"`
	Links        []UpdateLinkRequest   `json:"links"`
	// ExpectedVersion 是用户开始编辑时手里那份元数据的版本（见 seriesmeta.Version）。
	// 服务端此刻的版本与它不符，说明编辑期间有别的途径写过这个系列，保存被拒而不是照写不误——
	// 这份请求是「先清空再重建」的全量替换，照写会把别人新加的标签、作者、链接整批抹掉。
	//
	// 它是三态的，所以用指针：字段缺席表示调用方明示不参与并发控制（脚本、旧客户端），
	// 服务端不做检查，行为与从前一致；带了字段就必须带真版本，空串一律 400。空串只会来自
	// 「本该带版本却没拿到」的调用方，放行等于把防护栏静默关掉，用户还以为自己受着保护。
	ExpectedVersion *string `json:"expected_version"`
}

func (c *Controller) updateSeriesInfo(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	var req UpdateSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	result, err := seriesediting.New(seriesediting.NewSQLStore(c.store)).Save(r.Context(), seriesID, seriesmeta.ManualInput{
		Title: req.Title, Summary: req.Summary, Publisher: req.Publisher,
		Status: req.Status, Rating: req.Rating, Language: req.Language,
		LockedFields: req.LockedFields, Tags: req.Tags, Authors: req.Authors,
		Links: req.Links, ExpectedVersion: req.ExpectedVersion,
	})
	if errors.Is(err, seriesediting.ErrInvalidVersion) {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	if errors.Is(err, seriesmeta.ErrConflict) {
		// 409 而不是 412：请求里带的是我们自己定义的 expected_version 字段，不是 If-Match 前置条件头。
		// 载荷只报「变了」和最新版本，不报变的是哪几项——版本是内容指纹，服务端手里没有用户那一版的原值。
		jsonResponse(w, http.StatusConflict, map[string]any{
			"error":           "Series metadata was changed by someone else while you were editing",
			"current_version": result.CurrentVersion,
		})
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update series metadata")
		return
	}

	jsonResponse(w, http.StatusOK, result.Series)
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
