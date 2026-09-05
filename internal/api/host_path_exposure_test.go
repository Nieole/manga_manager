// 守「宿主机绝对路径只给管理员」这条不变量：破了就等于任意站点账号能拿到服务器的目录结构。
// 用真实会话走完整中间件链，守卫层（谁进得来）与字段层（进来能看到什么）各守一半。

package api

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/database"

	"github.com/go-chi/chi/v5"
)

// hostPathFixture 是一套带真实宿主机路径的数据 + 一台跑着完整路由的服务器。
type hostPathFixture struct {
	router *chi.Mux
	srv    *httptest.Server
	lib    database.Library
	series database.Series
	book   database.Book
	book2  database.Book
	// order 是这两本书在阅读顺序里的排列，book-next/book-prev 按它取上下册。
	order []database.Book
	root  string
}

func newHostPathFixture(t *testing.T) hostPathFixture {
	t.Helper()
	c, store, _, rootDir := newTestController(t)
	lib, series, book := seedBookFixture(t, store, rootDir, "Lib", "Series", "book.cbz", 10)
	if lib.Path == "" || series.Path == "" || book.Path == "" {
		t.Fatalf("fixture 前提不成立：路径是空的，用例守不住任何东西 lib=%q series=%q book=%q", lib.Path, series.Path, book.Path)
	}
	// 同系列的第二本：没有它 /api/book-next 与 /api/book-prev 只会回 404，测不到响应体。
	book2, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID: series.ID, LibraryID: lib.ID, Name: "book2.cbz",
		Path: filepath.Join(series.Path, "book2.cbz"), Size: 2048,
		FileModifiedAt: time.Now(), PageCount: 10,
	})
	if err != nil {
		t.Fatalf("CreateBook failed: %v", err)
	}
	_ = mkTestUser(t, store, "admin", database.RoleAdmin)
	_ = mkTestUser(t, store, "reg", database.RoleRegular)

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	order := []database.Book{book, book2}
	sortBooksForReading(order)
	return hostPathFixture{router: r, srv: srv, lib: lib, series: series, book: book, book2: book2, order: order, root: rootDir}
}

// loginAs 走真实的登录端点拿到会话 cookie，后续请求因此经过完整的 authGate。
func (f hostPathFixture) loginAs(t *testing.T, username string) *http.Client {
	t.Helper()
	cl := newAuthClient(t)
	resp, _ := authDo(t, cl, http.MethodPost, f.srv.URL+"/api/auth/login", "",
		map[string]string{"username": username, "password": "password1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s login want 200 got %d", username, resp.StatusCode)
	}
	return cl
}

func (f hostPathFixture) get(t *testing.T, cl *http.Client, path string) (*http.Response, []byte) {
	t.Helper()
	return authDo(t, cl, http.MethodGet, f.srv.URL+path, "", nil)
}

// TestReadEndpointsHideHostPathFromRegularUser 覆盖所有直发数据库行的读端点：普通账号的响应里
// 不得出现资料库/系列/书籍的宿主机绝对路径，管理员照旧拿得到（资料库编辑弹窗要把它填回去）。
func TestReadEndpointsHideHostPathFromRegularUser(t *testing.T) {
	f := newHostPathFixture(t)
	libID := strconv.FormatInt(f.lib.ID, 10)
	seriesID := strconv.FormatInt(f.series.ID, 10)
	bookID := strconv.FormatInt(f.book.ID, 10)

	cases := []struct {
		name string
		path string
		// adminSeesPath 为真时额外断言同一端点对管理员仍带路径，
		// 证明裁的是角色而不是把字段整个废掉。
		adminSeesPath bool
	}{
		{name: "libraries", path: "/api/libraries", adminSeesPath: true},
		{name: "books by series", path: "/api/books/" + seriesID, adminSeesPath: true},
		{name: "book info", path: "/api/book-info/" + bookID, adminSeesPath: true},
		{name: "next book", path: "/api/book-next/" + strconv.FormatInt(f.order[0].ID, 10), adminSeesPath: true},
		{name: "prev book", path: "/api/book-prev/" + strconv.FormatInt(f.order[1].ID, 10), adminSeesPath: true},
		{name: "series by library", path: "/api/series/" + libID, adminSeesPath: true},
		{name: "series info", path: "/api/series/info/" + seriesID, adminSeesPath: true},
		{name: "series search", path: "/api/series/search?libraryId=" + libID, adminSeesPath: true},
		{name: "series context", path: "/api/series/" + seriesID + "/context", adminSeesPath: true},
		{name: "series recent read", path: "/api/series/recent-read?libraryId=" + libID},
		{name: "stats recent read", path: "/api/stats/recent-read"},
		{name: "book search", path: "/api/search?q=book"},
		{name: "collection views", path: "/api/collection-views"},
		{name: "recommendations", path: "/api/recommendations"},
	}

	regular := f.loginAs(t, "reg")
	admin := f.loginAs(t, "admin")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := f.get(t, regular, tc.path)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("普通用户 GET %s want 200 got %d (body=%s)", tc.path, resp.StatusCode, body)
			}
			assertNoHostPath(t, f, body)

			if !tc.adminSeesPath {
				return
			}
			resp, body = f.get(t, admin, tc.path)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("管理员 GET %s want 200 got %d (body=%s)", tc.path, resp.StatusCode, body)
			}
			if !strings.Contains(string(body), jsonQuoted(f.lib.Path)) &&
				!strings.Contains(string(body), jsonQuoted(f.series.Path)) &&
				!strings.Contains(string(body), jsonQuoted(f.book.Path)) {
				t.Fatalf("管理员 GET %s 一条路径都没拿到，净化裁过头了：%s", tc.path, body)
			}
		})
	}
}

// TestDuplicateBooksRequiresAdmin 守卫层：重复文件列表连读都必须是管理员——它按绝对路径 +
// 体积 + file_hash 判断留哪一份，写侧 /api/books/remove 早已是管理员专属，读侧不得更宽。
func TestDuplicateBooksRequiresAdmin(t *testing.T) {
	f := newHostPathFixture(t)

	resp, body := f.get(t, f.loginAs(t, "reg"), "/api/books/duplicates")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("普通用户 GET /api/books/duplicates want 403 got %d (body=%s)", resp.StatusCode, body)
	}
	assertNoHostPath(t, f, body)

	resp, body = f.get(t, f.loginAs(t, "admin"), "/api/books/duplicates")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("管理员 GET /api/books/duplicates want 200 got %d (body=%s)", resp.StatusCode, body)
	}
}

// TestOPDSLibrariesFeedHidesHostPath OPDS 资料库列表的 content 是给人看的描述文字，
// 必须是系列数而不是宿主机绝对路径——任何站点账号都能在阅读器里打开这一屏。
func TestOPDSLibrariesFeedHidesHostPath(t *testing.T) {
	c, store, _, rootDir := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, rootDir, "Lib", "Series", "book.cbz", 10)

	rec := httptest.NewRecorder()
	c.opdsLibraries(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/libraries", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("opdsLibraries want 200 got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), lib.Path) {
		t.Fatalf("OPDS 资料库列表里带着宿主机绝对路径 %q：%s", lib.Path, rec.Body.String())
	}

	var feed OPDSFeed
	if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
		t.Fatalf("decode libraries feed failed: %v", err)
	}
	if len(feed.Entries) != 1 {
		t.Fatalf("want 1 entry got %d", len(feed.Entries))
	}
	// 置空会让每个条目都空着一行；描述必须仍然说得出这个资料库有什么。
	if want := opdsSeriesCountText("zh-CN", 1); feed.Entries[0].Content != want {
		t.Fatalf("entry content want %q got %q", want, feed.Entries[0].Content)
	}
}

// TestJSONResponseRedactsHostPathsByDefault 守「新端点默认安全」：一个此前不存在的响应结构体
// 不需要在任何名单里登记，只要经 jsonResponse 出去就会被裁；拿不到管理员标记时一律按普通用户处理。
func TestJSONResponseRedactsHostPathsByDefault(t *testing.T) {
	type brandNewRow struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	payload := map[string]any{"items": []brandNewRow{{Name: "n", Path: "/srv/manga/x.cbz"}}}

	decode := func(t *testing.T, body []byte) brandNewRow {
		t.Helper()
		var got struct {
			Items []brandNewRow `json:"items"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode failed: %v (body=%s)", err, body)
		}
		if len(got.Items) != 1 {
			t.Fatalf("want 1 item got %d", len(got.Items))
		}
		return got.Items[0]
	}

	t.Run("no admin marker", func(t *testing.T) {
		rec := httptest.NewRecorder()
		jsonResponse(rec, http.StatusOK, payload)
		if got := decode(t, rec.Body.Bytes()); got.Path != "" || got.Name != "n" {
			t.Fatalf("未带管理员标记的响应应当只裁掉 path：%+v", got)
		}
	})

	t.Run("admin marker", func(t *testing.T) {
		rec := httptest.NewRecorder()
		jsonResponse(&adminResponseWriter{ResponseWriter: rec}, http.StatusOK, payload)
		if got := decode(t, rec.Body.Bytes()); got.Path != "/srv/manga/x.cbz" {
			t.Fatalf("管理员应拿到真实 path：%+v", got)
		}
	})

	// 净化不得就地改写入参：响应值可能来自进程内缓存，改坏了是下一次请求才显形。
	if payload["items"].([]brandNewRow)[0].Path != "/srv/manga/x.cbz" {
		t.Fatal("redactHostPaths 就地改写了调用方持有的值")
	}
}

// TestRedactHostPathsSkipsExemptTypes 两处对普通用户回显路径是现行可见行为（整理页的健康问题、
// 读不到字节时的错误提示），净化必须放过它们，否则会静默改掉界面上已有的显示。
func TestRedactHostPathsSkipsExemptTypes(t *testing.T) {
	issue := database.HealthIssue{Type: "missing_file", Path: "/srv/manga/x.cbz"}
	if got := redactHostPaths(issue).(database.HealthIssue); got.Path != issue.Path {
		t.Fatalf("HealthIssue.Path 不该被裁：%+v", got)
	}
	failure := StorageFailureResponse{Error: "e", LibraryPath: "/srv/manga", Path: "/srv/manga/x.cbz"}
	got := redactHostPaths(failure).(StorageFailureResponse)
	if got.Path != failure.Path || got.LibraryPath != failure.LibraryPath {
		t.Fatalf("StorageFailureResponse 的路径不该被裁：%+v", got)
	}
}

// hostPathSweepSkip 是清扫用例够不着的读端点：一条会把连接挂住，两条会打真实外网。
var hostPathSweepSkip = map[string]string{
	"/api/events":                          "SSE 长连接，取不到完整响应体（订阅侧的角色口径另有其账）",
	"/api/metadata/search":                 "打外部刮削源，清扫用例不做网络请求",
	"/api/series/{seriesId}/scrape-search": "同上",
}

// hostPathSweepExempt 是清扫时已知仍会回显路径的读端点：它们对普通用户显示路径是**现行可见行为**
// （整理页的健康问题列表、读不到字节时的错误提示），改动需要产品裁决，不在本轮范围内。
var hostPathSweepExempt = map[string]string{
	"/api/health/report":               "整理页在非管理员分支里就渲染 issue.path",
	"/api/books/{bookId}/file":         "StorageFailureResponse 要说清是哪个盘掉了",
	"/api/pages/{bookId}":              "同上",
	"/api/pages/{bookId}/{pageNumber}": "同上",
}

// TestNoReadEndpointLeaksHostPath 遍历路由树上**全部** GET 端点，用普通账号各请求一次，
// 断言响应体里不出现宿主机路径。它守的是「新增端点默认安全」：漏挂一处不会无声通过，
// 而是在这里变红——本次缺口正是逐个 handler 裁剪时漏了一处造成的。
func TestNoReadEndpointLeaksHostPath(t *testing.T) {
	f := newHostPathFixture(t)
	regular := f.loginAs(t, "reg")
	params := strings.NewReplacer(
		"{libraryId}", strconv.FormatInt(f.lib.ID, 10),
		"{seriesId}", strconv.FormatInt(f.series.ID, 10),
		"{bookId}", strconv.FormatInt(f.book.ID, 10),
		"{pageNumber}", "1",
		"{collectionId}", "1", "{filterId}", "1", "{listId}", "1", "{userId}", "1",
		"{reviewId}", "1", "{relationId}", "1", "{tagId}", "1", "{accountId}", "1",
		"{progressId}", "1", "{bookmarkId}", "1", "{taskKey}", "k", "{sessionId}", "s",
		"/*", "/x",
	)

	err := chi.Walk(f.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet {
			return nil
		}
		if _, ok := hostPathSweepSkip[route]; ok {
			return nil
		}
		if _, ok := hostPathSweepExempt[route]; ok {
			return nil
		}
		path := strings.TrimSuffix(params.Replace(route), "/")
		if path == "" || strings.ContainsAny(path, "{}") {
			t.Errorf("路由 %s 有没填的路径参数，清扫覆盖不到它", route)
			return nil
		}
		resp, body := f.get(t, regular, path)
		for _, p := range []string{f.book.Path, f.book2.Path, f.series.Path, f.lib.Path, f.root} {
			if strings.Contains(string(body), jsonQuoted(p)) {
				t.Errorf("GET %s（status %d）把宿主机路径 %q 发给了普通用户：%s", path, resp.StatusCode, p, body)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes failed: %v", err)
	}
}

// assertNoHostPath 断言响应体里没有 fixture 的任何一条宿主机路径。
func assertNoHostPath(t *testing.T, f hostPathFixture, body []byte) {
	t.Helper()
	for _, p := range []string{f.book.Path, f.book2.Path, f.series.Path, f.lib.Path} {
		if strings.Contains(string(body), jsonQuoted(p)) {
			t.Fatalf("响应里带着宿主机绝对路径 %q：%s", p, body)
		}
	}
}

// jsonQuoted 把路径转成它在 JSON 里的字面量形式（Windows 的反斜杠会被转义），供子串比对。
func jsonQuoted(p string) string {
	b, _ := json.Marshal(p)
	return strings.Trim(string(b), `"`)
}
