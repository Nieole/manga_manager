package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

type aiGroupingReviewSeriesView struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
}

type aiGroupingReviewCollectionView struct {
	ID                  int64                        `json:"id"`
	ReviewID            int64                        `json:"review_id"`
	Name                string                       `json:"name"`
	Description         string                       `json:"description"`
	SeriesIDs           []int64                      `json:"series_ids"`
	Series              []aiGroupingReviewSeriesView `json:"series"`
	SeriesCount         int64                        `json:"series_count"`
	Status              string                       `json:"status"`
	CreatedCollectionID *int64                       `json:"created_collection_id,omitempty"`
}

type aiGroupingReviewView struct {
	ID              int64                            `json:"id"`
	LibraryID       int64                            `json:"library_id"`
	LibraryName     string                           `json:"library_name"`
	Provider        string                           `json:"provider"`
	Status          string                           `json:"status"`
	Summary         string                           `json:"summary"`
	CandidateCount  int64                            `json:"candidate_count"`
	CollectionCount int64                            `json:"collection_count"`
	CreatedAt       time.Time                        `json:"created_at"`
	UpdatedAt       time.Time                        `json:"updated_at"`
	AppliedAt       *time.Time                       `json:"applied_at,omitempty"`
	RejectedAt      *time.Time                       `json:"rejected_at,omitempty"`
	Collections     []aiGroupingReviewCollectionView `json:"collections"`
}

type aiGroupingReviewsResponse struct {
	Items  []aiGroupingReviewView `json:"items"`
	Total  int64                  `json:"total"`
	Limit  int64                  `json:"limit"`
	Offset int64                  `json:"offset"`
}

func aiGroupingParseSeriesIDs(raw string) []int64 {
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return []int64{}
	}
	clean := ids[:0]
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	return clean
}

func aiGroupingSeriesViews(ctx context.Context, q database.Querier, ids []int64) []aiGroupingReviewSeriesView {
	if len(ids) == 0 {
		return []aiGroupingReviewSeriesView{}
	}
	rows, err := q.GetSeriesNamesByIDs(ctx, ids)
	if err != nil {
		return []aiGroupingReviewSeriesView{}
	}
	byID := make(map[int64]database.GetSeriesNamesByIDsRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	views := make([]aiGroupingReviewSeriesView, 0, len(ids))
	for _, id := range ids {
		row, ok := byID[id]
		if !ok {
			continue
		}
		views = append(views, aiGroupingReviewSeriesView{
			ID:    row.ID,
			Name:  row.Name,
			Title: row.Title,
		})
	}
	return views
}

func aiGroupingReviewCollectionToView(ctx context.Context, q database.Querier, row database.AiGroupingReviewCollection) aiGroupingReviewCollectionView {
	ids := aiGroupingParseSeriesIDs(row.SeriesIds)
	view := aiGroupingReviewCollectionView{
		ID:          row.ID,
		ReviewID:    row.ReviewID,
		Name:        row.Name,
		Description: strings.TrimSpace(row.Description),
		SeriesIDs:   ids,
		Series:      aiGroupingSeriesViews(ctx, q, ids),
		SeriesCount: row.SeriesCount,
		Status:      row.Status,
	}
	if row.CreatedCollectionID.Valid {
		value := row.CreatedCollectionID.Int64
		view.CreatedCollectionID = &value
	}
	return view
}

func aiGroupingReviewToView(ctx context.Context, q database.Querier, review database.AiGroupingReview, libraryName string) aiGroupingReviewView {
	collections, _ := q.ListAIGroupingReviewCollections(ctx, review.ID)
	view := aiGroupingReviewView{
		ID:              review.ID,
		LibraryID:       review.LibraryID,
		LibraryName:     libraryName,
		Provider:        review.Provider,
		Status:          review.Status,
		Summary:         review.Summary,
		CandidateCount:  review.CandidateCount,
		CollectionCount: review.CollectionCount,
		CreatedAt:       review.CreatedAt,
		UpdatedAt:       review.UpdatedAt,
		Collections:     make([]aiGroupingReviewCollectionView, 0, len(collections)),
	}
	if review.AppliedAt.Valid {
		value := review.AppliedAt.Time
		view.AppliedAt = &value
	}
	if review.RejectedAt.Valid {
		value := review.RejectedAt.Time
		view.RejectedAt = &value
	}
	for _, collection := range collections {
		view.Collections = append(view.Collections, aiGroupingReviewCollectionToView(ctx, q, collection))
	}
	return view
}

type aiGroupingReviewProposal struct {
	Name        string
	Description string
	SeriesIDs   []int64
}

func normalizeAIGroupingReviewProposals(candidates []metadata.CandidateSeries, groups []metadata.AIGroupCollection) []aiGroupingReviewProposal {
	candidateIDs := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidateIDs[candidate.ID] = struct{}{}
	}

	proposals := make([]aiGroupingReviewProposal, 0, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			continue
		}
		seriesIDs := make([]int64, 0, len(group.SeriesIDs))
		seen := make(map[int64]struct{}, len(group.SeriesIDs))
		for _, id := range group.SeriesIDs {
			if _, ok := candidateIDs[id]; !ok {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			seriesIDs = append(seriesIDs, id)
		}
		sort.Slice(seriesIDs, func(i, j int) bool { return seriesIDs[i] < seriesIDs[j] })
		if len(seriesIDs) == 0 {
			continue
		}
		proposals = append(proposals, aiGroupingReviewProposal{
			Name:        name,
			Description: strings.TrimSpace(group.Description),
			SeriesIDs:   seriesIDs,
		})
	}
	return proposals
}

func (c *Controller) createAIGroupingReview(ctx context.Context, libraryID int64, providerName string, candidates []metadata.CandidateSeries, groups []metadata.AIGroupCollection) (database.AiGroupingReview, int, error) {
	var created database.AiGroupingReview
	proposals := normalizeAIGroupingReviewProposals(candidates, groups)
	if len(proposals) == 0 {
		return created, 0, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"candidates":  candidates,
		"collections": groups,
		"proposals":   proposals,
	})

	err := c.store.ExecTx(ctx, func(q *database.Queries) error {
		review, err := q.CreateAIGroupingReview(ctx, database.CreateAIGroupingReviewParams{
			LibraryID:       libraryID,
			Provider:        strings.TrimSpace(providerName),
			Status:          "pending",
			Summary:         "AI grouping review queued",
			RawPayload:      string(payload),
			CandidateCount:  int64(len(candidates)),
			CollectionCount: int64(len(proposals)),
		})
		if err != nil {
			return err
		}

		for _, proposal := range proposals {
			rawIDs, _ := json.Marshal(proposal.SeriesIDs)
			if _, err := q.CreateAIGroupingReviewCollection(ctx, database.CreateAIGroupingReviewCollectionParams{
				ReviewID:    review.ID,
				Name:        proposal.Name,
				Description: proposal.Description,
				SeriesIds:   string(rawIDs),
				SeriesCount: int64(len(proposal.SeriesIDs)),
				Status:      "pending",
			}); err != nil {
				return err
			}
		}

		created = review
		return nil
	})
	return created, len(proposals), err
}

func (c *Controller) listAIGroupingReviews(w http.ResponseWriter, r *http.Request) {
	libraryID, _ := strconv.ParseInt(r.URL.Query().Get("library_id"), 10, 64)
	if libraryID < 0 {
		libraryID = 0
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if offset < 0 {
		offset = 0
	}

	params := database.ListAIGroupingReviewsParams{
		LibraryID: libraryID,
		Status:    status,
		Offset:    offset,
		Limit:     limit,
	}
	total, err := c.store.CountAIGroupingReviews(r.Context(), database.CountAIGroupingReviewsParams{
		LibraryID: params.LibraryID,
		Status:    params.Status,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to count AI grouping reviews")
		return
	}
	rows, err := c.store.ListAIGroupingReviews(r.Context(), params)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list AI grouping reviews")
		return
	}
	if rows == nil {
		rows = []database.ListAIGroupingReviewsRow{}
	}

	payload := aiGroupingReviewsResponse{
		Items:  make([]aiGroupingReviewView, 0, len(rows)),
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
	for _, row := range rows {
		review := database.AiGroupingReview{
			ID:              row.ID,
			LibraryID:       row.LibraryID,
			Provider:        row.Provider,
			Status:          row.Status,
			Summary:         row.Summary,
			RawPayload:      row.RawPayload,
			CandidateCount:  row.CandidateCount,
			CollectionCount: row.CollectionCount,
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
			AppliedAt:       row.AppliedAt,
			RejectedAt:      row.RejectedAt,
		}
		payload.Items = append(payload.Items, aiGroupingReviewToView(r.Context(), c.store, review, row.LibraryName))
	}
	jsonResponse(w, http.StatusOK, payload)
}

func (c *Controller) applyAIGroupingReview(w http.ResponseWriter, r *http.Request) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid AI grouping review ID")
		return
	}
	review, err := c.store.GetAIGroupingReview(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "AI grouping review not found")
		return
	}
	if review.Status != "pending" {
		jsonError(w, http.StatusConflict, "AI grouping review is not pending")
		return
	}
	collections, err := c.store.ListAIGroupingReviewCollections(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load AI grouping review")
		return
	}
	applied := 0
	err = c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		for _, item := range collections {
			if item.Status != "pending" {
				continue
			}
			seriesIDs := aiGroupingParseSeriesIDs(item.SeriesIds)
			if strings.TrimSpace(item.Name) == "" || len(seriesIDs) == 0 {
				continue
			}
			created, err := q.CreateCollection(r.Context(), database.CreateCollectionParams{
				Name:        item.Name,
				Description: sql.NullString{String: strings.TrimSpace(item.Description), Valid: strings.TrimSpace(item.Description) != ""},
			})
			if err != nil {
				return err
			}
			for _, seriesID := range seriesIDs {
				if err := q.AddSeriesToCollection(r.Context(), database.AddSeriesToCollectionParams{
					CollectionID: created.ID,
					SeriesID:     seriesID,
				}); err != nil {
					return err
				}
			}
			if err := q.TouchCollection(r.Context(), created.ID); err != nil {
				return err
			}
			if err := q.MarkAIGroupingReviewCollectionApplied(r.Context(), database.MarkAIGroupingReviewCollectionAppliedParams{
				CreatedCollectionID: sql.NullInt64{Int64: created.ID, Valid: true},
				ID:                  item.ID,
			}); err != nil {
				return err
			}
			applied++
		}
		_, err := q.UpdateAIGroupingReviewStatus(r.Context(), database.UpdateAIGroupingReviewStatusParams{
			Status: "applied",
			ID:     review.ID,
		})
		return err
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to apply AI grouping review")
		return
	}
	c.PublishEvent("refresh")
	jsonResponse(w, http.StatusOK, map[string]any{
		"success":     true,
		"review_id":   reviewID,
		"collections": applied,
	})
}

func (c *Controller) rejectAIGroupingReview(w http.ResponseWriter, r *http.Request) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid AI grouping review ID")
		return
	}
	review, err := c.store.GetAIGroupingReview(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "AI grouping review not found")
		return
	}
	if review.Status != "pending" {
		jsonError(w, http.StatusConflict, "AI grouping review is not pending")
		return
	}
	if err := c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		if err := q.MarkAIGroupingReviewCollectionsRejected(r.Context(), review.ID); err != nil {
			return err
		}
		_, err := q.UpdateAIGroupingReviewStatus(r.Context(), database.UpdateAIGroupingReviewStatusParams{
			Status: "rejected",
			ID:     review.ID,
		})
		return err
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to reject AI grouping review")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"success":   true,
		"review_id": reviewID,
	})
}
