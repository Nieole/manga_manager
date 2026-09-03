// 守 /api/libraries 的字段级暴露：资料库列表的 path 是宿主机绝对路径，只该给管理员。

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/database"

	"github.com/go-chi/chi/v5"
)

// TestLibraryListHidesHostPathFromRegularUser 用真实会话走完整中间件链：普通用户拿到的
// 资料库列表不得带 path（宿主机目录结构），管理员照旧拿得到——编辑弹窗要填回去。
func TestLibraryListHidesHostPathFromRegularUser(t *testing.T) {
	c, store, _, rootDir := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, rootDir, "Lib", "Series", "book.cbz", 10)
	if lib.Path == "" {
		t.Fatal("fixture 前提不成立：资料库行的 path 是空的，这条用例就守不住任何东西")
	}
	_ = mkTestUser(t, store, "admin", database.RoleAdmin)
	_ = mkTestUser(t, store, "reg", database.RoleRegular)

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	listAs := func(username string) []database.Library {
		t.Helper()
		cl := newAuthClient(t)
		resp, _ := authDo(t, cl, http.MethodPost, srv.URL+"/api/auth/login", "",
			map[string]string{"username": username, "password": "password1"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s login want 200 got %d", username, resp.StatusCode)
		}
		resp, data := authDo(t, cl, http.MethodGet, srv.URL+"/api/libraries", "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s list libraries want 200 got %d", username, resp.StatusCode)
		}
		var libs []database.Library
		if err := json.Unmarshal(data, &libs); err != nil {
			t.Fatalf("%s decode libraries failed: %v (body=%s)", username, err, data)
		}
		if len(libs) != 1 {
			t.Fatalf("%s want 1 library got %d", username, len(libs))
		}
		return libs
	}

	if got := listAs("reg")[0]; got.Path != "" {
		t.Fatalf("普通用户拿到了宿主机绝对路径：%q", got.Path)
	}
	// 裁的是字段值不是整行：id/name 这些普通用户界面要用的字段必须还在。
	if got := listAs("reg")[0]; got.ID != lib.ID || got.Name != lib.Name {
		t.Fatalf("普通用户的资料库列表被裁过头了：%+v", got)
	}
	if got := listAs("admin")[0]; got.Path != lib.Path {
		t.Fatalf("管理员应拿到真实 path，want %q got %q", lib.Path, got.Path)
	}
}
