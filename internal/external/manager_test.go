// 业务说明：本文件是业务回归测试，覆盖外部库同步会话的生命周期与访问隔离。
// internal/external 此前是唯一完全没有测试的业务包（482 行零覆盖），而它管理的是
// 跨库文件传输——一旦会话串了库或过期未清，影响的是真实文件的读写目标。
// 维护时应保证「跨 libraryID 访问必须失败」这条隔离性质始终有用例守着。

package external

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/database"
)

func newExternalTestStore(t *testing.T) (database.Store, database.Library, string) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "manga.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	libPath := filepath.Join(root, "library")
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("mkdir library failed: %v", err)
	}
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:                "Library",
		Path:                libPath,
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         "zip,cbz,rar,cbr",
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}
	return store, lib, root
}

func TestCreateSessionRejectsNonDirectoryAndMissingPath(t *testing.T) {
	store, lib, root := newExternalTestStore(t)
	m := NewManager(store, time.Hour)
	ctx := context.Background()

	if _, err := m.CreateSession(ctx, lib.ID, filepath.Join(root, "nope"), false); err == nil {
		t.Fatal("expected a missing external path to be rejected")
	}

	filePath := filepath.Join(root, "a-file.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	if _, err := m.CreateSession(ctx, lib.ID, filePath, false); err == nil {
		t.Fatal("expected a regular file to be rejected as an external path")
	}
}

func TestCreateSessionRejectsUnknownLibrary(t *testing.T) {
	store, _, root := newExternalTestStore(t)
	m := NewManager(store, time.Hour)

	external := filepath.Join(root, "external")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatalf("mkdir external failed: %v", err)
	}
	if _, err := m.CreateSession(context.Background(), 999999, external, false); err == nil {
		t.Fatal("expected an unknown library id to be rejected")
	}
}

// TestSessionAccessIsScopedToItsLibrary 锁住会话的访问隔离。
// 会话 id 一旦泄露，若不校验 libraryID，别的库就能读到该会话（含服务器绝对路径）
// 并对着它发起传输——传输的目标目录也就跟着串了。
func TestSessionAccessIsScopedToItsLibrary(t *testing.T) {
	store, lib, root := newExternalTestStore(t)
	m := NewManager(store, time.Hour)
	ctx := context.Background()

	external := filepath.Join(root, "external")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatalf("mkdir external failed: %v", err)
	}
	snap, err := m.CreateSession(ctx, lib.ID, external, false)
	if err != nil {
		t.Fatalf("create session failed: %v", err)
	}

	if _, err := m.GetSession(lib.ID, snap.SessionID); err != nil {
		t.Fatalf("owning library should see its own session: %v", err)
	}

	otherLibraryID := lib.ID + 1
	if _, err := m.GetSession(otherLibraryID, snap.SessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("cross-library session read must fail with ErrSessionNotFound, got %v", err)
	}
	if _, err := m.GetSeriesCoverage(otherLibraryID, snap.SessionID, []int64{1}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("cross-library coverage read must fail with ErrSessionNotFound, got %v", err)
	}
	if _, err := m.PrepareTransfer(ctx, otherLibraryID, snap.SessionID, []int64{1}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("cross-library transfer must fail with ErrSessionNotFound, got %v", err)
	}
}

func TestUnknownSessionIsNotFound(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	m := NewManager(store, time.Hour)

	if _, err := m.GetSession(lib.ID, "does-not-exist"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

// TestSessionsExpireAfterTTL 锁住会话的 TTL 淘汰：会话持有外部盘的扫描结果，
// 不过期就会随使用无限累积在内存里。
func TestSessionsExpireAfterTTL(t *testing.T) {
	store, lib, root := newExternalTestStore(t)
	// TTL 取负数：任何已存在的会话在下一次 prune 时都已「过期」，无需 sleep。
	m := NewManager(store, -time.Second)
	ctx := context.Background()

	external := filepath.Join(root, "external")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatalf("mkdir external failed: %v", err)
	}
	snap, err := m.CreateSession(ctx, lib.ID, external, false)
	if err != nil {
		t.Fatalf("create session failed: %v", err)
	}

	if _, err := m.GetSession(lib.ID, snap.SessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expired session should be pruned, got %v", err)
	}
}

func TestClearSessionRemovesOnlyItsOwnLibrarySession(t *testing.T) {
	store, lib, root := newExternalTestStore(t)
	m := NewManager(store, time.Hour)
	ctx := context.Background()

	external := filepath.Join(root, "external")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatalf("mkdir external failed: %v", err)
	}
	snap, err := m.CreateSession(ctx, lib.ID, external, false)
	if err != nil {
		t.Fatalf("create session failed: %v", err)
	}

	// 用别的库 id 清理不应生效。
	m.ClearSession(lib.ID+1, snap.SessionID)
	if _, err := m.GetSession(lib.ID, snap.SessionID); err != nil {
		t.Fatalf("session must survive a clear from another library: %v", err)
	}

	m.ClearSession(lib.ID, snap.SessionID)
	if _, err := m.GetSession(lib.ID, snap.SessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session should be gone after its owner clears it, got %v", err)
	}
}
