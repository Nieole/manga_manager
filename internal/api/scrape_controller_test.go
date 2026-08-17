// 守刮削入口的传输语义：批量入口的 409（同类任务已在跑）与响应里的 outcome 结果码——
// 前端按 outcome 分支，换成解析中文 message 会随文案改动一起碎掉。
// 写回系列那条路径（锁定字段、标签作者、来源沿革与外链）归 internal/proposal 的写入器用例。

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

func TestScrapeSeriesMetadataValidationHandlers(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	invalidRec := httptest.NewRecorder()
	controller.scrapeSeriesMetadata(invalidRec, requestWithRouteParam(http.MethodPost, "/api/series/bad/scrape", nil, "seriesId", "bad"))
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid series id 400, got %d", invalidRec.Code)
	}

	notFoundRec := httptest.NewRecorder()
	controller.scrapeSeriesMetadata(notFoundRec, requestWithRouteParam(http.MethodPost, "/api/series/999/scrape", nil, "seriesId", "999"))
	if notFoundRec.Code != http.StatusNotFound {
		t.Fatalf("expected missing series 404, got %d", notFoundRec.Code)
	}

	_ = series
}

func TestBatchScrapeAllSeriesAndScrapeLibraryLocalBranches(t *testing.T) {
	t.Run("batch scrape returns zero when no libraries exist", func(t *testing.T) {
		controller, _, _, _ := newTestController(t)

		rec := httptest.NewRecorder()
		controller.batchScrapeAllSeries(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/scrape/all", bytes.NewBufferString(`{}`)))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode batch scrape response failed: %v", err)
		}
		if body["provider"] != "Bangumi" {
			t.Fatalf("expected Bangumi provider, got %+v", body)
		}
	})

	t.Run("batch scrape returns conflict when task already running", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		seedTask(t, controller.taskEngine, taskSeed{Key: "scrape_all_series", Type: "scrape", Total: 1})

		rec := httptest.NewRecorder()
		controller.batchScrapeAllSeries(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/scrape/all", bytes.NewBufferString(`{}`)))

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rec.Code)
		}
	})

	t.Run("scrape library validates library id", func(t *testing.T) {
		controller, _, _, _ := newTestController(t)

		rec := httptest.NewRecorder()
		controller.scrapeLibrary(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/bad/scrape", nil, "libraryId", "bad"))

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid library id 400, got %d", rec.Code)
		}
	})

	t.Run("scrape library returns zero when metadata already filled", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		lib, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		if _, err := controller.store.(*database.SqlStore).DB().Exec(`
			UPDATE series SET summary = ?, publisher = ? WHERE id = ?
		`, "filled", "publisher", series.ID); err != nil {
			t.Fatalf("seed series metadata failed: %v", err)
		}

		rec := httptest.NewRecorder()
		controller.scrapeLibrary(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/scrape", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode scrape library response failed: %v", err)
		}
		if body["provider"] != "Bangumi" {
			t.Fatalf("expected Bangumi provider, got %+v", body)
		}
	})

	t.Run("scrape library returns conflict when task already running", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		lib, _, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		taskKey := "scrape_library_" + strconv.FormatInt(lib.ID, 10)
		seedTask(t, controller.taskEngine, taskSeed{Key: taskKey, Type: "scrape", Total: 1})

		rec := httptest.NewRecorder()
		controller.scrapeLibrary(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/scrape", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rec.Code)
		}
	})
}

func TestApplyScrapedMetadataOutcomeCodes(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	body, err := json.Marshal(metadata.SeriesMetadata{
		Summary:  "A brand new summary produced by the scraper",
		Rating:   8.5,
		SourceID: 54321,
	})
	if err != nil {
		t.Fatalf("marshal metadata failed: %v", err)
	}

	post := func() map[string]any {
		req := requestWithRouteParam(http.MethodPost, "/api/series/1/scrape-apply?provider=bangumi", body, "seriesId", strconv.FormatInt(series.ID, 10))
		rec := httptest.NewRecorder()
		controller.applyScrapedMetadata(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response failed: %v", err)
		}
		return resp
	}

	// 首次提交带差异的元数据应入队审核，outcome=queued（前端据此本地化成功提示）。
	first := post()
	if first["outcome"] != "queued" {
		t.Fatalf("first apply: expected outcome=queued, got %v (resp=%v)", first["outcome"], first)
	}

	// 再次提交完全相同的数据应命中重复忽略，outcome=duplicate_ignored。
	second := post()
	if second["outcome"] != "duplicate_ignored" {
		t.Fatalf("second apply: expected outcome=duplicate_ignored, got %v (resp=%v)", second["outcome"], second)
	}
}
