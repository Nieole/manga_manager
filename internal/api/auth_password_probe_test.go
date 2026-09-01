// 守「用户名存不存在，从口令校验的开销上看不出来」这条属性：判据不是耗时，而是比对本身的
// 调用次数与 cost——前者天生抖，后者是确定的。

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"manga-manager/internal/database"
)

// passwordCompareRecorder 记录每次口令比对拿到的哈希，用来回答「这条分支跑了几次 KDF、cost 多少」。
type passwordCompareRecorder struct {
	mu     sync.Mutex
	hashes [][]byte
}

// recordPasswordCompares 在测试期间接管 bcryptCompare，退出时还原。
func recordPasswordCompares(t *testing.T) *passwordCompareRecorder {
	t.Helper()
	rec := &passwordCompareRecorder{}
	original := bcryptCompare
	bcryptCompare = func(hash, pw []byte) error {
		rec.mu.Lock()
		rec.hashes = append(rec.hashes, append([]byte(nil), hash...))
		rec.mu.Unlock()
		return original(hash, pw)
	}
	t.Cleanup(func() { bcryptCompare = original })
	return rec
}

// wantSingleCompareAtCost 断言这条分支恰好跑了一次比对，且其 cost 与真实账户的哈希一致。
// cost 不同 = 开销差还在，只是换了个形状。
func (rec *passwordCompareRecorder) wantSingleCompareAtCost(t *testing.T, wantCost int) {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.hashes) != 1 {
		t.Fatalf("这条分支跑了 %d 次口令比对，期望恰好 1 次——次数不同即开销可区分", len(rec.hashes))
	}
	gotCost, err := bcrypt.Cost(rec.hashes[0])
	if err != nil {
		t.Fatalf("参与比对的哈希格式非法（%v）——非法哈希会让 bcrypt 立刻返回，等于一次 KDF 都没跑", err)
	}
	if gotCost != wantCost {
		t.Fatalf("比对 cost = %d，真实账户 cost = %d——cost 不一致，开销差依旧存在", gotCost, wantCost)
	}
}

// TestPasswordCheckCostIsIndependentOfUsernameExistence 覆盖网页登录与协议 Basic 鉴权两条口令校验路径。
func TestPasswordCheckCostIsIndependentOfUsernameExistence(t *testing.T) {
	c, store, _, _ := newTestController(t)
	ctx := context.Background()
	hash, err := hashPassword("password1")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	user, err := store.CreateUser(ctx, database.CreateUserParams{
		Username: "alice", PasswordHash: hash, Role: database.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	c.auth.markUsersExist()
	realCost, err := bcrypt.Cost([]byte(user.PasswordHash))
	if err != nil {
		t.Fatalf("read real account cost: %v", err)
	}

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	postLogin := func(t *testing.T, username, password string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": username, "password": password})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("login request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("凭据错误应得 401，实得 %d", resp.StatusCode)
		}
	}

	t.Run("网页登录：用户名不存在时也跑满一次同 cost 的比对", func(t *testing.T) {
		rec := recordPasswordCompares(t)
		postLogin(t, "ghost", "password1")
		rec.wantSingleCompareAtCost(t, realCost)
	})

	t.Run("网页登录：用户名存在但口令错误仍只跑一次比对", func(t *testing.T) {
		rec := recordPasswordCompares(t)
		postLogin(t, "alice", "wrongpass")
		rec.wantSingleCompareAtCost(t, realCost)
	})

	t.Run("协议 Basic 鉴权：用户名不存在时也跑满一次同 cost 的比对", func(t *testing.T) {
		rec := recordPasswordCompares(t)
		if _, ok := c.resolveBasicAuthUser(ctx, "ghost", "password1"); ok {
			t.Fatal("不存在的用户名不该通过 Basic 鉴权")
		}
		rec.wantSingleCompareAtCost(t, realCost)
	})

	t.Run("协议 Basic 鉴权：用户名存在但口令错误仍只跑一次比对", func(t *testing.T) {
		rec := recordPasswordCompares(t)
		if _, ok := c.resolveBasicAuthUser(ctx, "alice", "wrongpass"); ok {
			t.Fatal("口令错误不该通过 Basic 鉴权")
		}
		rec.wantSingleCompareAtCost(t, realCost)
	})

	t.Run("协议 Basic 鉴权：凭据正确仍然放行", func(t *testing.T) {
		if uid, ok := c.resolveBasicAuthUser(ctx, "alice", "password1"); !ok || uid != user.ID {
			t.Fatalf("正确凭据应放行并返回 uid=%d，实得 uid=%d ok=%v", user.ID, uid, ok)
		}
	})
}
