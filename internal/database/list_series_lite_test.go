// 业务说明：本文件回归测试 ListSeriesByLibraryLite —— 扫描对账 / 批量刮削使用的“去封面子查询”投影，
// 必须与完整版 ListSeriesByLibrary 返回同一批系列的相同自身列，仅 CoverPath 不同（Lite 恒为零值）。

package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestListSeriesByLibraryLiteMatchesFullMinusCover(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name:         "Main",
		Path:         filepath.Join(t.TempDir(), "library"),
		ScanMode:     "none",
		ScanInterval: 60,
		ScanFormats:  "cbz",
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}

	alpha, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "Alpha",
		Path:        filepath.Join(lib.Path, "Alpha"),
		NameInitial: "A",
		Title:       sql.NullString{String: "Alpha Title", Valid: true},
	})
	if err != nil {
		t.Fatalf("create alpha failed: %v", err)
	}
	beta, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "Beta",
		Path:        filepath.Join(lib.Path, "Beta"),
		NameInitial: "B",
	})
	if err != nil {
		t.Fatalf("create beta failed: %v", err)
	}

	// 给 Alpha 一册带封面的书，使完整版的封面相关子查询有值可取。
	if _, err := store.CreateBook(ctx, CreateBookParams{
		SeriesID:       alpha.ID,
		LibraryID:      lib.ID,
		Name:           "Vol.01.cbz",
		Path:           filepath.Join(alpha.Path, "Vol.01.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		PageCount:      10,
		CoverPath:      sql.NullString{String: "covers/aa/alpha.webp", Valid: true},
	}); err != nil {
		t.Fatalf("create alpha book failed: %v", err)
	}

	full, err := store.ListSeriesByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ListSeriesByLibrary failed: %v", err)
	}
	lite, err := store.ListSeriesByLibraryLite(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ListSeriesByLibraryLite failed: %v", err)
	}

	if len(full) != 2 || len(lite) != 2 {
		t.Fatalf("expected 2 rows each, got full=%d lite=%d", len(full), len(lite))
	}

	// 以 ID 建索引比对（两个查询的排序可能不同：Lite 去掉了 ORDER BY）。
	// Lite 返回表模型 Series —— 它根本没有 CoverPath 字段，从类型层面即保证“不带封面”。
	liteByID := make(map[int64]Series, len(lite))
	for _, r := range lite {
		liteByID[r.ID] = r
	}
	for _, f := range full {
		l, ok := liteByID[f.ID]
		if !ok {
			t.Fatalf("lite missing series id=%d", f.ID)
		}
		// 除封面外的自身列必须完全一致。
		if l.LibraryID != f.LibraryID || l.Name != f.Name || l.Title != f.Title ||
			l.Summary != f.Summary || l.Publisher != f.Publisher || l.Status != f.Status ||
			l.Rating != f.Rating || l.Language != f.Language || l.LockedFields != f.LockedFields ||
			l.NameInitial != f.NameInitial || l.Path != f.Path ||
			l.IsFavorite != f.IsFavorite || l.VolumeCount != f.VolumeCount ||
			l.BookCount != f.BookCount || l.TotalPages != f.TotalPages {
			t.Fatalf("lite row diverges from full (id=%d)\nfull=%+v\nlite=%+v", f.ID, f, l)
		}
	}

	// 完整版应为 Alpha 取到封面（证明我们对比的是“去封面”而非“恰好都没有封面”）。
	var alphaCover string
	for _, f := range full {
		if f.ID == alpha.ID {
			alphaCover = f.CoverPath.String
		}
	}
	if alphaCover == "" {
		t.Fatal("full ListSeriesByLibrary should resolve Alpha's cover via the correlated subquery")
	}
	_ = beta
}
