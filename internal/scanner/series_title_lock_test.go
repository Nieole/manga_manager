// 守扫描与系列 title / locked_fields 的边界：locked_fields 是用户的开关，扫描只读不写；
// 系列 title 有值就原样保留，空 title 才用目录名兜底，name_initial 跟着最终标题走。

package scanner

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

const seriesLockTestComicInfo = `<?xml version="1.0" encoding="utf-8"?>
<ComicInfo>
  <Series>One Piece</Series>
  <Summary>归档内嵌的简介</Summary>
  <Publisher>Shueisha</Publisher>
  <LanguageISO>ja</LanguageISO>
</ComicInfo>`

// newSeriesLockScanner 铺一个带 ComicInfo.xml 的系列目录，返回可直接扫的扫描器。
func newSeriesLockScanner(t *testing.T) (*Scanner, database.Store, database.Library, string, string) {
	t.Helper()
	rootDir, store, lib, libraryPath := newScannerTestLibrary(t)
	seriesPath := filepath.Join(libraryPath, "One Piece")
	if err := os.MkdirAll(seriesPath, 0o755); err != nil {
		t.Fatalf("mkdir series failed: %v", err)
	}
	if err := writeScannerTestCBZ(filepath.Join(seriesPath, "One Piece 01.cbz"), map[string][]byte{
		"001.png":       testPNG1x1,
		"ComicInfo.xml": []byte(seriesLockTestComicInfo),
	}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(rootDir, "thumbs")
	return NewScanner(store, config.NewManager(cfg)), store, lib, libraryPath, seriesPath
}

// seriesUnderTest 取该库里唯一的系列。
func seriesUnderTest(t *testing.T, store database.Store, libraryID int64) database.Series {
	t.Helper()
	list, err := store.ListSeriesByLibraryLite(context.Background(), libraryID)
	if err != nil {
		t.Fatalf("list series failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one series, got %d", len(list))
	}
	return list[0]
}

// TestScanKeepsUserEditedSeriesTitleAndLocks 钉住「谁有权改 title 与 locked_fields」。
//
// 归档里的 ComicInfo.xml 会让扫描走系列增补分支。扫描对 title 唯一能给出的值是目录名，
// 而目录名已原样存在 name 列里；locked_fields 则是用户在详情页开关的东西。扫描既不该
// 把用户改过的标题打回目录名，也不该把用户主动解开的 title 锁重新锁上——锁一旦被扫描
// 锁死，proposal.applyMetadata 的 !locked["title"] 永不成立，刮削从此写不进标题。
func TestScanKeepsUserEditedSeriesTitleAndLocks(t *testing.T) {
	s, store, lib, libraryPath, _ := newSeriesLockScanner(t)
	ctx := context.Background()

	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("first scan failed: %v", err)
	}
	waitForScannerCoverQueue(t, s)
	first := seriesUnderTest(t, store, lib.ID)

	t.Run("新系列首扫用目录名作默认标题", func(t *testing.T) {
		if first.Title.String != "One Piece" {
			t.Fatalf("expected default title from directory name, got %q", first.Title.String)
		}
		if first.NameInitial != "O" {
			t.Fatalf("expected name_initial O, got %q", first.NameInitial)
		}
	})

	// 用户在系列详情页改标题、写简介，并把 title 解锁好让刮削能写进来（只锁 summary）。
	if _, err := store.UpdateSeriesMetadata(ctx, database.UpdateSeriesMetadataParams{
		ID:           first.ID,
		Title:        sql.NullString{String: "海贼王", Valid: true},
		Summary:      sql.NullString{String: "用户写的简介", Valid: true},
		Publisher:    first.Publisher,
		Status:       first.Status,
		Rating:       first.Rating,
		Language:     first.Language,
		LockedFields: sql.NullString{String: "summary", Valid: true},
		NameInitial:  database.SeriesInitial("海贼王", first.Name),
	}); err != nil {
		t.Fatalf("user edit failed: %v", err)
	}

	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("second scan failed: %v", err)
	}
	waitForScannerCoverQueue(t, s)
	second := seriesUnderTest(t, store, lib.ID)

	t.Run("用户改过的标题不被目录名覆盖", func(t *testing.T) {
		if second.Title.String != "海贼王" {
			t.Fatalf("expected user title kept, got %q", second.Title.String)
		}
	})

	t.Run("用户解锁的 title 不被重新锁上", func(t *testing.T) {
		if second.LockedFields.String != "summary" {
			t.Fatalf("expected locked_fields untouched by scan, got %q", second.LockedFields.String)
		}
	})

	t.Run("name_initial 跟着用户标题走", func(t *testing.T) {
		if second.NameInitial != "H" {
			t.Fatalf("expected name_initial from 海贼王, got %q", second.NameInitial)
		}
	})

	t.Run("锁住的字段仍挡住归档元数据", func(t *testing.T) {
		if second.Summary.String != "用户写的简介" {
			t.Fatalf("expected locked summary kept, got %q", second.Summary.String)
		}
	})
}

// TestScanKeepsSeriesLocksWhenAddingVolume 钉住新系列多卷入库时的锁：
// 系列在本次扫描里刚被建出来，同批后续卷不得把建库时给的默认锁与标题冲掉。
func TestScanKeepsSeriesLocksWhenAddingVolume(t *testing.T) {
	s, store, lib, libraryPath, seriesPath := newSeriesLockScanner(t)
	ctx := context.Background()

	if err := writeScannerTestCBZ(filepath.Join(seriesPath, "One Piece 02.cbz"), map[string][]byte{
		"001.png":       testPNG1x1,
		"ComicInfo.xml": []byte(seriesLockTestComicInfo),
	}); err != nil {
		t.Fatalf("write second cbz failed: %v", err)
	}
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	waitForScannerCoverQueue(t, s)

	series := seriesUnderTest(t, store, lib.ID)
	if series.Title.String != "One Piece" {
		t.Fatalf("expected default title from directory name, got %q", series.Title.String)
	}
	if series.LockedFields.String != "title" {
		t.Fatalf("expected creation-time default lock kept, got %q", series.LockedFields.String)
	}
}
