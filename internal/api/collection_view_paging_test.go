// 守智能书架成员端点的分页语义：书架配置里的每页大小只当默认值，
// limit/offset 由调用方决定，total 恒报全量命中数。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

// seedTaggedSmartCollection 造一个带 n 个同标签系列的资料库，并返回筛选规则 ID。
func seedTaggedSmartCollection(t *testing.T, n int, pageSize int) (*Controller, int64) {
	t.Helper()
	controller, store, _, rootDir := newTestController(t)
	ctx := context.Background()
	db := controller.store.(*database.SqlStore).DB()

	lib, _, _ := seedBookFixture(t, store, rootDir, "Library A", "Series 00", "S00 01.cbz", 10)
	tag, err := store.UpsertTag(ctx, "Action")
	if err != nil {
		t.Fatalf("UpsertTag failed: %v", err)
	}
	for i := 0; i < n; i++ {
		name := "Series " + strconv.Itoa(i)
		series, err := store.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
			LibraryID: lib.ID, Name: name, Path: rootDir + "/paged-" + strconv.Itoa(i),
		})
		if err != nil {
			t.Fatalf("UpsertSeriesByPath failed: %v", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE series SET library_id = ? WHERE id = ?`, lib.ID, series.ID); err != nil {
			t.Fatalf("update library failed: %v", err)
		}
		if err := store.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: series.ID, TagID: tag.ID}); err != nil {
			t.Fatalf("LinkSeriesTag failed: %v", err)
		}
	}
	// 首个夹带出来的系列没有标签，因此不参与命中，命中数恒为 n。
	res, err := db.ExecContext(ctx, `
		INSERT INTO smart_filters (library_id, name, active_tag, sort_by_field, sort_dir, page_size)
		VALUES (?, ?, ?, ?, ?, ?)
	`, lib.ID, "Action in A", "Action", "name", "asc", pageSize)
	if err != nil {
		t.Fatalf("insert smart filter failed: %v", err)
	}
	filterID, _ := res.LastInsertId()
	return controller, filterID
}

func fetchSmartCollectionPage(t *testing.T, controller *Controller, filterID int64, query string) SmartCollectionSeriesResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := requestWithRouteParam(http.MethodGet, "/api/collection-views/smart/1/series"+query, nil, "filterId", strconv.FormatInt(filterID, 10))
	controller.getSmartCollectionSeries(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected smart collection series 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload SmartCollectionSeriesResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode smart collection series failed: %v", err)
	}
	return payload
}

// TestSmartCollectionSeriesPagesBeyondConfiguredPageSize 守「命中数大于每页大小时，
// 剩下的成员仍取得到」：这正是 CountSmartCollectionSeries 的 doc 里要避免的
// 「书架显示 N 个系列，点进去只有 M 个」。
func TestSmartCollectionSeriesPagesBeyondConfiguredPageSize(t *testing.T) {
	const total = 5

	t.Run("不带参数时按书架配置的每页大小截断，但 total 报全量", func(t *testing.T) {
		controller, filterID := seedTaggedSmartCollection(t, total, 2)
		payload := fetchSmartCollectionPage(t, controller, filterID, "")
		if len(payload.Items) != 2 || payload.Total != total {
			t.Fatalf("expected 2 items of %d total, got %d items total=%d", total, len(payload.Items), payload.Total)
		}
		if payload.Limit != 2 || payload.Offset != 0 {
			t.Fatalf("expected echoed limit=2 offset=0, got limit=%d offset=%d", payload.Limit, payload.Offset)
		}
	})

	t.Run("带 offset 能取到超出第一页的剩余成员", func(t *testing.T) {
		controller, filterID := seedTaggedSmartCollection(t, total, 2)
		seen := map[int64]bool{}
		for offset := 0; offset < total; offset += 2 {
			payload := fetchSmartCollectionPage(t, controller, filterID, "?limit=2&offset="+strconv.Itoa(offset))
			if payload.Total != total {
				t.Fatalf("offset=%d: total 应恒为 %d，实际 %d", offset, total, payload.Total)
			}
			for _, item := range payload.Items {
				seen[item.ID] = true
			}
		}
		if len(seen) != total {
			t.Fatalf("翻完所有页只看到 %d 个成员，命中数是 %d —— 还是有成员永远看不到", len(seen), total)
		}
	})

	t.Run("limit 是端点自己的每页条数，不再被白名单吞回配置值", func(t *testing.T) {
		controller, filterID := seedTaggedSmartCollection(t, total, 2)
		payload := fetchSmartCollectionPage(t, controller, filterID, "?limit=3")
		if payload.Limit != 3 || len(payload.Items) != 3 {
			t.Fatalf("expected limit=3 honored, got limit=%d items=%d", payload.Limit, len(payload.Items))
		}
	})

	t.Run("limit 超过端点硬上限时夹到上限，不会被要求一次全灌", func(t *testing.T) {
		controller, filterID := seedTaggedSmartCollection(t, total, 2)
		payload := fetchSmartCollectionPage(t, controller, filterID, "?limit=100000")
		if payload.Limit != maxSmartCollectionPageLimit {
			t.Fatalf("expected limit clamped to %d, got %d", maxSmartCollectionPageLimit, payload.Limit)
		}
	})

	t.Run("配置的每页大小为 0 时退回默认值，不会返回空列表", func(t *testing.T) {
		controller, filterID := seedTaggedSmartCollection(t, total, 0)
		payload := fetchSmartCollectionPage(t, controller, filterID, "")
		if len(payload.Items) != total || payload.Limit != defaultSmartCollectionPageLimit {
			t.Fatalf("expected default page limit %d covering all %d members, got limit=%d items=%d",
				defaultSmartCollectionPageLimit, total, payload.Limit, len(payload.Items))
		}
	})
}
