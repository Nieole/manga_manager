// 本文件是业务回归测试，属于 SQLite 数据访问层，负责把漫画库、系列、阅读进度、任务和元数据状态持久化为稳定数据模型。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
)

func TestGetHealthReport(t *testing.T) {
	store := newHealthTestStore(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name:                "Library",
		Path:                filepath.Join(t.TempDir(), "library"),
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         "cbz,cbr",
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}
	series, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID:    lib.ID,
		Name:         "Series A",
		Path:         filepath.Join(lib.Path, "Series A"),
		LockedFields: sql.NullString{String: "[]", Valid: true},
		NameInitial:  "S",
	})
	if err != nil {
		t.Fatalf("create series failed: %v", err)
	}
	book, err := store.CreateBook(ctx, CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "Book A.cbz",
		Path:           filepath.Join(series.Path, "Book A.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		PageCount:      0,
	})
	if err != nil {
		t.Fatalf("create book failed: %v", err)
	}
	_, err = store.CreateBook(ctx, CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "Book B.cbz",
		Path:           filepath.Join(series.Path, "Book B.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		PageCount:      120,
	})
	if err != nil {
		t.Fatalf("create second book failed: %v", err)
	}
	if _, err := store.DB().Exec(`UPDATE books SET file_hash = 'dup' WHERE series_id = ?`, series.ID); err != nil {
		t.Fatalf("update duplicate hashes failed: %v", err)
	}
	if _, err := store.DB().Exec(`UPDATE books SET quick_hash = 'qdup' WHERE series_id = ?`, series.ID); err != nil {
		t.Fatalf("update duplicate quick hashes failed: %v", err)
	}
	if _, err := store.DB().Exec(`UPDATE books SET cover_path = 'cover.webp' WHERE id = ?`, book.ID); err != nil {
		t.Fatalf("update cover path failed: %v", err)
	}
	if _, err := store.DB().Exec(`INSERT INTO koreader_progress (username, document, progress, percentage, device, device_id, book_id) VALUES ('reader', 'missing.cbz', '{}', 0.5, 'device', 'id', NULL)`); err != nil {
		t.Fatalf("insert unmatched progress failed: %v", err)
	}

	report, err := store.GetHealthReport(ctx, HealthIssueFilters{LibraryID: lib.ID, Limit: 10})
	if err != nil {
		t.Fatalf("get health report failed: %v", err)
	}
	summary := healthSummaryMap(report.Summary)
	if summary["empty_pages"] != 1 {
		t.Fatalf("expected one empty page book, got %+v", summary)
	}
	if summary["missing_cover"] != 1 {
		t.Fatalf("expected one missing cover book, got %+v", summary)
	}
	if summary["missing_metadata"] != 1 {
		t.Fatalf("expected one missing metadata series, got %+v", summary)
	}
	if summary["duplicate_file_hash"] != 2 {
		t.Fatalf("expected two duplicate hash book entries, got %+v", summary)
	}
	if summary["duplicate_quick_hash"] != 2 {
		t.Fatalf("expected two duplicate quick hash book entries, got %+v", summary)
	}
	if summary["unmatched_koreader"] != 1 {
		t.Fatalf("expected one unmatched koreader item, got %+v", summary)
	}

	filtered, err := store.GetHealthReport(ctx, HealthIssueFilters{LibraryID: lib.ID, Type: "missing_cover", Limit: 10})
	if err != nil {
		t.Fatalf("get filtered health report failed: %v", err)
	}
	if len(filtered.Summary) != 1 || filtered.Summary[0].Type != "missing_cover" {
		t.Fatalf("expected only missing_cover summary, got %+v", filtered.Summary)
	}
	if len(filtered.Issues) != 1 || filtered.Issues[0].BookID == nil {
		t.Fatalf("expected one missing cover book issue, got %+v", filtered.Issues)
	}
}

func newHealthTestStore(t *testing.T) *SqlStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "health.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store.(*SqlStore)
}

func healthSummaryMap(items []HealthIssueSummary) map[string]int64 {
	result := make(map[string]int64, len(items))
	for _, item := range items {
		result[item.Type] = item.Count
	}
	return result
}

// TestAttachLastTaskKeysReadsRunsTable 守健康报告那个「查看日志」按钮的数据来源确实是运行表。
//
// 破了意味着旧任务表被丢弃之后按钮一律不出现（或者整个 /api/health/report 直接报错）。
// 系列优先于资料库：一条问题同时落在两者上时，用户想看的是那个系列自己的那次运行。
func TestAttachLastTaskKeysReadsRunsTable(t *testing.T) {
	store := newHealthTestStore(t)
	ctx := context.Background()

	runs := taskstore.New(store.db)
	seed := func(identity task.Identity, key string, sequence int64) {
		t.Helper()
		owner, err := runs.EnsureTask(ctx, identity)
		if err != nil {
			t.Fatalf("建身份失败: %v", err)
		}
		finishedAt := time.Now()
		if _, err := runs.CreateRun(ctx, task.Run{
			TaskID: owner.ID, Key: key, Trigger: task.TriggerManual, NthRun: int(sequence),
			Status: task.StatusCompleted, StartedAt: finishedAt.Add(-time.Minute),
			UpdatedAt: finishedAt, FinishedAt: &finishedAt, Sequence: sequence,
		}); err != nil {
			t.Fatalf("落一条运行失败: %v", err)
		}
	}
	seed(task.Identity{Type: "scan_library", Scope: task.ScopeLibrary, ScopeID: 7}, "scan_library_7", 1)
	seed(task.Identity{Type: "scrape_series", Scope: task.ScopeSeries, ScopeID: 42}, "scrape_series_42", 2)

	seriesID := int64(42)
	issues := []HealthIssue{
		{Type: "empty_pages", LibraryID: 7},
		{Type: "missing_metadata", LibraryID: 7, SeriesID: &seriesID},
		{Type: "empty_pages", LibraryID: 8},
	}
	if err := store.attachLastTaskKeys(ctx, issues); err != nil {
		t.Fatalf("挂任务键失败: %v", err)
	}
	if issues[0].LastTaskKey != "scan_library_7" {
		t.Errorf("库级问题拿到 %q，想要 scan_library_7", issues[0].LastTaskKey)
	}
	if issues[1].LastTaskKey != "scrape_series_42" {
		t.Errorf("系列级问题拿到 %q：系列自己的运行该优先于它所在的库", issues[1].LastTaskKey)
	}
	if issues[2].LastTaskKey != "" {
		t.Errorf("没跑过任何任务的库拿到了 %q", issues[2].LastTaskKey)
	}
}
