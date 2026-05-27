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

func metadataDefaultConfidence(providerName string) float64 {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "bangumi":
		return 0.9
	case "openai", "ollama", "llm", "openai-legacy":
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

func metadataBuildFieldDrafts(series database.Series, tags []database.Tag, result *metadata.SeriesMetadata, providerName string) []metadataReviewFieldDraft {
	locked := metadataLockedFieldSet(series)
	currentTags := metadataJoinTags(tags)
	proposedTags := metadataJoinProposedTags(result.Tags)
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

func metadataReviewDraftSignature(changes []metadataReviewFieldDraft) map[string]string {
	signature := make(map[string]string, len(changes))
	for _, change := range changes {
		signature[change.Name] = normalizeMetadataReviewValue(change.Current) + "\x00" + normalizeMetadataReviewValue(change.Proposed)
	}
	return signature
}

func metadataReviewFieldsSignature(fields []database.MetadataReviewField) map[string]string {
	signature := make(map[string]string, len(fields))
	for _, field := range fields {
		signature[field.FieldName] = normalizeMetadataReviewValue(field.CurrentValue) + "\x00" + normalizeMetadataReviewValue(field.ProposedValue)
	}
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

func (c *Controller) queueMetadataReview(ctx context.Context, series database.Series, result *metadata.SeriesMetadata, providerName, sourceQuery string) (database.MetadataReview, []database.MetadataReviewField, error) {
	var createdReview database.MetadataReview
	var createdFields []database.MetadataReviewField
	sourceURL := metadataSourceURL(providerName, result)
	confidence := result.Confidence
	if confidence <= 0 {
		confidence = metadataDefaultConfidence(providerName)
	}

	err := c.store.ExecTx(ctx, func(q *database.Queries) error {
		tags, err := q.GetTagsForSeries(ctx, series.ID)
		if err != nil {
			return err
		}

		changes := metadataBuildFieldDrafts(series, tags, result, providerName)
		if len(changes) == 0 {
			return errNoMetadataChanges
		}
		nextSignature := metadataReviewDraftSignature(changes)
		pendingReviews, err := q.ListPendingMetadataReviewsBySeries(ctx, series.ID)
		if err != nil {
			return err
		}
		for _, pendingReview := range pendingReviews {
			fields, err := q.ListMetadataReviewFields(ctx, pendingReview.ID)
			if err != nil {
				return err
			}
			if metadataReviewSignaturesEqual(nextSignature, metadataReviewFieldsSignature(fields)) {
				createdReview = pendingReview
				createdFields = fields
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
		return nil
	})

	if err != nil {
		return database.MetadataReview{}, nil, err
	}

	return createdReview, createdFields, nil
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
	for _, row := range rows {
		fields, err := c.store.ListMetadataReviewFields(r.Context(), row.ID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to load metadata review fields")
			return
		}
		payload.Items = append(payload.Items, metadataReviewInboxRowToView(row, fields))
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
