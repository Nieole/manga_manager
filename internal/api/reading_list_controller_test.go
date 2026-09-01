// 本文件是业务回归测试，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"manga-manager/internal/database"
)

func TestReadingListLifecycle(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	_, seriesB, _ := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)

	createRec := httptest.NewRecorder()
	controller.createReadingList(createRec, httptest.NewRequest(http.MethodPost, "/api/reading-lists/", jsonBody(`{"name":"Cosmic Order","description":"main + side stories"}`)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created database.ReadingList
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created reading list failed: %v", err)
	}
	if created.Name != "Cosmic Order" {
		t.Fatalf("unexpected created reading list: %+v", created)
	}

	addARec := httptest.NewRecorder()
	controller.addReadingListItem(addARec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":`+strconv.FormatInt(seriesA.ID, 10)+`,"note":"start here"}`), "listId", strconv.FormatInt(created.ID, 10)))
	if addARec.Code != http.StatusOK {
		t.Fatalf("expected add A 200, got %d body=%s", addARec.Code, addARec.Body.String())
	}
	var itemA database.ReadingListItem
	if err := json.NewDecoder(addARec.Body).Decode(&itemA); err != nil {
		t.Fatalf("decode item A failed: %v", err)
	}

	addBRec := httptest.NewRecorder()
	controller.addReadingListItem(addBRec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":`+strconv.FormatInt(seriesB.ID, 10)+`}`), "listId", strconv.FormatInt(created.ID, 10)))
	if addBRec.Code != http.StatusOK {
		t.Fatalf("expected add B 200, got %d body=%s", addBRec.Code, addBRec.Body.String())
	}
	var itemB database.ReadingListItem
	if err := json.NewDecoder(addBRec.Body).Decode(&itemB); err != nil {
		t.Fatalf("decode item B failed: %v", err)
	}

	reorderRec := httptest.NewRecorder()
	reorderBody := []byte(`{"item_ids":[` + strconv.FormatInt(itemB.ID, 10) + `,` + strconv.FormatInt(itemA.ID, 10) + `]}`)
	controller.reorderReadingListItems(reorderRec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items/reorder", reorderBody, "listId", strconv.FormatInt(created.ID, 10)))
	if reorderRec.Code != http.StatusOK {
		t.Fatalf("expected reorder 200, got %d body=%s", reorderRec.Code, reorderRec.Body.String())
	}

	listItemsRec := httptest.NewRecorder()
	controller.listReadingListItems(listItemsRec, requestWithRouteParam(http.MethodGet, "/api/reading-lists/1/items", nil, "listId", strconv.FormatInt(created.ID, 10)))
	if listItemsRec.Code != http.StatusOK {
		t.Fatalf("expected list items 200, got %d body=%s", listItemsRec.Code, listItemsRec.Body.String())
	}
	var items []database.ListReadingListItemsRow
	if err := json.NewDecoder(listItemsRec.Body).Decode(&items); err != nil {
		t.Fatalf("decode list items failed: %v", err)
	}
	if len(items) != 2 || items[0].SeriesID != seriesB.ID || items[1].SeriesID != seriesA.ID {
		t.Fatalf("unexpected item order: %+v", items)
	}
	if items[1].NextBookID <= 0 || items[1].Note != "start here" {
		t.Fatalf("unexpected item details: %+v", items[1])
	}

	removeRec := httptest.NewRecorder()
	controller.removeReadingListItem(removeRec, requestWithRouteParams(http.MethodDelete, "/api/reading-lists/1/items/1", nil, map[string]string{
		"listId": strconv.FormatInt(created.ID, 10),
		"itemId": strconv.FormatInt(itemB.ID, 10),
	}))
	if removeRec.Code != http.StatusOK {
		t.Fatalf("expected remove 200, got %d body=%s", removeRec.Code, removeRec.Body.String())
	}

	listRec := httptest.NewRecorder()
	controller.listReadingLists(listRec, httptest.NewRequest(http.MethodGet, "/api/reading-lists/", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}
	var lists []database.ListReadingListsRow
	if err := json.NewDecoder(listRec.Body).Decode(&lists); err != nil {
		t.Fatalf("decode reading lists failed: %v", err)
	}
	if len(lists) != 1 || lists[0].ItemCount != 1 {
		t.Fatalf("expected one list with one item, got %+v", lists)
	}

	deleteRec := httptest.NewRecorder()
	controller.deleteReadingList(deleteRec, requestWithRouteParam(http.MethodDelete, "/api/reading-lists/1", nil, "listId", strconv.FormatInt(created.ID, 10)))
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	remaining, err := store.ListReadingLists(context.Background())
	if err != nil {
		t.Fatalf("ListReadingLists failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected no reading lists after delete, got %+v", remaining)
	}
}

func TestReadingListValidation(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	list, err := store.CreateReadingList(context.Background(), database.CreateReadingListParams{Name: "Order", Description: ""})
	if err != nil {
		t.Fatalf("CreateReadingList failed: %v", err)
	}

	t.Run("create requires name", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.createReadingList(rec, httptest.NewRequest(http.MethodPost, "/api/reading-lists/", jsonBody(`{"name":" "}`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected create validation 400, got %d", rec.Code)
		}
	})

	t.Run("route ids are validated", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.listReadingListItems(rec, requestWithRouteParam(http.MethodGet, "/api/reading-lists/bad/items", nil, "listId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected bad list id 400, got %d", rec.Code)
		}
	})

	t.Run("add requires existing series", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.addReadingListItem(rec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":999}`), "listId", strconv.FormatInt(list.ID, 10)))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected missing series 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("readding updates note instead of duplicating", func(t *testing.T) {
		firstRec := httptest.NewRecorder()
		controller.addReadingListItem(firstRec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":`+strconv.FormatInt(series.ID, 10)+`,"note":"old"}`), "listId", strconv.FormatInt(list.ID, 10)))
		if firstRec.Code != http.StatusOK {
			t.Fatalf("expected first add 200, got %d", firstRec.Code)
		}
		secondRec := httptest.NewRecorder()
		controller.addReadingListItem(secondRec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":`+strconv.FormatInt(series.ID, 10)+`,"note":"new"}`), "listId", strconv.FormatInt(list.ID, 10)))
		if secondRec.Code != http.StatusOK {
			t.Fatalf("expected second add 200, got %d", secondRec.Code)
		}
		items, err := store.ListReadingListItems(context.Background(), list.ID)
		if err != nil {
			t.Fatalf("ListReadingListItems failed: %v", err)
		}
		if len(items) != 1 || items[0].Note != "new" {
			t.Fatalf("expected updated single item, got %+v", items)
		}
	})
}

func jsonBody(raw string) *strings.Reader {
	return strings.NewReader(raw)
}

// TestReadingListMissingTargetReturnsNotFound 锁定「目标不存在」在增/删/排三个入口的同一口径：
// 一律 404，而不是 500（把客户端传错的 id 当成服务端故障）或 200（谎称改动已保存）。
func TestReadingListMissingTargetReturnsNotFound(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	list, err := store.CreateReadingList(context.Background(), database.CreateReadingListParams{Name: "Order", Description: ""})
	if err != nil {
		t.Fatalf("CreateReadingList failed: %v", err)
	}
	other, err := store.CreateReadingList(context.Background(), database.CreateReadingListParams{Name: "Other", Description: ""})
	if err != nil {
		t.Fatalf("CreateReadingList other failed: %v", err)
	}
	otherItem, err := store.AddReadingListItem(context.Background(), database.AddReadingListItemParams{
		ReadingListID: other.ID,
		SeriesID:      series.ID,
		Note:          "",
	})
	if err != nil {
		t.Fatalf("AddReadingListItem failed: %v", err)
	}

	t.Run("清单不存在时新增条目返回 404 而不是外键兜底的 500", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.addReadingListItem(rec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/999999/items", []byte(`{"series_id":`+strconv.FormatInt(series.ID, 10)+`}`), "listId", "999999"))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected missing list 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("删除不存在的条目返回 404 而不是谎称删除成功", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.removeReadingListItem(rec, requestWithRouteParams(http.MethodDelete, "/api/reading-lists/1/items/999999", nil, map[string]string{
			"listId": strconv.FormatInt(list.ID, 10),
			"itemId": "999999",
		}))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected missing item 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("删除别的清单的条目返回 404 且不误删", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.removeReadingListItem(rec, requestWithRouteParams(http.MethodDelete, "/api/reading-lists/1/items/1", nil, map[string]string{
			"listId": strconv.FormatInt(list.ID, 10),
			"itemId": strconv.FormatInt(otherItem.ID, 10),
		}))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected cross-list remove 404, got %d body=%s", rec.Code, rec.Body.String())
		}
		remaining, err := store.ListReadingListItems(context.Background(), other.ID)
		if err != nil {
			t.Fatalf("ListReadingListItems failed: %v", err)
		}
		if len(remaining) != 1 {
			t.Fatalf("expected other list untouched, got %+v", remaining)
		}
	})

	t.Run("重排混进不属于本清单的条目返回 404 且整批回滚", func(t *testing.T) {
		mine, err := store.AddReadingListItem(context.Background(), database.AddReadingListItemParams{
			ReadingListID: list.ID,
			SeriesID:      series.ID,
			Note:          "",
		})
		if err != nil {
			t.Fatalf("AddReadingListItem mine failed: %v", err)
		}
		before := readingListItemSortOrder(t, store, list.ID, mine.ID)
		rec := httptest.NewRecorder()
		body := []byte(`{"item_ids":[` + strconv.FormatInt(otherItem.ID, 10) + `,` + strconv.FormatInt(mine.ID, 10) + `]}`)
		controller.reorderReadingListItems(rec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items/reorder", body, "listId", strconv.FormatInt(list.ID, 10)))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected foreign item 404, got %d body=%s", rec.Code, rec.Body.String())
		}
		if after := readingListItemSortOrder(t, store, list.ID, mine.ID); after != before {
			t.Fatalf("expected rollback to keep sort_order %d, got %d", before, after)
		}
	})
}

// TestReadingListNormalItemFlowStillWorks 是上面那组 404 口径的反向判据：合法的增、删、
// 以及只覆盖清单一部分条目的重排（并发新增会让前端手里的快照少一条）都必须照常成功。
func TestReadingListNormalItemFlowStillWorks(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	_, seriesB, _ := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)
	_, seriesC, _ := seedBookFixture(t, store, rootDir, "Library C", "Series Gamma", "Gamma 01.cbz", 8)
	list, err := store.CreateReadingList(context.Background(), database.CreateReadingListParams{Name: "Order", Description: ""})
	if err != nil {
		t.Fatalf("CreateReadingList failed: %v", err)
	}

	added := make([]database.ReadingListItem, 0, 3)
	for _, series := range []database.Series{seriesA, seriesB, seriesC} {
		rec := httptest.NewRecorder()
		controller.addReadingListItem(rec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items", []byte(`{"series_id":`+strconv.FormatInt(series.ID, 10)+`}`), "listId", strconv.FormatInt(list.ID, 10)))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected add 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var item database.ReadingListItem
		if err := json.NewDecoder(rec.Body).Decode(&item); err != nil {
			t.Fatalf("decode added item failed: %v", err)
		}
		added = append(added, item)
	}

	t.Run("只重排前两条时未送到的条目保留原序", func(t *testing.T) {
		untouched := readingListItemSortOrder(t, store, list.ID, added[2].ID)
		rec := httptest.NewRecorder()
		body := []byte(`{"item_ids":[` + strconv.FormatInt(added[1].ID, 10) + `,` + strconv.FormatInt(added[0].ID, 10) + `]}`)
		controller.reorderReadingListItems(rec, requestWithRouteParam(http.MethodPost, "/api/reading-lists/1/items/reorder", body, "listId", strconv.FormatInt(list.ID, 10)))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected partial reorder 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		if got := readingListItemSortOrder(t, store, list.ID, added[1].ID); got != 10 {
			t.Fatalf("expected first submitted item sort_order 10, got %d", got)
		}
		if got := readingListItemSortOrder(t, store, list.ID, added[2].ID); got != untouched {
			t.Fatalf("expected untouched item to keep sort_order %d, got %d", untouched, got)
		}
	})

	t.Run("删除本清单里真实存在的条目仍返回 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.removeReadingListItem(rec, requestWithRouteParams(http.MethodDelete, "/api/reading-lists/1/items/1", nil, map[string]string{
			"listId": strconv.FormatInt(list.ID, 10),
			"itemId": strconv.FormatInt(added[0].ID, 10),
		}))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected remove 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		items, err := store.ListReadingListItems(context.Background(), list.ID)
		if err != nil {
			t.Fatalf("ListReadingListItems failed: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected two remaining items, got %+v", items)
		}
	})
}

// readingListItemSortOrder 取一条阅读列表条目当前的 sort_order，供重排回滚断言比对。
func readingListItemSortOrder(t *testing.T, store database.Store, listID, itemID int64) int64 {
	t.Helper()
	items, err := store.ListReadingListItems(context.Background(), listID)
	if err != nil {
		t.Fatalf("ListReadingListItems failed: %v", err)
	}
	for _, item := range items {
		if item.ID == itemID {
			return item.SortOrder
		}
	}
	t.Fatalf("reading list item %d not found in list %d", itemID, listID)
	return 0
}
