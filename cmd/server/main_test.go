package main

import (
	"net/http/httptest"
	"testing"
)

func TestStaticCacheControl(t *testing.T) {
	t.Run("index html stays revalidatable", func(t *testing.T) {
		if got := staticCacheControl("/index.html"); got != "no-cache" {
			t.Fatalf("expected index cache-control no-cache, got %q", got)
		}
	})

	t.Run("hashed assets are long-lived", func(t *testing.T) {
		got := staticCacheControl("/assets/index-CRCnYWro.js")
		want := "public, max-age=31536000, immutable"
		if got != want {
			t.Fatalf("expected asset cache-control %q, got %q", want, got)
		}
	})

	t.Run("root level files stay revalidatable", func(t *testing.T) {
		if got := staticCacheControl("/favicon.ico"); got != "no-cache" {
			t.Fatalf("expected root asset cache-control no-cache, got %q", got)
		}
	})
}

func TestSetStaticResponseHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	setStaticResponseHeaders(rec, "/assets/index-CRCnYWro.js")

	if got := rec.Header().Get("Content-Type"); got != "application/javascript" {
		t.Fatalf("expected javascript content-type, got %q", got)
	}

	wantCacheControl := "public, max-age=31536000, immutable"
	if got := rec.Header().Get("Cache-Control"); got != wantCacheControl {
		t.Fatalf("expected cache-control %q, got %q", wantCacheControl, got)
	}
}
