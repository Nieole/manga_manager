// 守「合集页左栏能区分各个智能书架」：列表响应必须机器可读地带上筛选条件与视图身份，
// 前端才渲染得出筛选芯片；只在 description 里拼一串原始文本，界面上一个条件也显示不出来。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/database"
)

// TestCollectionViewsCarrySmartFilterConditions 覆盖左栏列表项的形状：
// 智能书架带筛选定义，手工合集不带，两者都带视图身份。
func TestCollectionViewsCarrySmartFilterConditions(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	db := controller.store.(*database.SqlStore).DB()
	res, err := db.ExecContext(context.Background(), `INSERT INTO collections (name, description) VALUES (?, ?)`, "手工精选", "静态")
	if err != nil {
		t.Fatalf("insert collection failed: %v", err)
	}
	collectionID, _ := res.LastInsertId()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO collection_series (collection_id, series_id) VALUES (?, ?)`, collectionID, seriesA.ID); err != nil {
		t.Fatalf("insert collection_series failed: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO smart_filters (library_id, name, active_tag, active_author, active_status, active_letter,
			read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
			sort_by_field, sort_dir, page_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, lib.ID, "高分动作在读", "Action", "ONE", "ongoing", "A", "reading", 8.0, 10.0, 20.0, 80.0, 30, "rating", "desc", 30); err != nil {
		t.Fatalf("insert smart filter failed: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.listCollectionViews(rec, httptest.NewRequest(http.MethodGet, "/api/collection-views", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	// 按前端实际收到的 JSON 断言，而不是解回 Go 结构体——芯片读的是 JSON 字段名。
	var raw []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode collection views failed: %v", err)
	}
	byKind := map[string]map[string]any{}
	for _, item := range raw {
		kind, _ := item["kind"].(string)
		byKind[kind] = item
	}
	if len(byKind) != 2 {
		t.Fatalf("expected one static and one smart view, got %s", rec.Body.String())
	}

	t.Run("智能书架带上完整的筛选条件", func(t *testing.T) {
		filter, ok := byKind["smart"]["smart_filter"].(map[string]any)
		if !ok {
			t.Fatalf("智能书架没带筛选条件，左栏无从渲染芯片: %s", rec.Body.String())
		}
		for key, want := range map[string]any{
			"activeTag":       "Action",
			"activeAuthor":    "ONE",
			"activeStatus":    "ongoing",
			"activeLetter":    "A",
			"readState":       "reading",
			"minRating":       8.0,
			"maxRating":       10.0,
			"minProgress":     20.0,
			"maxProgress":     80.0,
			"addedWithinDays": 30.0,
			"sortByField":     "rating",
			"sortDir":         "desc",
		} {
			if got := filter[key]; got != want {
				t.Errorf("filter[%q] = %#v, want %#v", key, got, want)
			}
		}
	})

	t.Run("手工合集不带筛选条件", func(t *testing.T) {
		if _, exists := byKind["collection"]["smart_filter"]; exists {
			t.Fatalf("手工合集不该带智能书架的筛选条件: %#v", byKind["collection"])
		}
	})

	t.Run("两类视图都带上左栏用来认选中项的 view_id", func(t *testing.T) {
		for kind, item := range byKind {
			if got, _ := item["view_id"].(string); got == "" {
				t.Errorf("%s 视图缺 view_id，左栏认不出选中的是哪一个: %#v", kind, item)
			}
		}
	})
}

// TestCollectionViewsOmitSmartFilterConditionsWhenUnset 覆盖「一个条件都没设」的智能书架：
// 筛选定义仍要在（否则前端分不清「没有条件」与「没拿到数据」），但各条件为空。
func TestCollectionViewsOmitSmartFilterConditionsWhenUnset(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	if _, err := controller.store.(*database.SqlStore).DB().ExecContext(context.Background(), `
		INSERT INTO smart_filters (library_id, name, sort_by_field, sort_dir, page_size)
		VALUES (?, ?, ?, ?, ?)
	`, lib.ID, "全部", "name", "asc", 30); err != nil {
		t.Fatalf("insert smart filter failed: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.listCollectionViews(rec, httptest.NewRequest(http.MethodGet, "/api/collection-views", nil))
	var raw []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode collection views failed: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected exactly one smart view, got %s", rec.Body.String())
	}
	filter, ok := raw[0]["smart_filter"].(map[string]any)
	if !ok {
		t.Fatalf("无条件的智能书架也必须带筛选定义: %s", rec.Body.String())
	}
	for _, key := range []string{"activeTag", "activeAuthor", "activeStatus", "activeLetter", "readState", "minRating", "maxRating", "minProgress", "maxProgress", "addedWithinDays"} {
		if got := filter[key]; got != nil {
			t.Errorf("filter[%q] = %#v, want null", key, got)
		}
	}
}
