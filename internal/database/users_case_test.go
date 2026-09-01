// 守账户身份的大小写口径：判重与查询必须是同一把字节精确的尺子。
// 两者一旦分叉，一次查询就可能命中多个仅大小写不同的账户，登录会落到其中任意一个头上。

package database

import (
	"context"
	"errors"
	"testing"
)

// TestUsernameIdentityIsByteExact 锁住 docs/adr/0003 选定的口径：改成 NOCASE 会让本用例失败，
// 从而把「存量库里已存在大小写冲突账户」的升级问题重新摆上台面。
func TestUsernameIdentityIsByteExact(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	lower, err := store.CreateUser(ctx, CreateUserParams{Username: "alice", PasswordHash: "hash-a", Role: RoleRegular})
	if err != nil {
		t.Fatalf("create alice failed: %v", err)
	}

	t.Run("仅大小写不同是两个独立账户", func(t *testing.T) {
		upper, err := store.CreateUser(ctx, CreateUserParams{Username: "Alice", PasswordHash: "hash-b", Role: RoleRegular})
		if err != nil {
			t.Fatalf("Alice 应当能独立建号，实得 %v", err)
		}
		if upper.ID == lower.ID {
			t.Fatal("两个账户拿到了同一个 id")
		}
	})

	t.Run("查询精确命中各自的账户", func(t *testing.T) {
		for _, want := range []struct {
			username string
			hash     string
		}{{"alice", "hash-a"}, {"Alice", "hash-b"}} {
			got, err := store.GetUserByUsername(ctx, want.username)
			if err != nil {
				t.Fatalf("查 %q 失败: %v", want.username, err)
			}
			if got.Username != want.username || got.PasswordHash != want.hash {
				t.Fatalf("查 %q 落到了 %q 头上", want.username, got.Username)
			}
		}
	})

	t.Run("同名重复建号仍被唯一约束挡下", func(t *testing.T) {
		if _, err := store.CreateUser(ctx, CreateUserParams{Username: "alice", PasswordHash: "hash-c", Role: RoleRegular}); !errors.Is(err, ErrUsernameTaken) {
			t.Fatalf("重名应得 ErrUsernameTaken，实得 %v", err)
		}
	})
}
