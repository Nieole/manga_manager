// 业务说明：本文件是「个人系列短评」（第 6 项）的 HTTP 层：当前用户对某系列的评分（1-5）+ 短文本的读写。
// 与全局 series.rating（刮削元数据）区分——这是每用户的私人评价。未登录（首启/单用户）时读为空、写为无操作。

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type seriesReviewResponse struct {
	Exists    bool       `json:"exists"`
	Rating    *float64   `json:"rating"`
	Review    string     `json:"review"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

func (c *Controller) getSeriesReview(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	uid := c.currentUserID(r)
	if uid == 0 {
		jsonResponse(w, http.StatusOK, seriesReviewResponse{Exists: false})
		return
	}
	rv, found, err := c.store.GetUserSeriesReview(r.Context(), uid, seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load review")
		return
	}
	if !found {
		jsonResponse(w, http.StatusOK, seriesReviewResponse{Exists: false})
		return
	}
	resp := seriesReviewResponse{Exists: true, Review: rv.Review, UpdatedAt: &rv.UpdatedAt}
	if rv.Rating.Valid {
		v := rv.Rating.Float64
		resp.Rating = &v
	}
	jsonResponse(w, http.StatusOK, resp)
}

func (c *Controller) putSeriesReview(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	var req struct {
		Rating *float64 `json:"rating"`
		Review string   `json:"review"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if req.Rating != nil && (*req.Rating < 0 || *req.Rating > 5) {
		jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "review.rating_range"))
		return
	}
	uid := c.currentUserID(r)
	if uid == 0 {
		jsonResponse(w, http.StatusOK, seriesReviewResponse{Exists: false})
		return
	}
	review := strings.TrimSpace(req.Review)
	// 评分与短评都为空 = 清空该条短评。
	if req.Rating == nil && review == "" {
		if err := c.store.DeleteUserSeriesReview(r.Context(), uid, seriesID); err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to save review")
			return
		}
		jsonResponse(w, http.StatusOK, seriesReviewResponse{Exists: false})
		return
	}
	if err := c.store.UpsertUserSeriesReview(r.Context(), uid, seriesID, req.Rating, review); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save review")
		return
	}
	resp := seriesReviewResponse{Exists: true, Review: review, Rating: req.Rating}
	now := time.Now()
	resp.UpdatedAt = &now
	jsonResponse(w, http.StatusOK, resp)
}

func (c *Controller) deleteSeriesReview(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	if uid := c.currentUserID(r); uid > 0 {
		if err := c.store.DeleteUserSeriesReview(r.Context(), uid, seriesID); err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to delete review")
			return
		}
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}
