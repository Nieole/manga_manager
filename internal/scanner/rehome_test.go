// 业务说明：本文件是「文件改名后阅读进度不丢」这一业务承诺的回归测试。
//
// 用户在文件管理器里把一卷漫画改个名，是再普通不过的操作；但扫描器以 books.path 作为唯一身份，
// 改名会被识别成「旧书消失 + 新书出现」——旧行随后被 CleanupLibrary（watcher 在任何删除/改名后
// 自动触发）连同 user_book_progress、书签、合集归属一起 CASCADE 删掉。这里的用例断言的是
// **书籍主键在改名前后保持不变**，因为所有关联数据都挂在那个 id 上。

package scanner

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

// scanOnceForRehome 跑一次非强制的整库扫描。
func scanOnceForRehome(t *testing.T, s *Scanner, lib database.Library) {
	t.Helper()
	if err := s.ScanLibrary(context.Background(), lib.ID, lib.Path, false); err != nil {
		t.Fatalf("scan library failed: %v", err)
	}
}

func newRehomeScanner(t *testing.T, store database.Store) *Scanner {
	t.Helper()
	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	cfg.Cache.Dir = t.TempDir()
	return NewScanner(store, config.NewManager(cfg))
}

// TestRenamedBookKeepsIdentityAndProgress 是本批次的核心回归：
// 改名一本已读过的书，重新扫描后必须仍是同一行（同一 id），进度与每用户进度都还在。
func TestRenamedBookKeepsIdentityAndProgress(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()

	seriesDir := filepath.Join(libraryPath, "Series Alpha")
	oldPath := filepath.Join(seriesDir, "Vol 01.cbz")
	if err := writeScannerTestCBZ(oldPath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	s := newRehomeScanner(t, store)
	scanOnceForRehome(t, s, lib)

	books, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("expected 1 book after first scan, got %d", len(books))
	}
	originalID := books[0].ID

	// 记录阅读进度：全局列 + 每用户表，两者都挂在 books.id 上。
	if err := store.UpdateBookProgress(ctx, database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 7, Valid: true},
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
		ID:           originalID,
	}); err != nil {
		t.Fatalf("update book progress failed: %v", err)
	}
	user, err := store.CreateUser(ctx, database.CreateUserParams{
		Username: "reader", PasswordHash: "x", Role: "regular", DisplayName: "Reader",
	})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := store.SetUserBookProgress(ctx, user.ID, originalID, 7, time.Now()); err != nil {
		t.Fatalf("set user book progress failed: %v", err)
	}

	// 用户在文件管理器里改名。os.Rename 保留 mtime 与 size——这正是重连所依赖的身份信号。
	newPath := filepath.Join(seriesDir, "Volume 001.cbz")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	scanOnceForRehome(t, s, lib)

	books, err = store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	if len(books) != 1 {
		paths := make([]string, 0, len(books))
		for _, b := range books {
			paths = append(paths, b.Path)
		}
		t.Fatalf("expected rename to reuse the existing row, got %d books: %v", len(books), paths)
	}
	if books[0].ID != originalID {
		t.Fatalf("book id changed on rename: %d -> %d (关联的进度/书签/合集归属会随旧行一起 CASCADE 删除)",
			originalID, books[0].ID)
	}
	if books[0].Path != newPath {
		t.Fatalf("path = %q, want %q", books[0].Path, newPath)
	}

	book, err := store.GetBook(ctx, originalID)
	if err != nil {
		t.Fatalf("get book failed: %v", err)
	}
	if !book.LastReadPage.Valid || book.LastReadPage.Int64 != 7 {
		t.Fatalf("last_read_page = %+v, want 7", book.LastReadPage)
	}

	progress, ok, err := store.GetUserBookProgress(ctx, user.ID, originalID)
	if err != nil {
		t.Fatalf("get user book progress failed: %v", err)
	}
	if !ok || !progress.LastReadPage.Valid || progress.LastReadPage.Int64 != 7 {
		t.Fatalf("user progress = %+v (ok=%v), want page 7", progress, ok)
	}
}

// TestRenamedBookSurvivesCleanup 把链路走完整：改名 + 重扫之后再跑一次 CleanupLibrary
// （watcher 在真实环境里就是这么触发的），书必须还在。
func TestRenamedBookSurvivesCleanup(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()

	seriesDir := filepath.Join(libraryPath, "Series Alpha")
	oldPath := filepath.Join(seriesDir, "Vol 01.cbz")
	if err := writeScannerTestCBZ(oldPath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	s := newRehomeScanner(t, store)
	scanOnceForRehome(t, s, lib)
	waitForScannerCoverQueue(t, s)

	books, _ := store.ListBooksByLibrary(ctx, lib.ID)
	if len(books) != 1 {
		t.Fatalf("expected 1 book, got %d", len(books))
	}
	originalID := books[0].ID

	newPath := filepath.Join(seriesDir, "Volume 001.cbz")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("rename failed: %v", err)
	}
	scanOnceForRehome(t, s, lib)
	waitForScannerCoverQueue(t, s)

	if err := s.CleanupLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	books, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	if len(books) != 1 || books[0].ID != originalID {
		t.Fatalf("cleanup removed the renamed book: got %d books (want id %d)", len(books), originalID)
	}
}

// TestCopiedBookIsNotRehomed 守住反向：源文件仍在磁盘上时，副本必须是**新书**而不是把旧行搬走。
// 若这条失败，说明重连把「复制」误判成了「改名」，用户会平白少一本书。
func TestCopiedBookIsNotRehomed(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()

	seriesDir := filepath.Join(libraryPath, "Series Alpha")
	originalPath := filepath.Join(seriesDir, "Vol 01.cbz")
	if err := writeScannerTestCBZ(originalPath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	s := newRehomeScanner(t, store)
	scanOnceForRehome(t, s, lib)

	// 连同 mtime 一起复制——最容易骗过 (size, mtime) 匹配的情形。
	raw, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	copyPath := filepath.Join(seriesDir, "Vol 01 (copy).cbz")
	if err := os.WriteFile(copyPath, raw, 0o644); err != nil {
		t.Fatalf("write copy failed: %v", err)
	}
	info, err := os.Stat(originalPath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if err := os.Chtimes(copyPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}

	scanOnceForRehome(t, s, lib)

	books, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	if len(books) != 2 {
		paths := make([]string, 0, len(books))
		for _, b := range books {
			paths = append(paths, b.Path)
		}
		t.Fatalf("expected copy to create a second book, got %d: %v", len(books), paths)
	}
}

// ---- renameIndex 守卫的单元测试 ----
//
// 这些用例直接构造索引，避免每条守卫都要跑一次完整扫描。
// 所有「旧路径」都指向 t.TempDir() 下确实不存在的文件，模拟「文件已消失」。

func goneDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestRenameIndexMatchesBySizeAndModTime(t *testing.T) {
	dir := goneDir(t)
	mod := time.Unix(1700000000, 12345)
	idx := newRenameIndex([]rehomeCandidate{
		{id: 11, path: filepath.Join(dir, "gone.cbz"), size: 4096, modTime: mod},
	})

	got := idx.claim(rehomeRequest{path: filepath.Join(dir, "renamed.cbz"), size: 4096, modTime: mod})
	if got == nil {
		t.Fatal("expected a rehome claim")
	}
	if got.bookID != 11 || got.reason != "size_mtime" {
		t.Fatalf("claim = %+v, want bookID 11 via size_mtime", got)
	}
}

func TestRenameIndexRefusesAmbiguousMatch(t *testing.T) {
	dir := goneDir(t)
	mod := time.Unix(1700000000, 0)
	// 两条候选共享同一 size+mtime（例如 `cp -p` 出来的副本）：无法判断该搬哪条，必须都不搬。
	idx := newRenameIndex([]rehomeCandidate{
		{id: 11, path: filepath.Join(dir, "gone-a.cbz"), size: 4096, modTime: mod},
		{id: 12, path: filepath.Join(dir, "gone-b.cbz"), size: 4096, modTime: mod},
	})

	if got := idx.claim(rehomeRequest{path: filepath.Join(dir, "renamed.cbz"), size: 4096, modTime: mod}); got != nil {
		t.Fatalf("expected no claim for ambiguous key, got %+v", got)
	}
}

func TestRenameIndexClaimsEachRowOnlyOnce(t *testing.T) {
	dir := goneDir(t)
	mod := time.Unix(1700000000, 0)
	idx := newRenameIndex([]rehomeCandidate{
		{id: 11, path: filepath.Join(dir, "gone.cbz"), size: 4096, modTime: mod},
	})

	if got := idx.claim(rehomeRequest{path: filepath.Join(dir, "first.cbz"), size: 4096, modTime: mod}); got == nil {
		t.Fatal("expected first claim to succeed")
	}
	// 同一批里第二个同尺寸同时间的文件不能再抢同一行，否则前一本书会被这一本顶掉。
	if got := idx.claim(rehomeRequest{path: filepath.Join(dir, "second.cbz"), size: 4096, modTime: mod}); got != nil {
		t.Fatalf("expected second claim to be refused, got %+v", got)
	}
}

func TestRenameIndexPrefersContentHash(t *testing.T) {
	dir := goneDir(t)
	mod := time.Unix(1700000000, 0)
	idx := newRenameIndex([]rehomeCandidate{
		// 目标：内容指纹相同但大小/时间已变（用户重新压缩过）。
		{id: 11, path: filepath.Join(dir, "gone-hash.cbz"), size: 111, modTime: mod, quickHash: "qh-a"},
		// 干扰：大小/时间恰好撞上，但内容不同。
		{id: 12, path: filepath.Join(dir, "gone-size.cbz"), size: 4096, modTime: mod, quickHash: "qh-b"},
	})

	got := idx.claim(rehomeRequest{
		path: filepath.Join(dir, "renamed.cbz"), size: 4096, modTime: mod, quickHash: "qh-a",
	})
	if got == nil {
		t.Fatal("expected a rehome claim")
	}
	if got.bookID != 11 || got.reason != "quick_hash" {
		t.Fatalf("claim = %+v, want bookID 11 via quick_hash", got)
	}
}

func TestRenameIndexIgnoresPathAlreadyInLibrary(t *testing.T) {
	dir := goneDir(t)
	mod := time.Unix(1700000000, 0)
	existing := filepath.Join(dir, "existing.cbz")
	idx := newRenameIndex([]rehomeCandidate{
		{id: 11, path: filepath.Join(dir, "gone.cbz"), size: 4096, modTime: mod},
		{id: 12, path: existing, size: 4096, modTime: time.Unix(1, 0)},
	})

	// existing 已经在库里 = 这是普通的增量更新，不是改名。
	if got := idx.claim(rehomeRequest{path: existing, size: 4096, modTime: mod}); got != nil {
		t.Fatalf("expected no claim for an already-known path, got %+v", got)
	}
}

func TestRenameIndexKeepsRowWhenOldFileStillExists(t *testing.T) {
	dir := goneDir(t)
	stillThere := filepath.Join(dir, "still-there.cbz")
	if err := os.WriteFile(stillThere, []byte("data"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	info, err := os.Stat(stillThere)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	idx := newRenameIndex([]rehomeCandidate{
		{id: 11, path: stillThere, size: info.Size(), modTime: info.ModTime()},
	})

	got := idx.claim(rehomeRequest{
		path: filepath.Join(dir, "copy.cbz"), size: info.Size(), modTime: info.ModTime(),
	})
	if got != nil {
		t.Fatalf("expected no claim while the source file is still on disk, got %+v", got)
	}
}

// TestSeriesScanRehomesRenamedBook 覆盖 ScanSeries 这条路径——它的候选集来自
// ListBooksBySeries，与 ScanLibrary 是两处独立的索引构建。
func TestSeriesScanRehomesRenamedBook(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()

	seriesDir := filepath.Join(libraryPath, "Series Alpha")
	oldPath := filepath.Join(seriesDir, "Vol 01.cbz")
	if err := writeScannerTestCBZ(oldPath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz failed: %v", err)
	}

	s := newRehomeScanner(t, store)
	scanOnceForRehome(t, s, lib)

	books, _ := store.ListBooksByLibrary(ctx, lib.ID)
	if len(books) != 1 {
		t.Fatalf("expected 1 book, got %d", len(books))
	}
	originalID := books[0].ID
	book, err := store.GetBook(ctx, originalID)
	if err != nil {
		t.Fatalf("get book failed: %v", err)
	}

	newPath := filepath.Join(seriesDir, "Volume 001.cbz")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	if err := s.ScanSeries(ctx, book.SeriesID, false); err != nil {
		t.Fatalf("scan series failed: %v", err)
	}

	books, err = store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	if len(books) != 1 || books[0].ID != originalID || books[0].Path != newPath {
		t.Fatalf("series scan did not rehome the rename: got %d books, first = id %d path %q",
			len(books), books[0].ID, books[0].Path)
	}
}
