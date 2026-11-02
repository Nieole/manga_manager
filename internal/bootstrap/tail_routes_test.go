package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/bootstrap"
	"manga-manager/internal/database"

	"github.com/go-chi/chi/v5"
)

// The final Host composes catalogue writes, content and all four families in
// the same gated subtrees. Real archive bytes also cross its content boundary.
func TestHostTailRoutesShareAuthenticationAndContent(t *testing.T) {
	store, cfg := fixture(t)
	ctx := context.Background()
	root := t.TempDir()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Tail library", Path: root, ScanMode: "none", ScanFormats: "cbz"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Tail series", Path: filepath.Join(root, "absent")})
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var picture bytes.Buffer
	if err := png.Encode(&picture, img); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	page, err := z.Create("01.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Write(picture.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "book.cbz")
	if err := os.WriteFile(path, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	book, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "book.cbz", Path: path, Size: int64(archive.Len()), PageCount: 1, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	process.HTTP.SetupOPDSRoutes(router)
	process.HTTP.SetupKOReaderRoutes(router)
	var cookie *http.Cookie
	var csrf string
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != nil {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
		return rec
	}
	session := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		var body struct {
			CSRF string `json:"csrf_token"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.CSRF == "" || len(rec.Result().Cookies()) == 0 {
			t.Fatal("missing session")
		}
		cookie = rec.Result().Cookies()[0]
		csrf = body.CSRF
	}
	request("GET", "/api/libraries", "", 401)
	session(request("POST", "/api/auth/setup", `{"username":"admin","password":"correct horse battery staple"}`, 200))
	sid, bid := fmt.Sprint(series.ID), fmt.Sprint(book.ID)
	request("POST", "/api/series/bulk-update", `{"series_ids":[`+sid+`],"is_favorite":true}`, 200)
	if rec := request("GET", "/api/series/info/"+sid, "", 200); !strings.Contains(rec.Body.String(), `"is_favorite":true`) {
		t.Fatal(rec.Body.String())
	}
	request("PUT", "/api/series/"+sid+"/custom-fields", `{"fields":[{"key":"edition","value":"first"}]}`, 200)
	if rec := request("GET", "/api/series/"+sid+"/custom-fields", "", 200); !strings.Contains(rec.Body.String(), "first") {
		t.Fatal(rec.Body.String())
	}
	request("POST", "/api/series/"+sid+"/open-dir", "", 400) // no desktop process in the test
	request("POST", "/api/series/bulk-edit", `{"series_ids":[`+sid+`],"add_tags":["Before"]}`, 200)
	tags := request("GET", "/api/series/"+sid+"/tags", "", 200)
	var listed []database.Tag
	if err := json.Unmarshal(tags.Body.Bytes(), &listed); err != nil || len(listed) != 1 {
		t.Fatalf("tags=%s err=%v", tags.Body.String(), err)
	}
	tid := fmt.Sprint(listed[0].ID)
	request("PATCH", "/api/tags/"+tid, `{"name":"After"}`, 200)
	if rec := request("GET", "/api/series/"+sid+"/tags", "", 200); !strings.Contains(rec.Body.String(), "After") {
		t.Fatal(rec.Body.String())
	}
	request("DELETE", "/api/tags/"+tid, "", 200)
	request("GET", "/api/pages/"+bid, "", 200)
	request("GET", "/api/pages/"+bid+"/1?format=png", "", 200)
	if rec := request("GET", "/api/books/"+bid+"/file", "", 200); !bytes.Equal(rec.Body.Bytes(), archive.Bytes()) {
		t.Fatal("download bytes changed")
	}
	request("POST", "/api/books/"+bid+"/cover", `{"page":1}`, 200)
	request("GET", "/api/covers/"+bid, "", 200)
	cover, err := store.GetBookCoverPath(ctx, book.ID)
	if err != nil || cover == "" {
		t.Fatalf("cover=%q err=%v", cover, err)
	}
	if rec := request("GET", "/api/thumbnails/"+cover, "", 200); rec.Header().Get("ETag") == "" {
		t.Fatal("thumbnail ETag missing")
	}
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	part, err := writer.CreateFormFile("file", "cover.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(picture.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/books/"+bid+"/cover/upload", &upload)
	req.AddCookie(cookie)
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("upload=%d %s", rec.Code, rec.Body.String())
	}
	request("GET", "/api/system/page-cache", "", 200)
	request("DELETE", "/api/system/page-cache", "", 200)
	request("GET", "/api/metadata/providers", "", 200)
	request("GET", "/api/collections/", "", 200)
	request("GET", "/api/system/tasks", "", 200)
	goodCSRF := csrf
	csrf = "wrong"
	request("POST", "/api/series/bulk-update", `{"series_ids":[`+sid+`],"is_favorite":false}`, 403)
	csrf = goodCSRF
	admin, err := store.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(ctx, database.CreateUserParams{Username: "reader", PasswordHash: admin.PasswordHash, Role: database.RoleRegular}); err != nil {
		t.Fatal(err)
	}
	session(request("POST", "/api/auth/login", `{"username":"reader","password":"correct horse battery staple"}`, 200))
	request("GET", "/api/pages/"+bid, "", 200)
	request("GET", "/api/books/"+bid+"/file", "", 200)
	request("POST", "/api/books/"+bid+"/cover", `{"page":1}`, 403)
	request("PUT", "/api/series/"+sid+"/custom-fields", `{"fields":[]}`, 403)
	request("GET", "/api/system/page-cache", "", 403)
	request("GET", "/api/books/duplicates", "", 403)
	rec = request("GET", "/api/libraries", "", 200)
	var libraries []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &libraries); err != nil || len(libraries) != 1 || libraries[0].Path != "" {
		t.Fatalf("private library paths: %s err=%v", rec.Body.String(), err)
	}
}
