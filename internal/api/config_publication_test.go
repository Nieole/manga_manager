package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestConfigurationSaveReportsDeferredEffectsSeparatelyFromPersistence(t *testing.T) {
	c, _, _, _ := newTestController(t)
	before := c.currentConfig()
	if err := c.admin.persistConfig(&before); err != nil {
		t.Fatal(err)
	}
	next := before
	next.Server.Port++
	next.Logging.Level = "debug"
	next.Scanner.Workers = 7
	data, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	req := requestWithRouteParam(http.MethodPost, "/config", data, "", "")
	req.Header.Set("If-Match", c.config.Version(before))
	rec := httptest.NewRecorder()
	c.admin.updateSystemConfig(rec, req)
	var response struct {
		Publication struct {
			Saved           bool     `json:"saved"`
			Published       bool     `json:"published"`
			Immediate       []string `json:"immediate"`
			NextOperation   []string `json:"next_operation"`
			RestartRequired []string `json:"restart_required"`
		} `json:"publication"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !response.Publication.Saved || !response.Publication.Published {
		t.Fatalf("save facts=%d %s", rec.Code, rec.Body.String())
	}
	if !slices.Contains(response.Publication.RestartRequired, "server.port") || !slices.Contains(response.Publication.NextOperation, "scanner.workers") || !slices.Contains(response.Publication.Immediate, "logging.level") {
		t.Fatalf("effect facts=%s", rec.Body.String())
	}
}

func TestPendingDatabaseSettingDoesNotRedirectActualTrashTarget(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, _, book := seedBookFixture(t, store, root, "Config", "Series", "book.cbz", 1)
	if err := os.WriteFile(book.Path, []byte("original book"), 0644); err != nil {
		t.Fatal(err)
	}
	before := c.currentConfig()
	next := c.currentConfig()
	next.Database.Path = filepath.Join(t.TempDir(), "future.db")
	if err := c.admin.persistConfig(&next); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"book_ids": []int64{book.ID}, "move_to_trash": true})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	c.families.removal.removeBooks(rec, requestWithRouteParam(http.MethodPost, "/remove", body, "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("remove=%d %s", rec.Code, rec.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(before.Database.Path), "trash"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("actual database trash target changed: %v %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(next.Database.Path), "trash")); !os.IsNotExist(err) {
		t.Fatalf("pending restart redirected writes: %v", err)
	}
}
