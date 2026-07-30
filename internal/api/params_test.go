// 业务说明：本文件是业务回归测试，属于后端 HTTP API 层，验证分页与列表规模参数的边界钳制。
// 它锁住「无上界分页导致 offset 溢出 / int32 截断」与「limit 无上限物化整库」两类缺陷。
// 维护时应保证任何新增的分页端点都复用 params.go 的助手，并在此补上对应断言。

package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestClampLimit(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		def, max int
		want     int
	}{
		{"empty falls back to default", "", 30, 100, 30},
		{"garbage falls back to default", "abc", 30, 100, 30},
		{"zero falls back to default", "0", 30, 100, 30},
		{"negative falls back to default", "-5", 30, 100, 30},
		{"in range passes through", "42", 30, 100, 42},
		{"above max is truncated", "1000000", 30, 100, 100},
		{"max<=0 falls back to global cap", "1000000", 30, 0, maxListLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampLimit(tc.raw, tc.def, tc.max); got != tc.want {
				t.Fatalf("clampLimit(%q, %d, %d) = %d, want %d", tc.raw, tc.def, tc.max, got, tc.want)
			}
		})
	}
}

func TestClampPageAndOffset(t *testing.T) {
	if got := clampPage(""); got != 1 {
		t.Fatalf("empty page should default to 1, got %d", got)
	}
	if got := clampPage("-3"); got != 1 {
		t.Fatalf("negative page should clamp to 1, got %d", got)
	}
	if got := clampPage(strconv.Itoa(maxPageNumber * 10)); got != maxPageNumber {
		t.Fatalf("huge page should clamp to %d, got %d", maxPageNumber, got)
	}
	if got := clampOffset("-1"); got != 0 {
		t.Fatalf("negative offset should clamp to 0, got %d", got)
	}
	if got := clampOffset(strconv.Itoa(maxListOffset * 10)); got != maxListOffset {
		t.Fatalf("huge offset should clamp to %d, got %d", maxListOffset, got)
	}
}

// TestPageOffsetNeverOverflows 锁住 offset 计算的安全性。
// 旧代码写 int64((page-1)*limit)：乘法先在 int 里完成，超大 page 会溢出成负数，
// OPDS 侧拿负 start 去切片直接 panic，Mihon 侧经 int32 截断静默返回第一页。
func TestPageOffsetNeverOverflows(t *testing.T) {
	if got := pageOffset(maxPageNumber, maxListLimit); got < 0 {
		t.Fatalf("pageOffset overflowed to %d", got)
	}
	if got := pageOffset(1, 30); got != 0 {
		t.Fatalf("first page should have offset 0, got %d", got)
	}
	if got := pageOffset(3, 20); got != 40 {
		t.Fatalf("page 3 of 20 should be offset 40, got %d", got)
	}
	if got := pageOffset(0, 0); got != 0 {
		t.Fatalf("degenerate input should yield 0, got %d", got)
	}
}

// TestOpdsSliceBoundsStaysInRange 锁住 OPDS 内存分页的下标安全。
// 任何 (total, page, limit) 组合都必须满足 0 <= start <= end <= total，否则切片 panic。
func TestOpdsSliceBoundsStaysInRange(t *testing.T) {
	cases := []struct{ total, page, limit int }{
		{100, 1, 30},
		{100, 4, 30},
		{100, 999999, 30},
		{100, maxPageNumber, maxListLimit},
		{0, 5, 30},
		{10, 1, 0},
		{-1, 1, 30},
	}
	for _, tc := range cases {
		start, end := opdsSliceBounds(tc.total, tc.page, tc.limit)
		total := tc.total
		if total < 0 {
			total = 0
		}
		if start < 0 || end < start || end > total {
			t.Fatalf("opdsSliceBounds(%d,%d,%d) = (%d,%d): out of range for total %d",
				tc.total, tc.page, tc.limit, start, end, total)
		}
	}
}

// TestOpdsPositiveQueryIntBoundsPage 确认「max<=0 表示不设上限」这一危险语义已被移除。
//
// 用 int64 上限附近的 page 值：旧实现会把它原样放行，随后 (page-1)*limit 在 int 里
// 溢出成负数，opdsSliceBounds 拿负 start 去切片 → runtime panic。攻击者只需一次
// GET /opds/v1.2/recent?page=9223372036854775807 即可打挂协议端点。
func TestOpdsPositiveQueryIntBoundsPage(t *testing.T) {
	const overflowPage = "9223372036854775807" // math.MaxInt64
	req := httptest.NewRequest("GET", "/opds/v1.2/recent?page="+overflowPage, nil)
	page := opdsPositiveQueryInt(req, "page", 1, 0)
	if page > maxPageNumber {
		t.Fatalf("expected page to be capped at %d, got %d", maxPageNumber, page)
	}
	// 钳制后算出的下标必须仍然可安全用于切片。
	limit := opdsPositiveQueryInt(req, "limit", 30, 100)
	start, end := opdsSliceBounds(100, page, limit)
	if start < 0 || end < start || end > 100 {
		t.Fatalf("slice bounds unsafe after clamping: start=%d end=%d", start, end)
	}
}

func TestPositiveQueryIntBoundsPage(t *testing.T) {
	const overflowPage = "9223372036854775807"
	req := httptest.NewRequest("GET", "/api/mihon/v1/series?page="+overflowPage, nil)
	page := positiveQueryInt(req, "page", 1, 0)
	if page > maxPageNumber {
		t.Fatalf("expected page to be capped at %d, got %d", maxPageNumber, page)
	}
	if off := pageOffset(page, 30); off < 0 {
		t.Fatalf("offset overflowed to %d", off)
	}
}

// TestRequestBodyLimitRejectsOversizedPayload 锁住全局请求体闸门。
// 此前全站没有任何 body 上限，未鉴权的 /api/auth/login 可被单个巨型 JSON 打爆内存。
func TestRequestBodyLimitRejectsOversizedPayload(t *testing.T) {
	var readErr error
	handler := RequestBodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	oversized := bytes.Repeat([]byte("a"), int(defaultMaxRequestBody)+1024)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(oversized))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if readErr == nil {
		t.Fatal("expected oversized body to be rejected by MaxBytesReader")
	}

	readErr = nil
	small := bytes.Repeat([]byte("a"), 1024)
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(small))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if readErr != nil {
		t.Fatalf("expected normal body to pass, got %v", readErr)
	}
}

// TestRequestBodyLimitAllowsUploadEndpoints 确认上传端点走放宽档，不会被 1 MiB 闸门误伤。
func TestRequestBodyLimitAllowsUploadEndpoints(t *testing.T) {
	var readErr error
	handler := RequestBodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	payload := bytes.Repeat([]byte("a"), int(defaultMaxRequestBody)+1024)
	req := httptest.NewRequest(http.MethodPost, "/api/books/1/cover/upload", bytes.NewReader(payload))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if readErr != nil {
		t.Fatalf("upload endpoint should accept payloads above the default cap, got %v", readErr)
	}
}
