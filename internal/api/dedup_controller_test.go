// 业务说明：本文件是业务回归测试，覆盖重复书籍的识别与移除工作流。
// 此前该链路唯一相关的用例只验证了 403（角色边界），移除本身、回收站落盘与
// 移除后派生统计的重算全部没有覆盖——而这条链路会真的删用户的文件。

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/database"
)

// TestMoveFileToTrashRelocatesFile 覆盖同设备下的常规路径（os.Rename 成功）。
func TestMoveFileToTrashRelocatesFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "vol01.cbz")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write source failed: %v", err)
	}
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatalf("mkdir trash failed: %v", err)
	}

	if err := moveFileToTrash(src, trash, 42); err != nil {
		t.Fatalf("moveFileToTrash failed: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source should be gone after a successful move, stat err = %v", err)
	}
	// 目标名带 bookID 前缀，避免不同书的同名文件在回收站里互相覆盖。
	moved := filepath.Join(trash, "42-vol01.cbz")
	data, err := os.ReadFile(moved)
	if err != nil {
		t.Fatalf("moved file missing: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("moved file content mismatch: %q", data)
	}
}

func TestMoveFileToTrashFailsWhenSourceMissing(t *testing.T) {
	root := t.TempDir()
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatalf("mkdir trash failed: %v", err)
	}

	if err := moveFileToTrash(filepath.Join(root, "nope.cbz"), trash, 1); err == nil {
		t.Fatal("expected moving a missing file to fail")
	}
	// 失败时不得在回收站留下任何残留。
	entries, err := os.ReadDir(trash)
	if err != nil {
		t.Fatalf("read trash failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed move must not leave residue in trash, got %d entries", len(entries))
	}
}

// TestRemoveBooksRefreshesSeriesDerivedData 锁住「移除后必须重算派生数据」。
//
// 删除会改变系列组成，而 series.book_count/total_pages 与每用户的 user_series_progress
// 都是派生快照。旧实现只刷了 series_stats，于是进度条与「已完成」计数长期偏差。
func TestRemoveBooksRefreshesSeriesDerivedData(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	ctx := context.Background()
	_, series, first := seedBookFixture(t, store, rootDir, "Lib", "Series", "vol01.cbz", 10)
	second := seedExtraBook(t, store, rootDir, series.ID, first.LibraryID, "vol02.cbz", 20)

	if err := store.RefreshSeriesDerivedData(ctx, series.ID); err != nil {
		t.Fatalf("baseline refresh failed: %v", err)
	}
	before, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("get series failed: %v", err)
	}
	if before.BookCount != 2 {
		t.Fatalf("expected 2 books before removal, got %d", before.BookCount)
	}

	body, _ := json.Marshal(map[string]any{"book_ids": []int64{second.ID}})
	req := httptest.NewRequest(http.MethodPost, "/api/books/remove", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	controller.removeBooks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("removeBooks want 200 got %d: %s", rec.Code, rec.Body.String())
	}

	after, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("get series after removal failed: %v", err)
	}
	if after.BookCount != 1 {
		t.Fatalf("series.book_count must be recomputed after removal, got %d", after.BookCount)
	}
	if after.TotalPages != before.TotalPages-20 {
		t.Fatalf("series.total_pages must drop by the removed book's pages: before=%d after=%d",
			before.TotalPages, after.TotalPages)
	}
}

// seedExtraBook 往既有系列里再塞一本书，用于构造「移除其中一本」的场景。
func seedExtraBook(t *testing.T, store database.Store, rootDir string, seriesID, libraryID int64, name string, pageCount int64) database.Book {
	t.Helper()
	path := filepath.Join(rootDir, name)
	if err := os.WriteFile(path, []byte("archive"), 0o644); err != nil {
		t.Fatalf("write extra book failed: %v", err)
	}
	book, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID:       seriesID,
		LibraryID:      libraryID,
		Name:           name,
		Path:           path,
		Size:           7,
		FileModifiedAt: time.Now(),
		Title:          sql.NullString{String: name, Valid: true},
		PageCount:      pageCount,
	})
	if err != nil {
		t.Fatalf("create extra book failed: %v", err)
	}
	return book
}
