// 业务说明：本文件是业务回归测试，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

func TestApplyMetadataToSeriesHonorsLocksAndCreatesTagsAndLinks(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	db := controller.store.(*database.SqlStore).DB()
	if _, err := db.ExecContext(context.Background(), `
		UPDATE series
		SET title = ?, summary = ?, publisher = ?, rating = ?, locked_fields = ?
		WHERE id = ?
	`, "Locked Title", "Old summary", "Old publisher", 7.2, "title,publisher,tags", series.ID); err != nil {
		t.Fatalf("seed locked series metadata failed: %v", err)
	}

	series, err := controller.store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries failed: %v", err)
	}

	input := &metadata.SeriesMetadata{
		Title:     "New Title",
		Summary:   "New summary",
		Publisher: "New publisher",
		Rating:    8.8,
		Tags:      []string{"Action", "Mystery", "Action", " "},
		SourceID:  12345,
	}

	if err := controller.applyMetadataToSeries(context.Background(), series, input, metadataApplyOptions{
		ProviderName: "bangumi",
		SourceURL:    "https://bgm.tv/subject/12345",
		SourceID:     12345,
		Confidence:   0.91,
		SourceQuery:  series.Name,
	}); err != nil {
		t.Fatalf("applyMetadataToSeries failed: %v", err)
	}

	updated, err := controller.store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries after update failed: %v", err)
	}

	if !updated.Title.Valid || updated.Title.String != "Locked Title" {
		t.Fatalf("expected title lock preserved, got %+v", updated.Title)
	}
	if !updated.Publisher.Valid || updated.Publisher.String != "Old publisher" {
		t.Fatalf("expected publisher lock preserved, got %+v", updated.Publisher)
	}
	if !updated.Summary.Valid || updated.Summary.String != "New summary" {
		t.Fatalf("expected summary updated, got %+v", updated.Summary)
	}
	if !updated.Rating.Valid || updated.Rating.Float64 != 8.8 {
		t.Fatalf("expected rating updated, got %+v", updated.Rating)
	}

	tags, err := controller.store.GetTagsForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetTagsForSeries failed: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected locked tags to stay unchanged, got %d", len(tags))
	}

	links, err := controller.store.GetLinksForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetLinksForSeries failed: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 source link, got %d", len(links))
	}
	if links[0].Name != "Bangumi" || links[0].Url != "https://bgm.tv/subject/12345" {
		t.Fatalf("unexpected source link: %+v", links[0])
	}

	provenance, err := controller.store.GetSeriesMetadataProvenance(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeriesMetadataProvenance failed: %v", err)
	}
	provenanceByField := map[string]database.SeriesMetadataProvenance{}
	for _, row := range provenance {
		provenanceByField[row.FieldName] = row
	}
	if provenanceByField["summary"].Source != "bangumi" || provenanceByField["summary"].SourceUrl != "https://bgm.tv/subject/12345" {
		t.Fatalf("expected summary provenance from Bangumi, got %+v", provenanceByField["summary"])
	}
	if provenanceByField["summary"].Confidence != 0.91 {
		t.Fatalf("expected summary confidence recorded, got %+v", provenanceByField["summary"])
	}
	if _, ok := provenanceByField["title"]; ok {
		t.Fatalf("expected locked title not to receive scraped provenance, got %+v", provenanceByField["title"])
	}
	if _, ok := provenanceByField["publisher"]; ok {
		t.Fatalf("expected locked publisher not to receive scraped provenance, got %+v", provenanceByField["publisher"])
	}
	if _, ok := provenanceByField["tags"]; ok {
		t.Fatalf("expected locked tags not to receive scraped provenance, got %+v", provenanceByField["tags"])
	}
	if _, ok := provenanceByField["source_link"]; !ok {
		t.Fatalf("expected source_link provenance, got %+v", provenanceByField)
	}

	if err := controller.applyMetadataToSeries(context.Background(), updated, input, metadataApplyOptions{
		ProviderName: "bangumi",
		SourceURL:    "https://bgm.tv/subject/12345",
		SourceID:     12345,
		Confidence:   0.91,
		SourceQuery:  series.Name,
	}); err != nil {
		t.Fatalf("second applyMetadataToSeries failed: %v", err)
	}

	links, err = controller.store.GetLinksForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetLinksForSeries second pass failed: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected link deduplication, got %d links", len(links))
	}
}

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

		if !controller.startTask("scrape_all_series", "scrape", "running", 1) {
			t.Fatal("expected batch scrape task to start")
		}

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
		if !controller.startTask(taskKey, "scrape", "running", 1) {
			t.Fatal("expected library scrape task to start")
		}

		rec := httptest.NewRecorder()
		controller.scrapeLibrary(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/scrape", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rec.Code)
		}
	})
}
