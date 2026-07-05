// 业务说明：本文件是后端 HTTP API 层的回归测试，聚焦「阅读进度 / 阅读时长 / 个人系列短评 / 深度统计」
// 这几条每用户业务线的边界与双路径（uid==0 全局旧路径 vs uid>0 每用户路径），以及筛选参数解析、
// 角色写权限、阅读协议 Basic 鉴权。测试直接调用处理器（注入 userCtxKey 模拟已登录会话）或经真实
// chi 路由（authGate）验证中间件行为，断言真实用户可观察结果，而非实现细节。

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"manga-manager/internal/database"
)

// withUserContext 把一个已登录用户注入请求上下文（等价于 authGate 解析会话后的效果），
// 让直接调用的处理器里 currentUserID 返回该用户 id，从而走每用户进度路径。
func withUserContext(req *http.Request, user database.User) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), userCtxKey, user))
}

// mkTestUser 建一个站点账户（密码固定 password1），返回其 User 供上下文注入与断言。
func mkTestUser(t *testing.T, store database.Store, username, role string) database.User {
	t.Helper()
	hash, err := hashPassword("password1")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	u, err := store.CreateUser(context.Background(), database.CreateUserParams{
		Username:     username,
		PasswordHash: hash,
		Role:         role,
	})
	if err != nil {
		t.Fatalf("CreateUser %s: %v", username, err)
	}
	return u
}

// ---- 筛选参数解析 ----

func TestParseFilterQueryHelpers(t *testing.T) {
	readStateCases := []struct{ in, want string }{
		{"unread", "unread"},
		{"reading", "reading"},
		{"completed", "completed"},
		{"READING", "reading"},         // 归一化大小写
		{"  Completed  ", "completed"}, // 去空白 + 归一化
		{"", ""},                       // 空 = 不筛选
		{"finished", ""},               // 未识别 = 不筛选
		{"unreadx", ""},                // 近似但不匹配
	}
	for _, tc := range readStateCases {
		if got := parseReadState(tc.in); got != tc.want {
			t.Errorf("parseReadState(%q)=%q want %q", tc.in, got, tc.want)
		}
	}

	// parseOptionalFloat：空/空白/非法 -> nil；合法（含负、零、科学计数）-> 指针值。
	if got := parseOptionalFloat(""); got != nil {
		t.Errorf("parseOptionalFloat(\"\") want nil got %v", *got)
	}
	if got := parseOptionalFloat("   "); got != nil {
		t.Errorf("parseOptionalFloat blank want nil")
	}
	if got := parseOptionalFloat("abc"); got != nil {
		t.Errorf("parseOptionalFloat(bad) want nil")
	}
	if got := parseOptionalFloat("3.5"); got == nil || *got != 3.5 {
		t.Errorf("parseOptionalFloat(3.5) got %v", got)
	}
	if got := parseOptionalFloat(" -2 "); got == nil || *got != -2 {
		t.Errorf("parseOptionalFloat(-2) got %v", got)
	}
	if got := parseOptionalFloat("0"); got == nil || *got != 0 {
		t.Errorf("parseOptionalFloat(0) got %v", got)
	}
	if got := parseOptionalFloat("1e3"); got == nil || *got != 1000 {
		t.Errorf("parseOptionalFloat(1e3) got %v", got)
	}

	// parseNonNegativeInt：空/非法/负数 -> 0；合法非负 -> 值。
	nnCases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 0},
		{"-5", 0}, // 负数按不筛选处理
		{"0", 0},
		{"42", 42},
		{" 7 ", 7},
		{"3.5", 0}, // Atoi 拒绝小数
	}
	for _, tc := range nnCases {
		if got := parseNonNegativeInt(tc.in); got != tc.want {
			t.Errorf("parseNonNegativeInt(%q)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseIDFromRouteParam(t *testing.T) {
	ok := requestWithRouteParam(http.MethodGet, "/x", nil, "bookId", "123")
	if id, err := parseID(ok, "bookId"); err != nil || id != 123 {
		t.Fatalf("parseID valid: id=%d err=%v", id, err)
	}
	bad := requestWithRouteParam(http.MethodGet, "/x", nil, "bookId", "abc")
	if _, err := parseID(bad, "bookId"); err == nil {
		t.Fatal("parseID non-numeric should error")
	}
	// 未出现的路由参数解析为空串 -> ParseInt 失败。
	if _, err := parseID(bad, "seriesId"); err == nil {
		t.Fatal("parseID missing param should error")
	}
	neg := requestWithRouteParam(http.MethodGet, "/x", nil, "bookId", "-9")
	if id, err := parseID(neg, "bookId"); err != nil || id != -9 {
		t.Fatalf("parseID negative: id=%d err=%v", id, err)
	}
}

// ---- updateBookProgress：夹取 / 错误路径 / 陈旧跳过 / 每用户隔离 ----

func statusOf(rec *httptest.ResponseRecorder) string {
	var m map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return m["status"]
}

func TestUpdateBookProgressClampingAndErrors(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 10)
	bid := strconv.FormatInt(book.ID, 10)

	call := func(bookIDParam, body string) *httptest.ResponseRecorder {
		req := requestWithRouteParam(http.MethodPost, "/x", []byte(body), "bookId", bookIDParam)
		rec := httptest.NewRecorder()
		controller.updateBookProgress(rec, req)
		return rec
	}

	if rec := call("abc", `{"page":3}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid book id want 400 got %d", rec.Code)
	}
	if rec := call(bid, `not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed payload want 400 got %d", rec.Code)
	}
	if rec := call("999999", `{"page":3}`); rec.Code != http.StatusNotFound {
		t.Fatalf("missing book want 404 got %d", rec.Code)
	}

	// page 超过 page_count 夹取到 10。
	if rec := call(bid, `{"page":999}`); rec.Code != http.StatusOK {
		t.Fatalf("clamp want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetBook(context.Background(), book.ID)
	if !got.LastReadPage.Valid || got.LastReadPage.Int64 != 10 {
		t.Fatalf("page should clamp to 10, got %+v", got.LastReadPage)
	}

	// page<=0 归一化为 1。
	if rec := call(bid, `{"page":0}`); rec.Code != http.StatusOK {
		t.Fatalf("zero page want 200 got %d", rec.Code)
	}
	got, _ = store.GetBook(context.Background(), book.ID)
	if !got.LastReadPage.Valid || got.LastReadPage.Int64 != 1 {
		t.Fatalf("page<=0 should become 1, got %+v", got.LastReadPage)
	}
}

func TestUpdateBookProgressStaleUpdatedAtSkipped(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 20)
	ctx := context.Background()

	serverAt := time.Now()
	if err := store.UpdateBookProgress(ctx, database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 12, Valid: true},
		LastReadAt:   sql.NullTime{Time: serverAt, Valid: true},
		ID:           book.ID,
	}); err != nil {
		t.Fatalf("seed server progress: %v", err)
	}

	bid := strconv.FormatInt(book.ID, 10)
	post := func(page int64, updatedAt *time.Time) *httptest.ResponseRecorder {
		b, _ := json.Marshal(UpdateProgressRequest{Page: page, UpdatedAt: updatedAt})
		req := requestWithRouteParam(http.MethodPost, "/x", b, "bookId", bid)
		rec := httptest.NewRecorder()
		controller.updateBookProgress(rec, req)
		return rec
	}

	// 客户端时间戳早于服务端记录 -> 判定本地陈旧，跳过，页码保持 12。
	stale := serverAt.Add(-time.Hour)
	rec := post(3, &stale)
	if rec.Code != http.StatusOK || statusOf(rec) != "Progress unchanged" {
		t.Fatalf("stale write should be unchanged, code=%d status=%q", rec.Code, statusOf(rec))
	}
	if got, _ := store.GetBook(ctx, book.ID); got.LastReadPage.Int64 != 12 {
		t.Fatalf("stale write must not overwrite, got %d", got.LastReadPage.Int64)
	}

	// 客户端时间戳更新 -> 应用写入。
	newer := serverAt.Add(time.Hour)
	rec = post(15, &newer)
	if rec.Code != http.StatusOK || statusOf(rec) != "Progress updated" {
		t.Fatalf("newer write should apply, code=%d status=%q", rec.Code, statusOf(rec))
	}
	if got, _ := store.GetBook(ctx, book.ID); got.LastReadPage.Int64 != 15 {
		t.Fatalf("newer write should set page 15, got %d", got.LastReadPage.Int64)
	}
}

func TestUpdateBookProgressPerUserIsolation(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 30)
	ctx := context.Background()
	userA := mkTestUser(t, store, "alice", database.RoleRegular)
	userB := mkTestUser(t, store, "bob", database.RoleRegular)
	bid := strconv.FormatInt(book.ID, 10)

	post := func(u database.User, page int64) *httptest.ResponseRecorder {
		req := withUserContext(
			requestWithRouteParam(http.MethodPost, "/x", []byte(`{"page":`+strconv.FormatInt(page, 10)+`}`), "bookId", bid),
			u,
		)
		rec := httptest.NewRecorder()
		controller.updateBookProgress(rec, req)
		return rec
	}

	if rec := post(userA, 5); rec.Code != http.StatusOK {
		t.Fatalf("A write want 200 got %d", rec.Code)
	}
	if rec := post(userB, 8); rec.Code != http.StatusOK {
		t.Fatalf("B write want 200 got %d", rec.Code)
	}

	pa, foundA, _ := store.GetUserBookProgress(ctx, userA.ID, book.ID)
	pb, foundB, _ := store.GetUserBookProgress(ctx, userB.ID, book.ID)
	if !foundA || pa.LastReadPage.Int64 != 5 {
		t.Fatalf("A per-user progress want 5 got %+v (found=%v)", pa.LastReadPage, foundA)
	}
	if !foundB || pb.LastReadPage.Int64 != 8 {
		t.Fatalf("B per-user progress want 8 got %+v (found=%v)", pb.LastReadPage, foundB)
	}
	// 每用户写入绝不能污染全局 books 行。
	if gb, _ := store.GetBook(ctx, book.ID); gb.LastReadPage.Valid {
		t.Fatalf("per-user writes must not touch global books.last_read_page, got %+v", gb.LastReadPage)
	}
}

// ---- bulkSyncBookProgress：每用户 + 页码夹取 + 非法体 ----

func TestBulkSyncBookProgressPerUserAndClamp(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, _, book := seedBookFixture(t, store, rootDir, "Lib", "Series", "01.cbz", 10)
	ctx := context.Background()
	user := mkTestUser(t, store, "carol", database.RoleRegular)

	body, _ := json.Marshal(BulkSyncProgressRequest{Items: []BulkSyncProgressItem{
		{BookID: book.ID, Page: 999}, // 应夹取到 10
	}})
	req := withUserContext(httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body)), user)
	rec := httptest.NewRecorder()
	controller.bulkSyncBookProgress(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", rec.Code)
	}
	var resp struct {
		Updated int
		Results []BulkSyncProgressResultItem
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Updated != 1 || len(resp.Results) != 1 ||
		resp.Results[0].Status != "updated" || resp.Results[0].Page != 10 {
		t.Fatalf("clamp/per-user sync unexpected: %+v", resp)
	}
	p, found, _ := store.GetUserBookProgress(ctx, user.ID, book.ID)
	if !found || p.LastReadPage.Int64 != 10 {
		t.Fatalf("per-user progress want 10 got %+v (found=%v)", p.LastReadPage, found)
	}
	if gb, _ := store.GetBook(ctx, book.ID); gb.LastReadPage.Valid {
		t.Fatalf("global books row must be untouched, got %+v", gb.LastReadPage)
	}
}

func TestBulkSyncBookProgressInvalidPayload(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	rec := httptest.NewRecorder()
	controller.bulkSyncBookProgress(rec, httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte(`{bad`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid payload want 400 got %d", rec.Code)
	}
}

// ---- addBookReadingTime：uid==0 空操作 / 秒数封顶 / 缺书优雅 200 / 非法入参 ----

func TestAddBookReadingTimeBehavior(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 10)
	ctx := context.Background()
	user := mkTestUser(t, store, "dave", database.RoleRegular)
	bid := strconv.FormatInt(book.ID, 10)

	call := func(u *database.User, bookIDParam, body string) *httptest.ResponseRecorder {
		req := requestWithRouteParam(http.MethodPost, "/x", []byte(body), "bookId", bookIDParam)
		if u != nil {
			req = withUserContext(req, *u)
		}
		rec := httptest.NewRecorder()
		controller.addBookReadingTime(rec, req)
		return rec
	}
	totalFor := func(u database.User) int64 {
		total, _ := store.GetUserTotalReadingTime(ctx, u.ID)
		return total
	}

	// uid==0（未登录）：静默接受，不落库。
	if rec := call(nil, bid, `{"seconds":100}`); rec.Code != http.StatusOK {
		t.Fatalf("anon want 200 got %d", rec.Code)
	}
	if totalFor(user) != 0 {
		t.Fatalf("anon should not record, got %d", totalFor(user))
	}

	// 非法 book id -> 400。
	if rec := call(&user, "abc", `{"seconds":30}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid id want 400 got %d", rec.Code)
	}
	// seconds<=0 -> 空操作 200。
	if rec := call(&user, bid, `{"seconds":0}`); rec.Code != http.StatusOK {
		t.Fatalf("zero seconds want 200 got %d", rec.Code)
	}
	if totalFor(user) != 0 {
		t.Fatalf("zero seconds should not record, got %d", totalFor(user))
	}
	// 缺书（阅读期间被删/重扫）-> 优雅 200 不落库。
	if rec := call(&user, "999999", `{"seconds":50}`); rec.Code != http.StatusOK {
		t.Fatalf("missing book want graceful 200 got %d", rec.Code)
	}
	if totalFor(user) != 0 {
		t.Fatalf("missing book should not record, got %d", totalFor(user))
	}
	// 秒数封顶到 maxReadingTimeReportSeconds。
	if rec := call(&user, bid, `{"seconds":99999}`); rec.Code != http.StatusOK {
		t.Fatalf("cap want 200 got %d", rec.Code)
	}
	if got := totalFor(user); got != maxReadingTimeReportSeconds {
		t.Fatalf("seconds should cap at %d, got %d", maxReadingTimeReportSeconds, got)
	}
}

// TestReadingTimeEndpointSessionAndCsrfExempt 经真实路由验证：reading-time 虽 CSRF 豁免，
// 仍需有效会话；对照非豁免的 progress 端点缺 CSRF 应 403。
func TestReadingTimeEndpointSessionAndCsrfExempt(t *testing.T) {
	c, store, _, rootDir := newTestController(t)
	ctx := context.Background()
	_, _, book := seedBookFixture(t, store, rootDir, "Lib", "Series", "book.cbz", 10)
	admin := mkTestUser(t, store, "admin", database.RoleAdmin)

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	rtURL := srv.URL + "/api/books/" + strconv.FormatInt(book.ID, 10) + "/reading-time"

	// 已有账户但无会话 -> 401（会话仍是前置条件）。
	if resp, _ := authDo(t, newAuthClient(t), http.MethodPost, rtURL, "", map[string]int{"seconds": 30}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reading-time without session want 401 got %d", resp.StatusCode)
	}

	// 登录管理员。
	cl := newAuthClient(t)
	loginResp, _ := authDo(t, cl, http.MethodPost, srv.URL+"/api/auth/login", "", map[string]string{"username": "admin", "password": "password1"})
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("admin login want 200 got %d", loginResp.StatusCode)
	}

	// 带会话但不带 CSRF 头 -> 200（豁免）。
	if resp, _ := authDo(t, cl, http.MethodPost, rtURL, "", map[string]int{"seconds": 45}); resp.StatusCode != http.StatusOK {
		t.Fatalf("csrf-exempt reading-time want 200 got %d", resp.StatusCode)
	}
	if total, _ := store.GetUserTotalReadingTime(ctx, admin.ID); total != 45 {
		t.Fatalf("reading-time should record 45 for admin, got %d", total)
	}

	// 对照：非豁免的 progress 端点缺 CSRF -> 403。
	progURL := srv.URL + "/api/books/" + strconv.FormatInt(book.ID, 10) + "/progress"
	if resp, _ := authDo(t, cl, http.MethodPost, progURL, "", map[string]int{"page": 3}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("progress without csrf should 403 (not exempt), got %d", resp.StatusCode)
	}
}

// ---- 个人系列短评：评分区间 / 清空 / 每用户 / GET / DELETE ----

func TestSeriesReviewRatingValidationAndClearOnEmpty(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Lib", "Series", "book.cbz", 10)
	ctx := context.Background()
	user := mkTestUser(t, store, "erin", database.RoleRegular)
	sid := strconv.FormatInt(series.ID, 10)

	put := func(u *database.User, body string) *httptest.ResponseRecorder {
		req := requestWithRouteParam(http.MethodPut, "/x", []byte(body), "seriesId", sid)
		if u != nil {
			req = withUserContext(req, *u)
		}
		rec := httptest.NewRecorder()
		controller.putSeriesReview(rec, req)
		return rec
	}

	// 评分越界 -> 400（该校验在 uid 判断之前）。
	for _, bad := range []string{`{"rating":6}`, `{"rating":-1}`, `{"rating":5.5}`} {
		if rec := put(&user, bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("rating %s want 400 got %d", bad, rec.Code)
		}
	}
	// 非法 series id -> 400。
	badRec := httptest.NewRecorder()
	controller.putSeriesReview(badRec, withUserContext(requestWithRouteParam(http.MethodPut, "/x", []byte(`{"rating":3}`), "seriesId", "abc"), user))
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid series id want 400 got %d", badRec.Code)
	}

	// 边界评分 0 与 5 均接受。
	if rec := put(&user, `{"rating":0,"review":"meh"}`); rec.Code != http.StatusOK {
		t.Fatalf("rating 0 want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := put(&user, `{"rating":5,"review":"great"}`); rec.Code != http.StatusOK {
		t.Fatalf("rating 5 want 200 got %d", rec.Code)
	}
	rv, found, _ := store.GetUserSeriesReview(ctx, user.ID, series.ID)
	if !found || !rv.Rating.Valid || rv.Rating.Float64 != 5 || rv.Review != "great" {
		t.Fatalf("stored review unexpected: %+v (found=%v)", rv, found)
	}

	// 评分空 + 短评仅空白 -> 清空该条，exists=false，且库中删除。
	rec := put(&user, `{"review":"   "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear want 200 got %d", rec.Code)
	}
	var resp seriesReviewResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Exists {
		t.Fatalf("empty review should clear -> exists=false, got %+v", resp)
	}
	if _, found, _ := store.GetUserSeriesReview(ctx, user.ID, series.ID); found {
		t.Fatal("review should be deleted from store")
	}

	// 未登录 PUT 为空操作，返回 exists=false，且不落库。
	anon := put(nil, `{"rating":3,"review":"anon"}`)
	if anon.Code != http.StatusOK {
		t.Fatalf("anon put want 200 got %d", anon.Code)
	}
	_ = json.Unmarshal(anon.Body.Bytes(), &resp)
	if resp.Exists {
		t.Fatalf("anon put should be no-op exists=false, got %+v", resp)
	}
}

func TestSeriesReviewPerUserGetAndDelete(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Lib", "Series", "book.cbz", 10)
	ctx := context.Background()
	userA := mkTestUser(t, store, "amy", database.RoleRegular)
	userB := mkTestUser(t, store, "ben", database.RoleRegular)
	sid := strconv.FormatInt(series.ID, 10)

	ratingA := 4.5
	if err := store.UpsertUserSeriesReview(ctx, userA.ID, series.ID, &ratingA, "loved it"); err != nil {
		t.Fatalf("seed review: %v", err)
	}

	get := func(u *database.User) seriesReviewResponse {
		req := requestWithRouteParam(http.MethodGet, "/x", nil, "seriesId", sid)
		if u != nil {
			req = withUserContext(req, *u)
		}
		rec := httptest.NewRecorder()
		controller.getSeriesReview(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get review want 200 got %d", rec.Code)
		}
		var resp seriesReviewResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp
	}

	if ra := get(&userA); !ra.Exists || ra.Rating == nil || *ra.Rating != 4.5 || ra.Review != "loved it" {
		t.Fatalf("A should see own review, got %+v", ra)
	}
	if rb := get(&userB); rb.Exists {
		t.Fatalf("B must not see A's review, got %+v", rb)
	}
	if anon := get(nil); anon.Exists {
		t.Fatalf("anon should get exists=false, got %+v", anon)
	}

	// A 删除自己的短评。
	delRec := httptest.NewRecorder()
	controller.deleteSeriesReview(delRec, withUserContext(requestWithRouteParam(http.MethodDelete, "/x", nil, "seriesId", sid), userA))
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete want 200 got %d", delRec.Code)
	}
	if _, found, _ := store.GetUserSeriesReview(ctx, userA.ID, series.ID); found {
		t.Fatal("A review should be gone after delete")
	}
}

// ---- 深度统计：uid==0 零值 与 uid>0 每用户双路径 ----

func TestStatsAnonymousZeroValues(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	streakRec := httptest.NewRecorder()
	controller.getReadingStreak(streakRec, httptest.NewRequest(http.MethodGet, "/api/stats/streak", nil))
	var streak map[string]int
	_ = json.Unmarshal(streakRec.Body.Bytes(), &streak)
	if streakRec.Code != http.StatusOK || streak["current"] != 0 || streak["longest"] != 0 {
		t.Fatalf("anon streak unexpected: code=%d %+v", streakRec.Code, streak)
	}

	rtRec := httptest.NewRecorder()
	controller.getReadingTimeStats(rtRec, httptest.NewRequest(http.MethodGet, "/api/stats/reading-time", nil))
	var rt struct {
		TotalSeconds int64                         `json:"total_seconds"`
		Top          []database.BookReadingTimeRow `json:"top"`
	}
	_ = json.Unmarshal(rtRec.Body.Bytes(), &rt)
	if rtRec.Code != http.StatusOK || rt.TotalSeconds != 0 || len(rt.Top) != 0 {
		t.Fatalf("anon reading-time unexpected: code=%d %+v", rtRec.Code, rt)
	}
	if rt.Top == nil {
		t.Fatal("top must serialize as [] not null")
	}

	perRec := httptest.NewRecorder()
	controller.getPeriodStats(perRec, httptest.NewRequest(http.MethodGet, "/api/stats/period?year=2024&month=3", nil))
	var period database.UserPeriodStats
	_ = json.Unmarshal(perRec.Body.Bytes(), &period)
	if perRec.Code != http.StatusOK || period.TopSeries == nil || len(period.TopSeries) != 0 {
		t.Fatalf("anon period unexpected: code=%d %+v", perRec.Code, period)
	}
}

func TestReadingStatsDualPathPerUser(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	ctx := context.Background()
	_, _, book := seedBookFixture(t, store, rootDir, "Lib", "Series", "book.cbz", 10)
	userA := mkTestUser(t, store, "ua", database.RoleRegular)
	userB := mkTestUser(t, store, "ub", database.RoleRegular)

	if err := store.AddUserBookReadingTime(ctx, userA.ID, book.ID, 120); err != nil {
		t.Fatalf("add user reading time: %v", err)
	}
	// 全局与每用户活动分别写入，验证热力图按 uid 分流。
	if err := store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: book.ID, PagesRead: 5}); err != nil {
		t.Fatalf("log global activity: %v", err)
	}
	if err := store.LogUserReadingActivity(ctx, userA.ID, book.ID, 3); err != nil {
		t.Fatalf("log user activity: %v", err)
	}

	readingTime := func(u *database.User) (int64, int) {
		req := httptest.NewRequest(http.MethodGet, "/api/stats/reading-time", nil)
		if u != nil {
			req = withUserContext(req, *u)
		}
		rec := httptest.NewRecorder()
		controller.getReadingTimeStats(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("reading-time want 200 got %d", rec.Code)
		}
		var rt struct {
			TotalSeconds int64                         `json:"total_seconds"`
			Top          []database.BookReadingTimeRow `json:"top"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &rt)
		return rt.TotalSeconds, len(rt.Top)
	}
	if ts, n := readingTime(&userA); ts != 120 || n != 1 {
		t.Fatalf("A reading-time want 120/1 got %d/%d", ts, n)
	}
	if ts, _ := readingTime(&userB); ts != 0 {
		t.Fatalf("B reading-time want 0 got %d", ts)
	}
	if ts, _ := readingTime(nil); ts != 0 {
		t.Fatalf("anon reading-time want 0 got %d", ts)
	}

	heatTotal := func(u *database.User, weeks string) int {
		url := "/api/stats/activity-heatmap"
		if weeks != "" {
			url += "?weeks=" + weeks
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		if u != nil {
			req = withUserContext(req, *u)
		}
		rec := httptest.NewRecorder()
		controller.getActivityHeatmap(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("heatmap want 200 got %d", rec.Code)
		}
		var days []database.ActivityDay
		_ = json.Unmarshal(rec.Body.Bytes(), &days)
		total := 0
		for _, d := range days {
			total += d.PageCount
		}
		return total
	}
	// uid==0 走全局表(=5)；userA 走每用户表(=3)；userB 无记录(=0)。weeks 越界(>52)夹回默认仍含今日。
	if got := heatTotal(nil, ""); got != 5 {
		t.Fatalf("global heatmap want 5 got %d", got)
	}
	if got := heatTotal(&userA, "52"); got != 3 {
		t.Fatalf("A heatmap want 3 got %d", got)
	}
	if got := heatTotal(&userB, "100"); got != 0 {
		t.Fatalf("B heatmap want 0 got %d", got)
	}
}

// ---- 角色写权限：普通用户个人写放行，管理写拒绝 ----

func TestRegularUserPersonalWritesVsAdminWrites(t *testing.T) {
	c, store, _, rootDir := newTestController(t)
	ctx := context.Background()
	_, series, book := seedBookFixture(t, store, rootDir, "Lib", "Series", "book.cbz", 10)
	_ = mkTestUser(t, store, "admin", database.RoleAdmin)
	reg := mkTestUser(t, store, "reg", database.RoleRegular)

	r := chi.NewRouter()
	c.SetupRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	cl := newAuthClient(t)
	resp, data := authDo(t, cl, http.MethodPost, srv.URL+"/api/auth/login", "", map[string]string{"username": "reg", "password": "password1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("regular login want 200 got %d", resp.StatusCode)
	}
	var sr authSessionResponse
	_ = json.Unmarshal(data, &sr)
	csrf := sr.CSRFToken
	if csrf == "" {
		t.Fatal("expected csrf token from login")
	}

	sid := strconv.FormatInt(series.ID, 10)
	bid := strconv.FormatInt(book.ID, 10)

	// 放行：个人短评、单本进度、批量进度。
	if resp, _ := authDo(t, cl, http.MethodPut, srv.URL+"/api/series/"+sid+"/review", csrf, map[string]any{"rating": 4, "review": "nice"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("regular review PUT want 200 got %d", resp.StatusCode)
	}
	if resp, _ := authDo(t, cl, http.MethodPost, srv.URL+"/api/books/"+bid+"/progress", csrf, map[string]int{"page": 3}); resp.StatusCode != http.StatusOK {
		t.Fatalf("regular progress POST want 200 got %d", resp.StatusCode)
	}
	if resp, _ := authDo(t, cl, http.MethodPost, srv.URL+"/api/books/bulk-progress", csrf, map[string]any{"book_ids": []int64{book.ID}, "is_read": true}); resp.StatusCode != http.StatusOK {
		t.Fatalf("regular bulk-progress want 200 got %d", resp.StatusCode)
	}

	// 拒绝：建库、系列刮削、删除书（管理写）。
	if resp, _ := authDo(t, cl, http.MethodPost, srv.URL+"/api/libraries", csrf, map[string]any{"name": "X", "path": rootDir}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("regular create library want 403 got %d", resp.StatusCode)
	}
	if resp, _ := authDo(t, cl, http.MethodPost, srv.URL+"/api/series/"+sid+"/scrape", csrf, map[string]any{}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("regular series scrape want 403 got %d", resp.StatusCode)
	}
	if resp, _ := authDo(t, cl, http.MethodPost, srv.URL+"/api/books/remove", csrf, map[string]any{"book_ids": []int64{book.ID}}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("regular book remove want 403 got %d", resp.StatusCode)
	}

	// 个人短评确实写入 regular 名下。
	if _, found, _ := store.GetUserSeriesReview(ctx, reg.ID, series.ID); !found {
		t.Fatal("regular user's review should be stored under their own id")
	}
}

// ---- 阅读协议 OPDS：Basic 鉴权 + 关闭优先于鉴权 ----

func TestOPDSProtocolBasicAuth(t *testing.T) {
	c, store, _, rootDir := newTestController(t)
	ctx := context.Background()
	seedBookFixture(t, store, rootDir, "Lib", "Series", "book.cbz", 10)

	cfg := c.currentConfig()
	cfg.Protocols.OPDS.Enabled = true
	c.config.Replace(&cfg)

	r := chi.NewRouter()
	c.SetupOPDSRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()
	url := srv.URL + "/opds/v1.2/"

	get := func(setCreds func(*http.Request)) int {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		if setCreds != nil {
			setCreds(req)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// 首启（无账户）：直通 200。
	if code := get(nil); code != http.StatusOK {
		t.Fatalf("setup-mode OPDS should pass, got %d", code)
	}

	// 建账户后锁定。
	hash, _ := hashPassword("password1")
	if _, err := store.CreateUser(ctx, database.CreateUserParams{Username: "reader", PasswordHash: hash, Role: database.RoleRegular}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if code := get(nil); code != http.StatusUnauthorized {
		t.Fatalf("no creds want 401 got %d", code)
	}
	if code := get(func(req *http.Request) { req.SetBasicAuth("reader", "wrong") }); code != http.StatusUnauthorized {
		t.Fatalf("wrong creds want 401 got %d", code)
	}
	if code := get(func(req *http.Request) { req.SetBasicAuth("reader", "password1") }); code != http.StatusOK {
		t.Fatalf("valid creds want 200 got %d", code)
	}

	// 协议关闭优先于鉴权：即便凭据正确也 404。
	cfg2 := c.currentConfig()
	cfg2.Protocols.OPDS.Enabled = false
	c.config.Replace(&cfg2)
	if code := get(func(req *http.Request) { req.SetBasicAuth("reader", "password1") }); code != http.StatusNotFound {
		t.Fatalf("disabled OPDS want 404 even with valid creds, got %d", code)
	}
}
