// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

var errNoMetadataChanges = errors.New("no metadata changes")

type metadataReviewFieldDraft struct {
	Name       string
	Label      string
	Current    string
	Proposed   string
	Confidence float64
	Locked     bool
}

type metadataReviewFieldView struct {
	Name       string  `json:"name"`
	Label      string  `json:"label"`
	Current    string  `json:"current"`
	Proposed   string  `json:"proposed"`
	Confidence float64 `json:"confidence"`
	Locked     bool    `json:"locked"`
	Source     string  `json:"source"`
	SourceURL  string  `json:"source_url"`
	Status     string  `json:"status"`
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
	Success  bool    `json:"success"`
	Applied  []int64 `json:"applied,omitempty"`
	Rejected []int64 `json:"rejected,omitempty"`
	Skipped  []int64 `json:"skipped,omitempty"`
	Failed   []int64 `json:"failed,omitempty"`
	Total    int     `json:"total"`
	Mode     string  `json:"mode"`
}

type metadataApplyOptions struct {
	ProviderName string
	SourceURL    string
	SourceID     int64
	Confidence   float64
	SourceQuery  string
	ReviewID     *int64
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
		return strings.Title(strings.ReplaceAll(name, "_", " "))
	}
}

func metadataLockedFieldSet(series database.Series) map[string]bool {
	locked := make(map[string]bool)
	if series.LockedFields.Valid && series.LockedFields.String != "" {
		for _, field := range strings.Split(series.LockedFields.String, ",") {
			field = strings.TrimSpace(field)
			if field != "" {
				locked[field] = true
			}
		}
	}
	return locked
}

func metadataSeriesDisplayTitle(series database.Series) string {
	if series.Title.Valid && strings.TrimSpace(series.Title.String) != "" {
		return series.Title.String
	}
	return series.Name
}

func metadataJoinTags(tags []database.Tag) string {
	if len(tags) == 0 {
		return ""
	}
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		if strings.TrimSpace(tag.Name) == "" {
			continue
		}
		names = append(names, tag.Name)
	}
	sort.Strings(names)
	return strings.Join(names, " / ")
}

func metadataCleanTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	cleaned := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, tag)
	}
	sort.Strings(cleaned)
	return cleaned
}

func metadataJoinProposedTags(tags []string) string {
	return strings.Join(metadataCleanTags(tags), " / ")
}

func metadataAuthorEntryString(name, role string) string {
	name = strings.TrimSpace(name)
	role = strings.TrimSpace(role)
	if role == "" {
		return name
	}
	return name + " (" + role + ")"
}

func metadataJoinAuthors(authors []database.Author) string {
	if len(authors) == 0 {
		return ""
	}
	parts := make([]string, 0, len(authors))
	for _, a := range authors {
		if strings.TrimSpace(a.Name) == "" {
			continue
		}
		parts = append(parts, metadataAuthorEntryString(a.Name, a.Role))
	}
	sort.Strings(parts)
	return strings.Join(parts, " / ")
}

func metadataJoinProposedAuthors(authors []metadata.SeriesAuthor) string {
	if len(authors) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(authors))
	parts := make([]string, 0, len(authors))
	for _, a := range authors {
		entry := metadataAuthorEntryString(a.Name, a.Role)
		if entry == "" {
			continue
		}
		key := strings.ToLower(entry)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, entry)
	}
	sort.Strings(parts)
	return strings.Join(parts, " / ")
}

func metadataParseAuthors(value string) []metadata.SeriesAuthor {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	out := make([]metadata.SeriesAuthor, 0)
	for _, raw := range strings.Split(value, " / ") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		role := ""
		name := raw
		if idx := strings.LastIndex(raw, " ("); idx >= 0 && strings.HasSuffix(raw, ")") {
			name = strings.TrimSpace(raw[:idx])
			role = strings.TrimSpace(raw[idx+2 : len(raw)-1])
		}
		if name == "" {
			continue
		}
		out = append(out, metadata.SeriesAuthor{Name: name, Role: role})
	}
	return out
}

func metadataDefaultConfidence(providerName string) float64 {
	name := strings.ToLower(strings.TrimSpace(providerName))
	switch name {
	case "bangumi":
		return 0.9
	case "openai", "ollama", "llm", "openai-legacy":
		return 0.6
	}
	// providerName 也可能是 provider.Name() 的显示名（如 "Ollama LLM"、
	// "OpenAI/Compatible LLM"、"OpenAI Compatible (v1/chat/completions)"），
	// 这里对显示名做包含匹配兜底，确保各 LLM provider 一视同仁。
	switch {
	case strings.Contains(name, "bangumi"):
		return 0.9
	case strings.Contains(name, "llm"), strings.Contains(name, "openai"), strings.Contains(name, "ollama"):
		return 0.6
	default:
		return 0.5
	}
}

func metadataSourceURL(providerName string, result *metadata.SeriesMetadata) string {
	if result == nil {
		return ""
	}
	if strings.TrimSpace(result.SourceURL) != "" {
		return strings.TrimSpace(result.SourceURL)
	}
	if result.SourceID > 0 && strings.EqualFold(providerName, "bangumi") {
		return fmt.Sprintf("https://bgm.tv/subject/%d", result.SourceID)
	}
	return ""
}

func metadataBuildFieldDrafts(series database.Series, tags []database.Tag, authors []database.Author, result *metadata.SeriesMetadata, providerName string) []metadataReviewFieldDraft {
	locked := metadataLockedFieldSet(series)
	currentTags := metadataJoinTags(tags)
	proposedTags := metadataJoinProposedTags(result.Tags)
	currentAuthors := metadataJoinAuthors(authors)
	proposedAuthors := metadataJoinProposedAuthors(result.Authors)
	confidence := result.Confidence
	if confidence <= 0 {
		confidence = metadataDefaultConfidence(providerName)
	}

	drafts := []metadataReviewFieldDraft{
		{
			Name:       "title",
			Label:      metadataFieldLabel("title"),
			Current:    metadataSeriesDisplayTitle(series),
			Proposed:   strings.TrimSpace(result.Title),
			Confidence: confidence,
			Locked:     locked["title"],
		},
		{
			Name:       "summary",
			Label:      metadataFieldLabel("summary"),
			Current:    seriesText(series.Summary),
			Proposed:   strings.TrimSpace(result.Summary),
			Confidence: confidence,
			Locked:     locked["summary"],
		},
		{
			Name:       "publisher",
			Label:      metadataFieldLabel("publisher"),
			Current:    seriesText(series.Publisher),
			Proposed:   strings.TrimSpace(result.Publisher),
			Confidence: confidence,
			Locked:     locked["publisher"],
		},
		{
			Name:       "status",
			Label:      metadataFieldLabel("status"),
			Current:    seriesText(series.Status),
			Proposed:   metadata.NormalizeStatusCode(result.Status),
			Confidence: confidence,
			Locked:     locked["status"],
		},
		{
			Name:       "rating",
			Label:      metadataFieldLabel("rating"),
			Current:    seriesNumber(series.Rating),
			Proposed:   metadataNumber(result.Rating),
			Confidence: confidence,
			Locked:     locked["rating"],
		},
		{
			Name:       "tags",
			Label:      metadataFieldLabel("tags"),
			Current:    currentTags,
			Proposed:   proposedTags,
			Confidence: confidence,
			Locked:     locked["tags"],
		},
		{
			Name:       "authors",
			Label:      metadataFieldLabel("authors"),
			Current:    currentAuthors,
			Proposed:   proposedAuthors,
			Confidence: confidence,
			Locked:     locked["authors"],
		},
	}

	changes := make([]metadataReviewFieldDraft, 0, len(drafts))
	for _, draft := range drafts {
		if draft.Proposed == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(draft.Current), strings.TrimSpace(draft.Proposed)) {
			continue
		}
		changes = append(changes, draft)
	}
	return changes
}

func seriesText(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return strings.TrimSpace(value.String)
}

func seriesNumber(value sql.NullFloat64) string {
	if !value.Valid || value.Float64 <= 0 {
		return ""
	}
	return strconv.FormatFloat(value.Float64, 'f', 1, 64)
}

func metadataNumber(value float64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', 1, 64)
}

func metadataReviewFieldToView(field database.MetadataReviewField) metadataReviewFieldView {
	return metadataReviewFieldView{
		Name:       field.FieldName,
		Label:      metadataFieldLabel(field.FieldName),
		Current:    field.CurrentValue,
		Proposed:   field.ProposedValue,
		Confidence: field.Confidence,
		Locked:     field.Locked,
		Source:     field.Source,
		SourceURL:  field.SourceUrl,
		Status:     field.Status,
	}
}

func metadataReviewToView(review database.MetadataReview, fields []database.MetadataReviewField) metadataReviewView {
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

func metadataReviewInboxRowToView(row database.ListPendingMetadataReviewInboxRow, fields []database.MetadataReviewField) metadataReviewInboxItemView {
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
	return metadataReviewInboxItemView{
		metadataReviewView: metadataReviewToView(review, fields),
		LibraryID:          row.LibraryID,
		LibraryName:        row.LibraryName,
		SeriesName:         row.SeriesName,
		SeriesTitle:        row.SeriesTitle,
		CoverBookID:        row.CoverBookID,
		FieldCount:         row.FieldCount,
		LockedFieldCount:   row.LockedFieldCount,
	}
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

func filterMetadataReviewFieldsForMode(fields []database.MetadataReviewField, mode string) []database.MetadataReviewField {
	if mode != "fill_empty" {
		return fields
	}
	filtered := make([]database.MetadataReviewField, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field.CurrentValue) == "" {
			filtered = append(filtered, field)
		}
	}
	return filtered
}

func normalizeMetadataReviewValue(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func metadataReviewDraftSignature(changes []metadataReviewFieldDraft, sourceID int64) map[string]string {
	signature := make(map[string]string, len(changes)+1)
	for _, change := range changes {
		signature[change.Name] = normalizeMetadataReviewValue(change.Current) + "\x00" + normalizeMetadataReviewValue(change.Proposed)
	}
	// 把来源条目 ID 纳入签名：不同来源（如 Bangumi 不同 subject）即使字段 diff 相同
	// 也应视为不同候选，分别入队，避免复用旧 review 时丢弃用户选中条目的 source_url。
	signature["\x00source_id"] = strconv.FormatInt(sourceID, 10)
	return signature
}

func metadataReviewFieldsSignature(fields []database.MetadataReviewField, sourceID int64) map[string]string {
	signature := make(map[string]string, len(fields)+1)
	for _, field := range fields {
		signature[field.FieldName] = normalizeMetadataReviewValue(field.CurrentValue) + "\x00" + normalizeMetadataReviewValue(field.ProposedValue)
	}
	signature["\x00source_id"] = strconv.FormatInt(sourceID, 10)
	return signature
}

func metadataReviewSignaturesEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if right[key] != leftValue {
			return false
		}
	}
	return true
}

func (c *Controller) queueMetadataReview(ctx context.Context, series database.Series, result *metadata.SeriesMetadata, providerName, sourceQuery string) (database.MetadataReview, []database.MetadataReviewField, bool, error) {
	var createdReview database.MetadataReview
	var createdFields []database.MetadataReviewField
	sourceURL := metadataSourceURL(providerName, result)
	confidence := result.Confidence
	if confidence <= 0 {
		confidence = metadataDefaultConfidence(providerName)
	}

	var isNew bool
	err := c.store.ExecTx(ctx, func(q *database.Queries) error {
		tags, err := q.GetTagsForSeries(ctx, series.ID)
		if err != nil {
			return err
		}
		authors, err := q.GetAuthorsForSeries(ctx, series.ID)
		if err != nil {
			return err
		}

		changes := metadataBuildFieldDrafts(series, tags, authors, result, providerName)
		if len(changes) == 0 {
			return errNoMetadataChanges
		}
		nextSignature := metadataReviewDraftSignature(changes, int64(result.SourceID))
		pendingReviews, err := q.ListPendingMetadataReviewsBySeries(ctx, series.ID)
		if err != nil {
			return err
		}
		for _, pendingReview := range pendingReviews {
			fields, err := q.ListMetadataReviewFields(ctx, pendingReview.ID)
			if err != nil {
				return err
			}
			if metadataReviewSignaturesEqual(nextSignature, metadataReviewFieldsSignature(fields, pendingReview.SourceID)) {
				createdReview = pendingReview
				createdFields = fields
				isNew = false
				return nil
			}
		}

		payload, _ := json.Marshal(result)
		review, err := q.CreateMetadataReview(ctx, database.CreateMetadataReviewParams{
			SeriesID:    series.ID,
			Provider:    strings.TrimSpace(providerName),
			SourceUrl:   sourceURL,
			SourceID:    int64(result.SourceID),
			SourceQuery: strings.TrimSpace(sourceQuery),
			Summary:     fmt.Sprintf("Queued %d metadata fields for review", len(changes)),
			Confidence:  confidence,
			Status:      "pending",
			RawPayload:  string(payload),
		})
		if err != nil {
			return err
		}

		for _, change := range changes {
			field, err := q.CreateMetadataReviewField(ctx, database.CreateMetadataReviewFieldParams{
				ReviewID:      review.ID,
				FieldName:     change.Name,
				CurrentValue:  change.Current,
				ProposedValue: change.Proposed,
				Confidence:    change.Confidence,
				Source:        strings.TrimSpace(providerName),
				SourceUrl:     sourceURL,
				Locked:        change.Locked,
				Status:        "pending",
			})
			if err != nil {
				return err
			}
			createdFields = append(createdFields, field)
		}

		createdReview = review
		isNew = true
		return nil
	})

	if err != nil {
		return database.MetadataReview{}, nil, false, err
	}

	return createdReview, createdFields, isNew, nil
}

func (c *Controller) applyReviewedMetadata(ctx context.Context, series database.Series, review database.MetadataReview, fields []database.MetadataReviewField) error {
	if len(fields) == 0 {
		return errNoMetadataChanges
	}
	metadataResult := &metadata.SeriesMetadata{
		Provider:   review.Provider,
		SourceID:   int(review.SourceID),
		SourceURL:  review.SourceUrl,
		Confidence: review.Confidence,
	}
	for _, field := range fields {
		switch field.FieldName {
		case "title":
			metadataResult.Title = field.ProposedValue
		case "summary":
			metadataResult.Summary = field.ProposedValue
		case "publisher":
			metadataResult.Publisher = field.ProposedValue
		case "status":
			metadataResult.Status = field.ProposedValue
		case "rating":
			if parsed, err := strconv.ParseFloat(field.ProposedValue, 64); err == nil {
				metadataResult.Rating = parsed
			}
		case "tags":
			if field.ProposedValue != "" {
				raw := strings.Split(field.ProposedValue, " / ")
				metadataResult.Tags = make([]string, 0, len(raw))
				for _, tag := range raw {
					tag = strings.TrimSpace(tag)
					if tag != "" {
						metadataResult.Tags = append(metadataResult.Tags, tag)
					}
				}
			}
		case "authors":
			metadataResult.Authors = metadataParseAuthors(field.ProposedValue)
		}
	}

	return c.applyMetadataToSeries(ctx, series, metadataResult, metadataApplyOptions{
		ProviderName: review.Provider,
		SourceURL:    review.SourceUrl,
		SourceID:     review.SourceID,
		Confidence:   review.Confidence,
		SourceQuery:  review.SourceQuery,
		ReviewID:     &review.ID,
	})
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

func (c *Controller) loadSeriesMetadataReview(ctx context.Context, seriesID int64) (metadataReviewResponse, error) {
	reviews, err := c.store.ListPendingMetadataReviewsBySeries(ctx, seriesID)
	if err != nil {
		return metadataReviewResponse{}, err
	}
	if reviews == nil {
		reviews = []database.MetadataReview{}
	}

	provenanceRows, err := c.store.GetSeriesMetadataProvenance(ctx, seriesID)
	if err != nil {
		return metadataReviewResponse{}, err
	}
	if provenanceRows == nil {
		provenanceRows = []database.SeriesMetadataProvenance{}
	}

	payload := metadataReviewResponse{
		Reviews:    make([]metadataReviewView, 0, len(reviews)),
		Provenance: make([]metadataProvenanceView, 0, len(provenanceRows)),
	}
	for _, review := range reviews {
		fields, err := c.store.ListMetadataReviewFields(ctx, review.ID)
		if err != nil {
			return metadataReviewResponse{}, err
		}
		payload.Reviews = append(payload.Reviews, metadataReviewToView(review, fields))
	}
	for _, row := range provenanceRows {
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
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	total, err := c.store.CountPendingMetadataReviewInbox(r.Context(), database.CountPendingMetadataReviewInboxParams{
		LibraryID: libraryID,
		Provider:  provider,
		Query:     query,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to count metadata reviews")
		return
	}

	rows, err := c.store.ListPendingMetadataReviewInbox(r.Context(), database.ListPendingMetadataReviewInboxParams{
		LibraryID: libraryID,
		Provider:  provider,
		Query:     query,
		Offset:    offset,
		Limit:     limit,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list metadata reviews")
		return
	}
	if rows == nil {
		rows = []database.ListPendingMetadataReviewInboxRow{}
	}

	payload := metadataReviewInboxResponse{
		Items:  make([]metadataReviewInboxItemView, 0, len(rows)),
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
	// 一次性批量取所有 review 的字段，避免逐条查询造成 N+1（此前每行 review 单独发一次 SQL）。
	fieldsByReview := make(map[int64][]database.MetadataReviewField, len(rows))
	if len(rows) > 0 {
		reviewIDs := make([]int64, 0, len(rows))
		for _, row := range rows {
			reviewIDs = append(reviewIDs, row.ID)
		}
		allFields, err := c.store.ListMetadataReviewFieldsByReviews(r.Context(), reviewIDs)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to load metadata review fields")
			return
		}
		for _, f := range allFields {
			fieldsByReview[f.ReviewID] = append(fieldsByReview[f.ReviewID], f)
		}
	}
	for _, row := range rows {
		payload.Items = append(payload.Items, metadataReviewInboxRowToView(row, fieldsByReview[row.ID]))
	}

	jsonResponse(w, http.StatusOK, payload)
}

func (c *Controller) applyMetadataReview(w http.ResponseWriter, r *http.Request) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid review ID")
		return
	}

	review, err := c.store.GetMetadataReview(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Metadata review not found")
		return
	}
	if strings.ToLower(review.Status) != "pending" {
		jsonError(w, http.StatusConflict, "Metadata review is not pending")
		return
	}

	fields, err := c.store.ListMetadataReviewFields(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load review fields")
		return
	}

	series, err := c.store.GetSeries(r.Context(), review.SeriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	if err := c.applyReviewedMetadata(r.Context(), series, review, fields); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to apply metadata review")
		return
	}

	if _, err := c.store.UpdateMetadataReviewStatus(r.Context(), database.UpdateMetadataReviewStatusParams{
		Status: "applied",
		ID:     review.ID,
	}); err != nil {
		slog.Warn("Failed to mark metadata review applied", "review_id", review.ID, "error", err)
	}

	updated, _ := c.store.GetSeries(r.Context(), review.SeriesID)
	jsonResponse(w, http.StatusOK, map[string]any{
		"success": true,
		"series":  updated,
		"review":  reviewID,
	})
}

func (c *Controller) bulkApplyMetadataReviews(w http.ResponseWriter, r *http.Request) {
	ids, mode, ok := metadataReviewIDsFromRequest(w, r)
	if !ok {
		return
	}

	result := metadataReviewBulkResponse{
		Success: true,
		Applied: make([]int64, 0, len(ids)),
		Skipped: make([]int64, 0),
		Failed:  make([]int64, 0),
		Total:   len(ids),
		Mode:    mode,
	}
	for _, id := range ids {
		review, err := c.store.GetMetadataReview(r.Context(), id)
		if err != nil || strings.ToLower(review.Status) != "pending" {
			result.Failed = append(result.Failed, id)
			continue
		}
		fields, err := c.store.ListMetadataReviewFields(r.Context(), id)
		if err != nil {
			result.Failed = append(result.Failed, id)
			continue
		}
		fields = filterMetadataReviewFieldsForMode(fields, mode)
		if len(fields) == 0 {
			result.Skipped = append(result.Skipped, id)
			continue
		}
		series, err := c.store.GetSeries(r.Context(), review.SeriesID)
		if err != nil {
			result.Failed = append(result.Failed, id)
			continue
		}
		if err := c.applyReviewedMetadata(r.Context(), series, review, fields); err != nil {
			result.Failed = append(result.Failed, id)
			continue
		}
		if _, err := c.store.UpdateMetadataReviewStatus(r.Context(), database.UpdateMetadataReviewStatusParams{
			Status: "applied",
			ID:     review.ID,
		}); err != nil {
			result.Failed = append(result.Failed, id)
			continue
		}
		result.Applied = append(result.Applied, id)
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

	review, err := c.store.GetMetadataReview(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Metadata review not found")
		return
	}

	if _, err := c.store.UpdateMetadataReviewStatus(r.Context(), database.UpdateMetadataReviewStatusParams{
		Status: "rejected",
		ID:     review.ID,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to reject metadata review")
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
		Failed:   make([]int64, 0),
		Total:    len(ids),
		Mode:     mode,
	}
	for _, id := range ids {
		review, err := c.store.GetMetadataReview(r.Context(), id)
		if err != nil || strings.ToLower(review.Status) != "pending" {
			result.Failed = append(result.Failed, id)
			continue
		}
		if _, err := c.store.UpdateMetadataReviewStatus(r.Context(), database.UpdateMetadataReviewStatusParams{
			Status: "rejected",
			ID:     review.ID,
		}); err != nil {
			result.Failed = append(result.Failed, id)
			continue
		}
		result.Rejected = append(result.Rejected, id)
	}
	if len(result.Failed) > 0 {
		result.Success = false
	}
	jsonResponse(w, http.StatusOK, result)
}
