package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"manga-manager/internal/application/seriesediting"
	"manga-manager/internal/seriesmeta"
	"net/http"
)

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

// BulkEditSeriesRequest 批量增量编辑多个系列的元数据；未提供的字段（nil/空）不改。
type BulkEditSeriesRequest struct {
	SeriesIDs  []int64  `json:"series_ids"`
	AddTags    []string `json:"add_tags"`
	RemoveTags []string `json:"remove_tags"`
	Status     *string  `json:"status"`
	Publisher  *string  `json:"publisher"`
}

func (c *organizationHTTP) updateSeriesInfo(w http.ResponseWriter, r *http.Request) {
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
	result, err := c.metadata.Save(r.Context(), seriesID, seriesmeta.ManualInput{
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

func (c *organizationHTTP) bulkEditSeries(w http.ResponseWriter, r *http.Request) {
	var req BulkEditSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	result, err := c.selection.Edit(r.Context(), req.SeriesIDs, seriesmeta.SelectionInput{
		AddTags: req.AddTags, RemoveTags: req.RemoveTags, Status: req.Status, Publisher: req.Publisher,
	})
	response := selectionResponse(result)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to bulk edit series", "count", len(req.SeriesIDs), "error", err)
		response.Error = "Failed to bulk edit series"
		jsonResponse(w, http.StatusInternalServerError, response)
		return
	}
	jsonResponse(w, http.StatusOK, response)
}
