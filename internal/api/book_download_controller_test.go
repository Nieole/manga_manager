// 本文件是业务回归测试，验证整卷归档下载路由（FIX M38）以正确 MIME、附件文件名与
// Range 支持下发原始 CBZ/CBR/PDF，供非 PSE 的 OPDS 客户端整卷下载。

package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"

	"github.com/go-chi/chi/v5"
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

func TestProtocolWholeBookRangeAndAuthentication(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, _, book := seedBookFixture(t, store, root, "Protocol", "Series", "book.pdf", 1)
	if err := os.WriteFile(book.Path, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := c.currentConfig()
	cfg.Protocols.OPDS.Enabled, cfg.Protocols.Mihon.Enabled = true, true
	c.config.Replace(&cfg)
	hash, err := hashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(context.Background(), database.CreateUserParams{Username: "reader", PasswordHash: hash, Role: database.RoleRegular}); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	c.SetupRoutes(router)
	c.SetupOPDSRoutes(router)
	for _, prefix := range []string{opdsResourceBase, mihonResourceBase} {
		t.Run(prefix, func(t *testing.T) {
			url := prefix + "/books/" + strconv.FormatInt(book.ID, 10) + "/file"
			for _, authenticated := range []bool{false, true} {
				req := httptest.NewRequest(http.MethodGet, url, nil)
				req.Header.Set("Range", "bytes=2-5")
				if authenticated {
					req.SetBasicAuth("reader", "password1")
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if !authenticated {
					if rec.Code != http.StatusUnauthorized {
						t.Fatalf("unauthenticated: %d", rec.Code)
					}
					continue
				}
				if rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" || rec.Header().Get("Content-Range") != "bytes 2-5/10" || rec.Header().Get("Content-Type") != "application/octet-stream" {
					t.Fatalf("range: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
				}
				if !strings.Contains(rec.Header().Get("Content-Disposition"), "filename*=UTF-8''book.pdf") {
					t.Fatalf("attachment: %v", rec.Header())
				}
				if parser.ActiveArchiveBorrows(book.Path) != 0 {
					t.Fatal("range retained borrow")
				}
			}
		})
	}
}

type downloadFlushGate struct {
	http.ResponseWriter
	entered chan struct{}
	resume  chan struct{}
	blocked bool
}

func (w *downloadFlushGate) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if !w.blocked {
		w.blocked = true
		w.ResponseWriter.(http.Flusher).Flush()
		close(w.entered)
		<-w.resume
	}
	return n, err
}

func TestWholeBookClientDisconnectReturnsAccess(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "Disconnect", "Series", "book.cbz", 1)
	if err := os.WriteFile(book.Path, bytes.Repeat([]byte("archive bytes"), 1<<20), 0600); err != nil {
		t.Fatal(err)
	}
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	router := chi.NewRouter()
	router.Get("/books/{bookId}/file", c.serveBookFile)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		router.ServeHTTP(&downloadFlushGate{ResponseWriter: w, entered: entered, resume: resume}, r)
	}))
	defer srv.Close()
	defer func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
	}()
	resp, err := srv.Client().Get(srv.URL + "/books/" + strconv.FormatInt(book.ID, 10) + "/file")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	buf := make([]byte, 1)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatal(err)
	}
	if got := parser.ActiveArchiveBorrows(book.Path); got != 1 {
		t.Errorf("active stream borrow: %d", got)
	}
	if release, err := c.acquireLibraryExclusive(context.Background(), lib.ID); err == nil {
		release()
		t.Error("active download allowed exclusive access")
	}
	_ = resp.Body.Close()
	close(resume)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("disconnected handler did not exit")
	}
	if got := parser.ActiveArchiveBorrows(book.Path); got != 0 {
		t.Fatalf("disconnect retained borrow: %d", got)
	}
	release, err := c.acquireLibraryExclusive(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
