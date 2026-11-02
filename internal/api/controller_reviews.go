// 本文件是「个人系列短评」（第 6 项）的 HTTP 层：当前用户对某系列的评分（1-5）+ 短文本的读写。
// 与全局 series.rating（刮削元数据）区分——这是每用户的私人评价。未登录（首启/单用户）时读为空、写为无操作。

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"manga-manager/internal/seriesreview"
)

type seriesReviewResponse struct {
	Exists    bool       `json:"exists"`
	Rating    *float64   `json:"rating"`
	Review    string     `json:"review"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

func (c *ReaderHTTP) getSeriesReview(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	out, err := c.reviews.Get(r.Context(), subject, seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load review")
		return
	}
	jsonResponse(w, http.StatusOK, seriesReviewResponse(out))
}

func (c *ReaderHTTP) putSeriesReview(w http.ResponseWriter, r *http.Request) {
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
	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	out, err := c.reviews.Save(r.Context(), subject, seriesID, req.Rating, req.Review)
	if errors.Is(err, seriesreview.ErrRating) {
		jsonError(w, http.StatusBadRequest, apiText(requestLocale(r), "review.rating_range"))
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save review")
		return
	}
	jsonResponse(w, http.StatusOK, seriesReviewResponse(out))
}

func (c *ReaderHTTP) deleteSeriesReview(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	if err := c.reviews.Delete(r.Context(), subject, seriesID); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete review")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}
