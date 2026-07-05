// 业务说明：本文件是业务回归测试，覆盖站点账户体系(users.go)的账户与会话存储原语：账户创建的角色归一
// 与用户名唯一冲突、按 id/用户名查询的未找到路径、管理员计数与首个管理员定位，以及会话的建立/取回/过期/
// 滑动续期/登出/按用户清理/过期清理。这是「强制登录 + 首次建管理员 + 每用户进度」多用户主线的地基。

package database

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateUserRoleNormalizationAndUniqueness(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()

	admin, err := store.CreateUser(ctx, CreateUserParams{Username: "root", PasswordHash: "h", Role: RoleAdmin})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if admin.Role != RoleAdmin || !admin.IsAdmin() {
		t.Fatalf("admin role want admin got %q", admin.Role)
	}

	// 未知角色应归一为 regular。
	weird, err := store.CreateUser(ctx, CreateUserParams{Username: "weirdo", PasswordHash: "h", Role: "superuser"})
	if err != nil {
		t.Fatalf("create weird: %v", err)
	}
	if weird.Role != RoleRegular {
		t.Fatalf("unknown role should normalize to regular, got %q", weird.Role)
	}
	// 空角色也应为 regular。
	empty, err := store.CreateUser(ctx, CreateUserParams{Username: "plain", PasswordHash: "h"})
	if err != nil {
		t.Fatalf("create plain: %v", err)
	}
	if empty.Role != RoleRegular {
		t.Fatalf("empty role should be regular, got %q", empty.Role)
	}

	// 用户名精确重复 → ErrUsernameTaken。
	if _, err := store.CreateUser(ctx, CreateUserParams{Username: "root", PasswordHash: "h2"}); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate username want ErrUsernameTaken got %v", err)
	}
}

func TestUserLookupNotFoundPaths(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()

	if _, err := store.GetUserByUsername(ctx, "ghost"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing username want ErrUserNotFound got %v", err)
	}
	if _, err := store.GetUserByID(ctx, 99999); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing id want ErrUserNotFound got %v", err)
	}

	created, err := store.CreateUser(ctx, CreateUserParams{Username: "Alice", PasswordHash: "h", Role: RoleAdmin, DisplayName: "A"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.GetUserByUsername(ctx, "Alice")
	if err != nil {
		t.Fatalf("get by username: %v", err)
	}
	if got.ID != created.ID || got.DisplayName != "A" {
		t.Fatalf("lookup mismatch got %+v want id=%d", got, created.ID)
	}
	// GetUserByUsername 为精确匹配：大小写不同不应命中。
	if _, err := store.GetUserByUsername(ctx, "alice"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("case-different username should miss exact match, got %v", err)
	}
}

func TestCountAdminsAndFirstAdmin(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()

	// 无用户时：CountAdmins=0，FirstAdminUserID → ErrUserNotFound。
	if n, err := store.CountAdmins(ctx); err != nil || n != 0 {
		t.Fatalf("count admins empty want 0 got %d err %v", n, err)
	}
	if _, err := store.FirstAdminUserID(ctx); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("first admin with no admins want ErrUserNotFound got %v", err)
	}

	// 先建一个 regular，再建两个 admin：首个管理员应是 id 最小的那个 admin，而非 regular。
	reg := mkUser(t, ctx, store, "reg", RoleRegular)
	adm1 := mkUser(t, ctx, store, "adm1", RoleAdmin)
	adm2 := mkUser(t, ctx, store, "adm2", RoleAdmin)
	_ = adm2

	if n, err := store.CountUsers(ctx); err != nil || n != 3 {
		t.Fatalf("count users want 3 got %d err %v", n, err)
	}
	if n, err := store.CountAdmins(ctx); err != nil || n != 2 {
		t.Fatalf("count admins want 2 got %d err %v", n, err)
	}
	first, err := store.FirstAdminUserID(ctx)
	if err != nil {
		t.Fatalf("first admin: %v", err)
	}
	if first != adm1 {
		t.Fatalf("first admin want %d (min-id admin) got %d (reg=%d)", adm1, first, reg)
	}

	// ListUsers 按 id 升序。
	list, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].ID >= list[i].ID {
			t.Fatalf("ListUsers not id-ascending: %+v", list)
		}
	}
}

func TestUpdateUserProfileAndPassword(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	uid := mkUser(t, ctx, store, "u", RoleRegular)

	// 改角色为未知值应归一为 regular；改展示名生效。
	if err := store.UpdateUserProfile(ctx, uid, "Display", "captain"); err != nil {
		t.Fatalf("update profile: %v", err)
	}
	u, _ := store.GetUserByID(ctx, uid)
	if u.Role != RoleRegular || u.DisplayName != "Display" {
		t.Fatalf("profile after update = %+v want role=regular name=Display", u)
	}

	// 升级为 admin。
	if err := store.UpdateUserProfile(ctx, uid, "Display", RoleAdmin); err != nil {
		t.Fatalf("promote: %v", err)
	}
	u, _ = store.GetUserByID(ctx, uid)
	if !u.IsAdmin() {
		t.Fatalf("expected admin after promote, got %q", u.Role)
	}

	// 改密并清 must_change 标志。
	if err := store.UpdateUserPassword(ctx, uid, "newhash", false); err != nil {
		t.Fatalf("update password: %v", err)
	}
	u, _ = store.GetUserByID(ctx, uid)
	if u.PasswordHash != "newhash" || u.MustChangePassword {
		t.Fatalf("password state = %+v want hash=newhash mustChange=false", u)
	}
}

func TestSessionLifecycle(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	uid := mkUser(t, ctx, store, "alice", RoleAdmin)
	now := time.Now()

	sess := Session{
		ID:        "sha256-token-a",
		UserID:    uid,
		CSRFToken: "csrf-a",
		UserAgent: "test-agent",
		ExpiresAt: now.Add(time.Hour),
	}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// 取回未过期会话并带出账户。
	gotSess, gotUser, err := store.GetSessionWithUser(ctx, sess.ID, now)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if gotSess.UserID != uid || gotSess.CSRFToken != "csrf-a" || gotUser.ID != uid || gotUser.Username != "alice" {
		t.Fatalf("session/user mismatch: sess=%+v user=%+v", gotSess, gotUser)
	}

	// 用未来时刻查询（会话已过期）→ ErrSessionNotFound。
	if _, _, err := store.GetSessionWithUser(ctx, sess.ID, now.Add(2*time.Hour)); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expired lookup want ErrSessionNotFound got %v", err)
	}

	// 滑动续期后，原本会过期的时刻又变为有效。
	if err := store.TouchSession(ctx, sess.ID, now.Add(90*time.Minute), now.Add(3*time.Hour)); err != nil {
		t.Fatalf("touch: %v", err)
	}
	if _, _, err := store.GetSessionWithUser(ctx, sess.ID, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("after touch should be valid at +2h, got %v", err)
	}

	// 登出删除单会话。
	if err := store.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, _, err := store.GetSessionWithUser(ctx, sess.ID, now); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("after delete want ErrSessionNotFound got %v", err)
	}
}

func TestSessionBulkAndExpiryCleanup(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	uid := mkUser(t, ctx, store, "alice", RoleAdmin)
	now := time.Now()

	mk := func(id string, expires time.Time) {
		t.Helper()
		if err := store.CreateSession(ctx, Session{ID: id, UserID: uid, ExpiresAt: expires}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	mk("live-1", now.Add(time.Hour))
	mk("live-2", now.Add(time.Hour))
	mk("stale-1", now.Add(-time.Hour))

	// DeleteExpiredSessions 只清过期的，保留有效的。
	if err := store.DeleteExpiredSessions(ctx, now); err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if _, _, err := store.GetSessionWithUser(ctx, "stale-1", now); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("stale should be gone, got %v", err)
	}
	if _, _, err := store.GetSessionWithUser(ctx, "live-1", now); err != nil {
		t.Fatalf("live-1 should survive expiry cleanup, got %v", err)
	}

	// DeleteSessionsForUser 清掉该账户全部会话。
	if err := store.DeleteSessionsForUser(ctx, uid); err != nil {
		t.Fatalf("delete for user: %v", err)
	}
	if _, _, err := store.GetSessionWithUser(ctx, "live-2", now); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("live-2 should be gone after per-user purge, got %v", err)
	}
}

func TestDeleteUserCascadesSessions(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	uid := mkUser(t, ctx, store, "alice", RoleAdmin)
	now := time.Now()
	if err := store.CreateSession(ctx, Session{ID: "s-cascade", UserID: uid, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.DeleteUser(ctx, uid); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	// 账户已删。
	if _, err := store.GetUserByID(ctx, uid); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("user should be gone, got %v", err)
	}
	// FK ON DELETE CASCADE 应连带删掉其会话。
	if _, _, err := store.GetSessionWithUser(ctx, "s-cascade", now); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session should cascade-delete with user, got %v", err)
	}
}
