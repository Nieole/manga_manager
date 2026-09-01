package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/proposal"
)

type metadataReviewFieldView struct {
	Name       string  `json:"name"`
	Label      string  `json:"label"`
	Current    string  `json:"current"`
	Proposed   string  `json:"proposed"`
	Confidence float64 `json:"confidence"`
	Locked     bool    `json:"locked"`
	Source     string  `json:"source"`
	SourceURL  string  `json:"source_url"`
}

type metadataReviewView struct {
	ID          int64                     `json:"id"`
	SeriesID    int64                     `json:"series_id"`
	Provider    string                    `json:"provider"`
	SourceURL   string                    `json:"source_url"`
	SourceID    int64                     `json:"source_id"`
	SourceQuery string                    `json:"source_query"`
	Summary     string                    `json:"summary"`
	Confidence  float64                   `json:"confidence"`
	Status      string                    `json:"status"`
	RawPayload  string                    `json:"raw_payload"`
	CreatedAt   time.Time                 `json:"created_at"`
	UpdatedAt   time.Time                 `json:"updated_at"`
	AppliedAt   *time.Time                `json:"applied_at,omitempty"`
	RejectedAt  *time.Time                `json:"rejected_at,omitempty"`
	Fields      []metadataReviewFieldView `json:"fields"`
}

type metadataProvenanceView struct {
	FieldName  string    `json:"field_name"`
	Label      string    `json:"label"`
	Value      string    `json:"value"`
	Source     string    `json:"source"`
	SourceURL  string    `json:"source_url"`
	Confidence float64   `json:"confidence"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type metadataReviewResponse struct {
	Reviews    []metadataReviewView     `json:"reviews"`
	Provenance []metadataProvenanceView `json:"provenance"`
}

type metadataReviewInboxItemView struct {
	metadataReviewView
	LibraryID        int64  `json:"library_id"`
	LibraryName      string `json:"library_name"`
	SeriesName       string `json:"series_name"`
	SeriesTitle      string `json:"series_title"`
	CoverBookID      int64  `json:"cover_book_id"`
	FieldCount       int64  `json:"field_count"`
	LockedFieldCount int64  `json:"locked_field_count"`
}

type metadataReviewInboxResponse struct {
	Items  []metadataReviewInboxItemView `json:"items"`
	Total  int64                         `json:"total"`
	Limit  int64                         `json:"limit"`
	Offset int64                         `json:"offset"`
}

type metadataReviewBulkRequest struct {
	ReviewIDs []int64 `json:"review_ids"`
	Mode      string  `json:"mode"`
}

type metadataReviewBulkResponse struct {
	Success bool    `json:"success"`
	Applied []int64 `json:"applied,omitempty"`
	// Partial 是「写入了一部分提案，但整条 review 仍待处理」的那些。
	// 与 Applied 分开是因为它们还留在收件箱里——混进 Applied 会让用户以为已经处理完了。
	Partial  []int64 `json:"partial,omitempty"`
	Rejected []int64 `json:"rejected,omitempty"`
	Skipped  []int64 `json:"skipped,omitempty"`
	// Conflict 收 proposal.ApplyConflict / proposal.RejectConflict。与 Failed 分开是因为
	// 它们并没有坏：混进去会让整条汇总提示变红，而用户什么也不用做。
	Conflict []int64 `json:"conflict,omitempty"`
	// Failed 只收真故障：查询报错、写入失败、提案查不到。
	Failed []int64 `json:"failed,omitempty"`
	Total  int     `json:"total"`
	Mode   string  `json:"mode"`
}

func metadataFieldLabel(name string) string {
	switch name {
	case "title":
		return "Title"
	case "summary":
		return "Summary"
	case "publisher":
		return "Publisher"
	case "status":
		return "Status"
	case "rating":
		return "Rating"
	case "tags":
		return "Tags"
	case "authors":
		return "Authors"
	case "source_link":
		return "Source link"
	default:
		return titleFieldLabel(strings.ReplaceAll(name, "_", " "))
	}
}

// titleFieldLabel upper-cases the first letter of each space-separated word (ASCII field keys such
// as "release date"), replacing the deprecated strings.Title for the metadata-field-label use case.
func titleFieldLabel(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// metadataReviewFieldToView 把待审字段转成前端视图：只搬字段，只补面向用户的标签。
// 锁定标志由 proposal.Field.Locked 定值，展示层不得参与那条规则。
func metadataReviewFieldToView(field proposal.Field) metadataReviewFieldView {
	return metadataReviewFieldView{
		Name:       field.Name,
		Label:      metadataFieldLabel(field.Name),
		Current:    field.Current,
		Proposed:   field.Proposed,
		Confidence: field.Confidence,
		Locked:     field.Locked,
		Source:     field.Source,
		SourceURL:  field.SourceURL,
	}
}

func metadataReviewToView(review database.MetadataReview, fields []proposal.Field) metadataReviewView {
	view := metadataReviewView{
		ID:          review.ID,
		SeriesID:    review.SeriesID,
		Provider:    review.Provider,
		SourceURL:   review.SourceUrl,
		SourceID:    review.SourceID,
		SourceQuery: review.SourceQuery,
		Summary:     review.Summary,
		Confidence:  review.Confidence,
		Status:      review.Status,
		RawPayload:  review.RawPayload,
		CreatedAt:   review.CreatedAt,
		UpdatedAt:   review.UpdatedAt,
		Fields:      make([]metadataReviewFieldView, 0, len(fields)),
	}
	if review.AppliedAt.Valid {
		value := review.AppliedAt.Time
		view.AppliedAt = &value
	}
	if review.RejectedAt.Valid {
		value := review.RejectedAt.Time
		view.RejectedAt = &value
	}
	for _, field := range fields {
		view.Fields = append(view.Fields, metadataReviewFieldToView(field))
	}
	return view
}

func metadataReviewInboxRowToView(row database.ListPendingMetadataReviewInboxRow, fields []proposal.Field) metadataReviewInboxItemView {
	review := database.MetadataReview{
		ID:          row.ID,
		SeriesID:    row.SeriesID,
		Provider:    row.Provider,
		SourceUrl:   row.SourceUrl,
		SourceID:    row.SourceID,
		SourceQuery: row.SourceQuery,
		Summary:     row.Summary,
		Confidence:  row.Confidence,
		Status:      row.Status,
		RawPayload:  row.RawPayload,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		AppliedAt:   row.AppliedAt,
		RejectedAt:  row.RejectedAt,
	}
	view := metadataReviewInboxItemView{
		metadataReviewView: metadataReviewToView(review, fields),
		LibraryID:          row.LibraryID,
		LibraryName:        row.LibraryName,
		SeriesName:         row.SeriesName,
		SeriesTitle:        row.SeriesTitle,
		CoverBookID:        row.CoverBookID,
	}
	// 两个计数都数眼前这批字段视图，不另取一份：列表的徽章与展开后 diff 面板上的锁标记
	// 出自同一个响应体，各算各的就会自相矛盾——徽章恒为 0，用户事前看不出哪些提案有锁，
	// 批量应用后才收到一串 locked_skipped。
	view.FieldCount = int64(len(view.Fields))
	for _, field := range view.Fields {
		if field.Locked {
			view.LockedFieldCount++
		}
	}
	return view
}

func provenanceToView(row database.SeriesMetadataProvenance) metadataProvenanceView {
	return metadataProvenanceView{
		FieldName:  row.FieldName,
		Label:      metadataFieldLabel(row.FieldName),
		Value:      row.Value,
		Source:     row.Source,
		SourceURL:  row.SourceUrl,
		Confidence: row.Confidence,
		UpdatedAt:  row.UpdatedAt,
	}
}

func metadataReviewIDsFromRequest(w http.ResponseWriter, r *http.Request) ([]int64, string, bool) {
	var req metadataReviewBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid metadata review payload")
		return nil, "", false
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "all"
	}
	if mode != "all" && mode != "fill_empty" {
		jsonError(w, http.StatusBadRequest, "Invalid metadata review mode")
		return nil, "", false
	}
	seen := make(map[int64]struct{}, len(req.ReviewIDs))
	ids := make([]int64, 0, len(req.ReviewIDs))
	for _, id := range req.ReviewIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		jsonError(w, http.StatusBadRequest, "No metadata review IDs provided")
		return nil, "", false
	}
	if len(ids) > 100 {
		jsonError(w, http.StatusBadRequest, "Too many metadata reviews in one request")
		return nil, "", false
	}
	return ids, mode, true
}

func (c *Controller) listSeriesMetadataReview(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	payload, err := c.loadSeriesMetadataReview(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list metadata reviews")
		return
	}

	jsonResponse(w, http.StatusOK, payload)
}

// loadSeriesMetadataReview 把模块给出的提案折成响应体。系列详情的上下文接口也用它，
// 两处因此不会对同一批数据给出两种形状。
func (c *Controller) loadSeriesMetadataReview(ctx context.Context, seriesID int64) (metadataReviewResponse, error) {
	listed, err := c.proposals.ListBySeries(ctx, seriesID)
	if err != nil {
		return metadataReviewResponse{}, err
	}

	payload := metadataReviewResponse{
		Reviews:    make([]metadataReviewView, 0, len(listed.Proposals)),
		Provenance: make([]metadataProvenanceView, 0, len(listed.Provenance)),
	}
	for _, item := range listed.Proposals {
		payload.Reviews = append(payload.Reviews, metadataReviewToView(item.Row, item.Fields))
	}
	for _, row := range listed.Provenance {
		payload.Provenance = append(payload.Provenance, provenanceToView(row))
	}

	return payload, nil
}

func emptyMetadataReviewResponse() metadataReviewResponse {
	return metadataReviewResponse{
		Reviews:    []metadataReviewView{},
		Provenance: []metadataProvenanceView{},
	}
}

func (c *Controller) listMetadataReviewInbox(w http.ResponseWriter, r *http.Request) {
	libraryID, _ := strconv.ParseInt(r.URL.Query().Get("library_id"), 10, 64)
	if libraryID < 0 {
		libraryID = 0
	}
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if offset < 0 {
		offset = 0
	}
	page, err := c.proposals.Inbox(r.Context(), proposal.InboxQuery{
		LibraryID: libraryID,
		Provider:  strings.TrimSpace(r.URL.Query().Get("provider")),
		Keyword:   strings.TrimSpace(r.URL.Query().Get("q")),
		Offset:    offset,
		Limit:     limit,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list metadata reviews")
		return
	}

	payload := metadataReviewInboxResponse{
		Items:  make([]metadataReviewInboxItemView, 0, len(page.Items)),
		Total:  page.Total,
		Limit:  limit,
		Offset: offset,
	}
	for _, item := range page.Items {
		payload.Items = append(payload.Items, metadataReviewInboxRowToView(item.Row, item.Fields))
	}

	jsonResponse(w, http.StatusOK, payload)
}

// applyOutcomeCode 把模块的结局分类折成前端分支用的结果码。
//
// 映射写在这里而不是直接把 ApplyStatus 的值当 JSON 发出去：结果码是前端契约，改它要看
// web/ 里谁在读；模块的分类是领域词汇，改它只看领域。两者今天字面相同，不该因此被焊在一起。
func applyOutcomeCode(status proposal.ApplyStatus) string {
	switch status {
	case proposal.ApplyApplied:
		return "applied"
	case proposal.ApplyPartial:
		return "partial"
	case proposal.ApplyLockedSkipped:
		return "locked_skipped"
	case proposal.ApplyNoChanges:
		return "no_changes"
	default:
		return ""
	}
}

func (c *Controller) applyMetadataReview(w http.ResponseWriter, r *http.Request) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid review ID")
		return
	}

	outcome, err := c.proposals.Apply(r.Context(), reviewID, proposal.ApplyModeAll)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to apply metadata review")
		return
	}

	switch outcome.Status {
	case proposal.ApplyNotFound:
		jsonError(w, http.StatusNotFound, "Metadata review not found")
		return
	case proposal.ApplyConflict:
		// 被并发的 apply/reject（或用户重复点击、多标签页）抢先，元数据写入已整体撤销。
		// 不回吐 series 快照：抢先者的结果才是准的，返回半新半旧的数据只会误导前端。
		jsonError(w, http.StatusConflict, "Metadata review is not pending")
		return
	case proposal.ApplyLockedSkipped, proposal.ApplyNoChanges:
		// 这两种都是**良性结果**而非服务端故障，必须回 200 + applied:false + 具体 outcome，
		// 不能落 500——落 500 时前端只能提示「服务器错误」，给不出可行动的建议
		//（「先解锁字段」还是「数据已是最新」）。提案保持待裁决，不消费掉。
		jsonResponse(w, http.StatusOK, map[string]any{
			"success": true,
			"applied": false,
			"outcome": applyOutcomeCode(outcome.Status),
			"review":  reviewID,
		})
		return
	}

	updated, _ := c.store.GetSeries(r.Context(), outcome.SeriesID)
	jsonResponse(w, http.StatusOK, map[string]any{
		"success":          true,
		"applied":          true,
		"outcome":          applyOutcomeCode(outcome.Status),
		"applied_fields":   outcome.Applied,
		"remaining_fields": outcome.Remaining,
		"series":           updated,
		"review":           reviewID,
	})
}

func (c *Controller) bulkApplyMetadataReviews(w http.ResponseWriter, r *http.Request) {
	ids, mode, ok := metadataReviewIDsFromRequest(w, r)
	if !ok {
		return
	}

	result := metadataReviewBulkResponse{
		Success:  true,
		Applied:  make([]int64, 0, len(ids)),
		Partial:  make([]int64, 0),
		Skipped:  make([]int64, 0),
		Conflict: make([]int64, 0),
		Failed:   make([]int64, 0),
		Total:    len(ids),
		Mode:     mode,
	}
	for _, id := range ids {
		outcome, err := c.proposals.Apply(r.Context(), id, proposal.ApplyMode(mode))
		if err != nil {
			result.Failed = append(result.Failed, id)
			continue
		}
		switch outcome.Status {
		case proposal.ApplyApplied:
			result.Applied = append(result.Applied, id)
		case proposal.ApplyPartial:
			// 只应用了一部分（fill_empty 筛掉的、或已被锁的）。单独成桶而不是塞进 Applied：
			// 这条提案还留在收件箱里等着处理，报成「已应用」会让用户以为它已经消失了。
			result.Partial = append(result.Partial, id)
		case proposal.ApplyLockedSkipped, proposal.ApplyNoChanges:
			// 良性结果落 Skipped 而不是 Failed——前端把 Failed 当成出错来提示，
			// 会让用户以为系统坏了。
			result.Skipped = append(result.Skipped, id)
		case proposal.ApplyConflict:
			result.Conflict = append(result.Conflict, id)
		default:
			// ApplyNotFound 落这里：入参指向的行不存在，是真故障。模块若新增一种结局而
			// 这里没跟上，也落 failed——保守：报成成功会让用户以为已经处理完了。
			result.Failed = append(result.Failed, id)
		}
	}
	if len(result.Failed) > 0 {
		result.Success = false
	}
	jsonResponse(w, http.StatusOK, result)
}

func (c *Controller) rejectMetadataReview(w http.ResponseWriter, r *http.Request) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid review ID")
		return
	}

	outcome, err := c.proposals.Reject(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to reject metadata review")
		return
	}
	switch outcome.Status {
	case proposal.RejectNotFound:
		jsonError(w, http.StatusNotFound, "Metadata review not found")
		return
	case proposal.RejectConflict:
		jsonError(w, http.StatusConflict, "Metadata review is not pending")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"success": true,
		"review":  reviewID,
	})
}

func (c *Controller) bulkRejectMetadataReviews(w http.ResponseWriter, r *http.Request) {
	ids, mode, ok := metadataReviewIDsFromRequest(w, r)
	if !ok {
		return
	}

	result := metadataReviewBulkResponse{
		Success:  true,
		Rejected: make([]int64, 0, len(ids)),
		Conflict: make([]int64, 0),
		Failed:   make([]int64, 0),
		Total:    len(ids),
		Mode:     mode,
	}
	for _, id := range ids {
		outcome, err := c.proposals.Reject(r.Context(), id)
		if err != nil {
			result.Failed = append(result.Failed, id)
			continue
		}
		// 归桶与批量应用一侧一致：被抢先落 Conflict，其余（RejectNotFound 与将来新增的
		// 结局）落 Failed。
		switch outcome.Status {
		case proposal.RejectRejected:
			result.Rejected = append(result.Rejected, id)
		case proposal.RejectConflict:
			result.Conflict = append(result.Conflict, id)
		default:
			result.Failed = append(result.Failed, id)
		}
	}
	if len(result.Failed) > 0 {
		result.Success = false
	}
	jsonResponse(w, http.StatusOK, result)
}
