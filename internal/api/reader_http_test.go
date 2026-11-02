package api

import (
	"database/sql"
	"encoding/json"
	"manga-manager/internal/database"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The reader adapter is exercised without mounting the compatibility controller.
func TestReaderHTTPPositionAndRatingConsumers(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, series, book := seedBookFixture(t, store, root, "Library", "Series", "01.cbz", 10)
	user := mkTestUser(t, store, "reader-http", database.RoleRegular)
	r := chi.NewRouter()
	c.newReaderHTTP().SetupRoutes(r)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, withUserContext(httptest.NewRequest(method, path, strings.NewReader(body)), user))
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	id := strconv.FormatInt(book.ID, 10)
	request("POST", "/books/"+id+"/progress", `{"page":4}`)
	w := request("GET", "/book-info/"+id, "")
	var got struct {
		LastReadPage sql.NullInt64 `json:"last_read_page"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.LastReadPage.Int64 != 4 {
		t.Fatalf("book=%s err=%v", w.Body.String(), err)
	}
	sid := strconv.FormatInt(series.ID, 10)
	request("PUT", "/series/"+sid+"/review", `{"rating":4.5,"review":"thoughtful"}`)
	w = request("GET", "/series/"+sid+"/review", "")
	if !strings.Contains(w.Body.String(), "thoughtful") {
		t.Fatalf("review=%s", w.Body.String())
	}
}
