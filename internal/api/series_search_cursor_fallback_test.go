// 守 /api/series/search 的游标降级契约：客户端手里的游标过期或无效时，这一页仍要出结果。
// 过期游标来自改排序、分享出去的旧链接与浏览器前进后退，是可预期的正常输入；
// 只有服务端自己出问题才该让整个请求失败并记 ERROR。

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

type seriesSearchBody struct {
	Items      []database.SearchSeriesPagedRow `json:"items"`
	Total      int                             `json:"total"`
	NextCursor string                          `json:"next_cursor"`
	HasMore    bool                            `json:"has_more"`
}

// failingCursorStore 让游标查询按「服务端自己坏了」的方式失败，其余调用照旧走真库。
type failingCursorStore struct {
	database.Store
}

func (failingCursorStore) SearchSeriesCursor(context.Context, int64, database.SeriesListFilters, int32, string, string) ([]database.SearchSeriesPagedRow, string, bool, error) {
	return nil, "", false, errors.New("disk on fire")
}

func seedCursorFallbackLibrary(t *testing.T) (*Controller, database.Library) {
	t.Helper()
	controller, store, _, rootDir := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 10)
	for _, name := range []string{"Series Beta", "Series Gamma"} {
		if _, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
			LibraryID:   lib.ID,
			Name:        name,
			Path:        filepath.Join(rootDir, "Library A", name),
			NameInitial: database.SeriesInitial("", name),
		}); err != nil {
			t.Fatalf("CreateSeries %s failed: %v", name, err)
		}
	}
	return controller, lib
}

// doSeriesSearch 打一次列表请求，并把这次请求写出的日志一并收回来——降级路径的判据里
// 有一半是「别再往运维日志里写 ERROR」。
func doSeriesSearch(t *testing.T, controller *Controller, query string) (int, seriesSearchBody, []slog.Record) {
	t.Helper()
	capture := &captureLogHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(previous)

	rec := httptest.NewRecorder()
	controller.searchSeriesPaged(rec, httptest.NewRequest(http.MethodGet, "/api/series/search?"+query, nil))
	var body seriesSearchBody
	if rec.Code == http.StatusOK {
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode %s failed: %v", query, err)
		}
	}
	return rec.Code, body, capture.records
}

func hasErrorLog(records []slog.Record) bool {
	for _, r := range records {
		if r.Level >= slog.LevelError {
			return true
		}
	}
	return false
}

func seriesNames(rows []database.SearchSeriesPagedRow) []string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names
}

// TestSearchSeriesStaleCursorFallsBackToOffset 覆盖游标不可用时的降级：忽略游标按 page 走 offset
// 分页，用户照常拿到这一页；服务端故障仍旧 500。
func TestSearchSeriesStaleCursorFallsBackToOffset(t *testing.T) {
	controller, lib := seedCursorFallbackLibrary(t)
	base := "libraryId=" + strconv.FormatInt(lib.ID, 10) + "&limit=1"

	// 先在 name_asc 下取得一个真游标，它只属于 name_asc。
	code, first, _ := doSeriesSearch(t, controller, base+"&page=1&sortBy=name_asc")
	if code != http.StatusOK || first.NextCursor == "" {
		t.Fatalf("seed cursor failed: code=%d body=%+v", code, first)
	}
	nameCursor := first.NextCursor

	t.Run("改排序后旧游标过期，这一页照样出结果", func(t *testing.T) {
		code, body, records := doSeriesSearch(t, controller, base+"&page=2&sortBy=created_asc&cursor="+url.QueryEscape(nameCursor))
		if code != http.StatusOK {
			t.Fatalf("want 200 got %d", code)
		}
		// 走了 offset 分页：带真实 total，内容是 created_asc 的第 2 页。
		if body.Total != 3 || len(body.Items) != 1 || body.Items[0].Name != "Series Beta" {
			t.Fatalf("want offset page 2 of created_asc, got total=%d names=%v", body.Total, seriesNames(body.Items))
		}
		if hasErrorLog(records) {
			t.Fatalf("过期游标不该记 ERROR: %+v", records)
		}
		// 降级顺带发回属于新排序的游标，客户端下一次翻页可以直接用。
		if body.NextCursor == "" {
			t.Fatal("want a fresh cursor for the new sort")
		}
		code, next, _ := doSeriesSearch(t, controller, base+"&page=3&sortBy=created_asc&cursor="+url.QueryEscape(body.NextCursor))
		if code != http.StatusOK || next.Total != 0 || len(next.Items) != 1 || next.Items[0].Name != "Series Gamma" {
			t.Fatalf("fresh cursor unusable: code=%d total=%d names=%v", code, next.Total, seriesNames(next.Items))
		}
	})

	t.Run("游标串本身坏掉也降级", func(t *testing.T) {
		badPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"sort_by":"name_asc","id":0}`))
		for _, tc := range []struct {
			name   string
			cursor string
		}{
			{"不是 base64", "!!!not-base64!!!"},
			{"base64 里不是 JSON", base64.RawURLEncoding.EncodeToString([]byte("hello"))},
			{"载荷里的 ID 非法", badPayload},
		} {
			t.Run(tc.name, func(t *testing.T) {
				code, body, records := doSeriesSearch(t, controller, base+"&page=1&sortBy=name_asc&cursor="+url.QueryEscape(tc.cursor))
				if code != http.StatusOK {
					t.Fatalf("want 200 got %d", code)
				}
				if body.Total != 3 || len(body.Items) != 1 || body.Items[0].Name != "Series Alpha" {
					t.Fatalf("want offset page 1, got total=%d names=%v", body.Total, seriesNames(body.Items))
				}
				if hasErrorLog(records) {
					t.Fatalf("无效游标不该记 ERROR: %+v", records)
				}
			})
		}
	})

	t.Run("排序本来就不支持游标时忽略游标", func(t *testing.T) {
		code, body, records := doSeriesSearch(t, controller, base+"&page=1&sortBy=rating_desc&cursor="+url.QueryEscape(nameCursor))
		if code != http.StatusOK || body.Total != 3 || len(body.Items) != 1 {
			t.Fatalf("want offset page 1, got code=%d total=%d names=%v", code, body.Total, seriesNames(body.Items))
		}
		if hasErrorLog(records) {
			t.Fatalf("不支持游标的排序不该记 ERROR: %+v", records)
		}
	})

	t.Run("正常的游标翻页不退化", func(t *testing.T) {
		code, body, _ := doSeriesSearch(t, controller, base+"&page=2&sortBy=name_asc&cursor="+url.QueryEscape(nameCursor))
		if code != http.StatusOK {
			t.Fatalf("want 200 got %d", code)
		}
		// 游标路径不做 COUNT，total 恒为 0；这正是它与降级后的 offset 路径的区别。
		if body.Total != 0 || len(body.Items) != 1 || body.Items[0].Name != "Series Beta" || !body.HasMore {
			t.Fatalf("want cursor page 2, got total=%d hasMore=%v names=%v", body.Total, body.HasMore, seriesNames(body.Items))
		}
	})

	t.Run("服务端自己出错仍然 500 并记 ERROR", func(t *testing.T) {
		broken, brokenLib := seedCursorFallbackLibrary(t)
		broken.store = failingCursorStore{Store: broken.store}
		code, _, records := doSeriesSearch(t, broken,
			"libraryId="+strconv.FormatInt(brokenLib.ID, 10)+"&limit=1&page=2&sortBy=name_asc&cursor="+url.QueryEscape(nameCursor))
		if code != http.StatusInternalServerError {
			t.Fatalf("want 500 got %d", code)
		}
		if !hasErrorLog(records) {
			t.Fatal("服务端故障必须记 ERROR")
		}
	})
}
