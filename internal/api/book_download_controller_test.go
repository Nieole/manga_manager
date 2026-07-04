// 业务说明：本文件是业务回归测试，验证整卷归档下载路由（FIX M38）以正确 MIME、附件文件名与
// Range 支持下发原始 CBZ/CBR/PDF，供非 PSE 的 OPDS 客户端整卷下载。
package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestServeBookFileWholeArchiveDownload(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, _, book := seedBookFixture(t, store, rootDir, "Library D", "Series Delta", "Delta 01.cbz", 8)

	// seedBookFixture 只建库行不落盘，这里写入真实归档字节到 book.Path。
	payload := []byte("PK\x03\x04 fake cbz whole-book bytes")
	if err := os.WriteFile(book.Path, payload, 0o644); err != nil {
		t.Fatalf("write cbz fixture failed: %v", err)
	}

	idStr := strconv.FormatInt(book.ID, 10)
	req := requestWithRouteParam(http.MethodGet, "/api/books/"+idStr+"/file", nil, "bookId", idStr)
	rec := httptest.NewRecorder()

	controller.serveBookFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.comicbook+zip" {
		t.Fatalf("expected comicbook zip content type, got %q", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, "filename*=UTF-8''Delta%2001.cbz") {
		t.Fatalf("unexpected content disposition: %q", cd)
	}
	if !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Fatalf("served bytes mismatch: got %q want %q", rec.Body.Bytes(), payload)
	}
	// http.ServeContent 应声明 Range 支持，便于阅读器断点续传。
	if ar := rec.Header().Get("Accept-Ranges"); ar != "bytes" {
		t.Fatalf("expected Accept-Ranges: bytes from ServeContent, got %q", ar)
	}
}
