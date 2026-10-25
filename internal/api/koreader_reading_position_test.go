package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/koreader"
)

func TestMatchedKOReaderProtocolUpdatesPositionAndInvalidatesDashboard(t *testing.T) {
	c, store, _, root := newTestController(t)
	ctx := context.Background()
	cfg := c.currentConfig()
	cfg.KOReader.Enabled = true
	cfg.KOReader.MatchMode = config.KOReaderMatchModeFilePath
	c.config.Replace(&cfg)
	lib, _, book := seedBookFixture(t, store, root, "Lib", "Series", "Book.cbz", 32)
	if err := store.UpdateBookIdentity(ctx, database.UpdateBookIdentityParams{ID: book.ID, PathFingerprint: koreader.FingerprintRelativePath(lib.Path, book.Path, false), PathFingerprintNoExt: koreader.FingerprintRelativePath(lib.Path, book.Path, true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateKOReaderAccount(ctx, database.CreateKOReaderAccountParams{Username: "device", SyncKey: "secret", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	dashboard := func() database.DashboardStats {
		t.Helper()
		rec := httptest.NewRecorder()
		c.getDashboardStats(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/stats", nil))
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var stats database.DashboardStats
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatal(err)
		}
		return stats
	}
	if got := dashboard().ReadBooks; got != 0 {
		t.Fatalf("initial read count=%d", got)
	}
	for _, percentage := range []float64{.75, .25} {
		payload, _ := json.Marshal(koreader.ProgressPayload{Document: "Series/Book.cbz", Progress: "/page", Percentage: percentage, Device: "reader", DeviceID: "one"})
		req := httptest.NewRequest(http.MethodPut, "/syncs/progress", bytes.NewReader(payload))
		req.Header.Set("x-auth-user", "device")
		req.Header.Set("x-auth-key", koreader.HashKey("secret"))
		req.Header.Set("Accept", "application/vnd.koreader.v1+json")
		rec := httptest.NewRecorder()
		c.koreaderUpdateProgress(rec, req)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/vnd.koreader.v1+json" || strings.Contains(rec.Body.String(), "received_at_ns") {
			t.Fatalf("protocol response=%d %s", rec.Code, rec.Body.String())
		}
		var response map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response["state"] != "OK" || response["timestamp"] == nil || len(response) != 3 {
			t.Fatalf("wire contract=%+v", response)
		}
	}
	updated, err := store.GetBook(ctx, book.ID)
	if err != nil || updated.LastReadPage.Int64 != 8 {
		t.Fatalf("book=%+v err=%v", updated, err)
	}
	if got := dashboard().ReadBooks; got != 1 {
		t.Fatalf("production reading invalidation missing, cached count=%d", got)
	}
}
