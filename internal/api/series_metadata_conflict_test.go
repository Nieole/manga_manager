// 守「系列元数据的手工保存不静默覆盖别人的改动」这条契约：编辑期间服务端被别的途径改过时，
// 保存必须被拒（409）而不是照写不误，且先写的那份改动一条都不能少。

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/proposal"
)

// seriesMetadataVersionOf 走 GET /context 读出这一刻的元数据版本，即用户打开编辑器时手里那份。
func seriesMetadataVersionOf(t *testing.T, controller *Controller, seriesID int64) string {
	t.Helper()
	rec := httptest.NewRecorder()
	controller.getSeriesContext(rec, requestWithRouteParam(http.MethodGet, "/api/series/1/context", nil, "seriesId", strconv.FormatInt(seriesID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected series context 200, got %d", rec.Code)
	}
	var body struct {
		MetadataVersion string `json:"metadata_version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode series context failed: %v", err)
	}
	if body.MetadataVersion == "" {
		t.Fatalf("expected series context to carry metadata_version")
	}
	return body.MetadataVersion
}

// putSeriesInfo 打一次手工保存，返回状态码与响应体。
func putSeriesInfo(t *testing.T, controller *Controller, seriesID int64, payload string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	controller.updateSeriesInfo(rec, requestWithRouteParam(http.MethodPut, "/api/series/info/1", []byte(payload), "seriesId", strconv.FormatInt(seriesID, 10)))
	var body map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&body)
	return rec.Code, body
}

func TestUpdateSeriesInfoConcurrentEditConflict(t *testing.T) {
	t.Run("编辑期间服务端被改过，保存被拒且先写的改动不丢", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		// 用户打开编辑器，手里拿到的是这一刻的版本。
		userVersion := seriesMetadataVersionOf(t, controller, series.ID)

		// 与此同时，别的途径写了这个系列：另一个标签页保存、刮削应用了一条提案、另一个用户改了，
		// 三者对本用例是同一件事——服务端的值变了。
		code, _ := putSeriesInfo(t, controller, series.ID, `{
			"title":"别人写的标题",
			"summary":"别人写的简介",
			"locked_fields":"",
			"tags":["别人加的标签"],
			"authors":[{"name":"别人加的作者","role":"story"}],
			"links":[],
			"expected_version":"`+userVersion+`"
		}`)
		if code != http.StatusOK {
			t.Fatalf("expected the first writer to succeed, got %d", code)
		}

		// 用户此刻按下保存，手里还是老版本。
		code, body := putSeriesInfo(t, controller, series.ID, `{
			"title":"我写的标题",
			"summary":"我写的简介",
			"locked_fields":"",
			"tags":["我加的标签"],
			"authors":[],
			"links":[],
			"expected_version":"`+userVersion+`"
		}`)
		if code != http.StatusConflict {
			t.Fatalf("expected 409 on stale save, got %d", code)
		}
		if v, _ := body["current_version"].(string); v == "" {
			t.Fatalf("expected the conflict payload to carry current_version, got %+v", body)
		}

		// 先写的那份必须原样还在——被拒的保存一个字段都不该落库。
		saved, err := store.GetSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetSeries failed: %v", err)
		}
		if saved.Title.String != "别人写的标题" || saved.Summary.String != "别人写的简介" {
			t.Fatalf("stale save overwrote the earlier writer: title=%q summary=%q", saved.Title.String, saved.Summary.String)
		}
		tags, err := store.GetTagsForSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetTagsForSeries failed: %v", err)
		}
		if len(tags) != 1 || tags[0].Name != "别人加的标签" {
			t.Fatalf("stale save wiped the earlier writer's tags: %+v", tags)
		}
		authors, err := store.GetAuthorsForSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetAuthorsForSeries failed: %v", err)
		}
		if len(authors) != 1 || authors[0].Name != "别人加的作者" {
			t.Fatalf("stale save wiped the earlier writer's authors: %+v", authors)
		}
	})

	t.Run("编辑期间刮削应用了一条提案，保存被拒", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		userVersion := seriesMetadataVersionOf(t, controller, series.ID)

		// 版本是内容指纹，不由写入方维护——所以刮削这条路径一行没改，也能让用户手里那份版本作废。
		queued, err := controller.proposals.Queue(t.Context(), series, fullScrapeResult(), "bangumi", "Alpha", proposal.QueueOptions{})
		if err != nil || queued.Status != proposal.QueueQueued {
			t.Fatalf("queue proposal failed: status=%q err=%v", queued.Status, err)
		}
		applyRec := httptest.NewRecorder()
		controller.applyMetadataReview(applyRec, requestWithRouteParam(http.MethodPost, "/api/reviews/1/apply", nil,
			"reviewId", strconv.FormatInt(queued.Proposal.ID, 10)))
		if applyRec.Code != http.StatusOK {
			t.Fatalf("expected apply proposal 200, got %d: %s", applyRec.Code, applyRec.Body.String())
		}

		code, _ := putSeriesInfo(t, controller, series.ID, `{
			"title":"我写的标题","summary":"我写的简介","locked_fields":"","tags":[],"authors":[],"links":[],
			"expected_version":"`+userVersion+`"
		}`)
		if code != http.StatusConflict {
			t.Fatalf("expected 409 after a proposal landed, got %d", code)
		}
		saved, err := store.GetSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetSeries failed: %v", err)
		}
		if saved.Summary.String != "Scraped summary" {
			t.Fatalf("stale save overwrote the applied proposal: %q", saved.Summary.String)
		}
	})

	t.Run("没人插队时保存照常落库", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		version := seriesMetadataVersionOf(t, controller, series.ID)
		code, _ := putSeriesInfo(t, controller, series.ID, `{
			"title":"我写的标题",
			"summary":"我写的简介",
			"locked_fields":"title",
			"tags":["我加的标签"],
			"authors":[],
			"links":[],
			"expected_version":"`+version+`"
		}`)
		if code != http.StatusOK {
			t.Fatalf("expected a solo edit to succeed, got %d", code)
		}
		saved, err := store.GetSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetSeries failed: %v", err)
		}
		if saved.Title.String != "我写的标题" || saved.Summary.String != "我写的简介" {
			t.Fatalf("solo edit did not land: %+v", saved)
		}
	})

	t.Run("不带版本的调用按旧行为放行", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		code, _ := putSeriesInfo(t, controller, series.ID, `{
			"title":"脚本写的标题",
			"locked_fields":"",
			"tags":[],
			"authors":[],
			"links":[]
		}`)
		if code != http.StatusOK {
			t.Fatalf("expected a versionless caller to be accepted, got %d", code)
		}
		saved, err := store.GetSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetSeries failed: %v", err)
		}
		if saved.Title.String != "脚本写的标题" {
			t.Fatalf("versionless save did not land: %+v", saved)
		}
	})

	t.Run("拿到冲突里的最新版本后可以再存一次", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		userVersion := seriesMetadataVersionOf(t, controller, series.ID)
		if code, _ := putSeriesInfo(t, controller, series.ID, `{
			"title":"别人写的标题","locked_fields":"","tags":[],"authors":[],"links":[],
			"expected_version":"`+userVersion+`"
		}`); code != http.StatusOK {
			t.Fatalf("expected the first writer to succeed, got %d", code)
		}

		code, body := putSeriesInfo(t, controller, series.ID, `{
			"title":"我写的标题","locked_fields":"","tags":[],"authors":[],"links":[],
			"expected_version":"`+userVersion+`"
		}`)
		if code != http.StatusConflict {
			t.Fatalf("expected 409 on stale save, got %d", code)
		}
		current, _ := body["current_version"].(string)

		// 用户看过提示后决定仍以自己这份为准：带上刚拿到的版本重发即可。
		if code, _ := putSeriesInfo(t, controller, series.ID, `{
			"title":"我写的标题","locked_fields":"","tags":[],"authors":[],"links":[],
			"expected_version":"`+current+`"
		}`); code != http.StatusOK {
			t.Fatalf("expected the retry with the fresh version to succeed, got %d", code)
		}
		saved, err := store.GetSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetSeries failed: %v", err)
		}
		if saved.Title.String != "我写的标题" {
			t.Fatalf("retry did not land: %+v", saved)
		}
	})

	t.Run("派生统计的刷新不算元数据改动", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		before := seriesMetadataVersionOf(t, controller, series.ID)

		// 扫描、阅读进度、收藏都会碰 series / series_stats，但它们不是编辑器管辖的字段：
		// 若把它们算进版本，用户读完一本书就存不下简介了。
		if err := store.RefreshSeriesStats(t.Context(), series.ID); err != nil {
			t.Fatalf("RefreshSeriesStats failed: %v", err)
		}
		if err := store.UpdateSeriesFavorite(t.Context(), database.UpdateSeriesFavoriteParams{ID: series.ID, IsFavorite: true}); err != nil {
			t.Fatalf("UpdateSeriesFavorite failed: %v", err)
		}

		if after := seriesMetadataVersionOf(t, controller, series.ID); after != before {
			t.Fatalf("derived-stat refresh moved the metadata version: %q -> %q", before, after)
		}
	})
}
