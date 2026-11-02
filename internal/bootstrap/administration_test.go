package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	appruntime "manga-manager/internal/application/runtime"
	"manga-manager/internal/bootstrap"
	"manga-manager/internal/config"
)

// Standalone and combined routes must share the same publication and auth contracts.
func TestAdministrationHostPublishesConfigAndProtectsSecrets(t *testing.T) {
	for _, combined := range []bool{false, true} {
		t.Run(fmt.Sprintf("combined=%t", combined), func(t *testing.T) {
			testAdministrationConfigPublication(t, combined)
		})
	}
}

func testAdministrationConfigPublication(t *testing.T, combined bool) {
	t.Helper()
	store, cfg := fixture(t)
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), WatchConfig: func(string, *config.Manager) (appruntime.ConfigWatch, error) { return &watch{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	if combined {
		process.HTTP.SetupRoutes(router)
	} else {
		process.HTTP.SetupAdministrationRoutes(router)
	}
	var cookie *http.Cookie
	var csrf string
	request := func(method, path, body, version string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("If-Match", version)
		if cookie != nil {
			r.AddCookie(cookie)
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	setup := request("POST", "/api/auth/setup", `{"username":"admin","password":"password123"}`, "")
	if setup.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", setup.Code, setup.Body)
	}
	cookie = setup.Result().Cookies()[0]
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	csrf = session.CSRF
	first := request("GET", "/api/system/config", "", "")
	if first.Code != http.StatusOK {
		t.Fatalf("read: %d %s", first.Code, first.Body)
	}
	var view struct {
		Config config.Config `json:"config"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	view.Config.LLM.APIKey = "private-host-key"
	payload, _ := json.Marshal(view.Config)
	missing := request("POST", "/api/system/config", string(payload), "")
	if missing.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing version: %d %s", missing.Code, missing.Body)
	}
	saved := request("POST", "/api/system/config", string(payload), first.Header().Get("ETag"))
	if saved.Code != http.StatusOK || strings.Contains(saved.Body.String(), "private-host-key") {
		t.Fatalf("save: %d %s", saved.Code, saved.Body)
	}
	if cfg.Snapshot().LLM.APIKey != "private-host-key" {
		t.Fatal("save did not publish through authoritative Manager")
	}
	stale := request("POST", "/api/system/config", string(payload), first.Header().Get("ETag"))
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale version: %d %s", stale.Code, stale.Body)
	}

	device := request("GET", "/api/system/koreader", "", "")
	if device.Code != http.StatusOK {
		t.Fatalf("device settings: %d %s", device.Code, device.Body)
	}
	settingsBody := `{"enabled":true,"base_path":" /devices ","match_mode":"file_path"}`
	deviceSaved := request("POST", "/api/system/koreader", settingsBody, device.Header().Get("ETag"))
	if deviceSaved.Code != http.StatusOK || cfg.Snapshot().KOReader.BasePath != "/devices" || cfg.Snapshot().LLM.APIKey != "private-host-key" {
		t.Fatalf("device section save: %d %s", deviceSaved.Code, deviceSaved.Body)
	}
	deviceStale := request("POST", "/api/system/koreader", settingsBody, device.Header().Get("ETag"))
	if deviceStale.Code != http.StatusConflict {
		t.Fatalf("stale device settings: %d %s", deviceStale.Code, deviceStale.Body)
	}
	cookie = nil
	denied := request("GET", "/api/system/config", "", "")
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("private config: %d", denied.Code)
	}
}

func TestAdministrationResetRevokesBrowserAndCachedProtocolCredentials(t *testing.T) {
	store, cfg := fixture(t)
	if err := cfg.Save(filepath.Join(t.TempDir(), "enabled.yaml"), nil, func(next *config.Config) error { next.Protocols.OPDS.Enabled = true; return nil }, nil, nil); err != nil {
		t.Fatal(err)
	}
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), WatchConfig: func(string, *config.Manager) (appruntime.ConfigWatch, error) { return &watch{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	process.HTTP.SetupOPDSRoutes(router)
	type session struct {
		cookie *http.Cookie
		csrf   string
	}
	request := func(method, path, body string, s session) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if s.cookie != nil {
			r.AddCookie(s.cookie)
			r.Header.Set("X-CSRF-Token", s.csrf)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	readSession := func(w *httptest.ResponseRecorder) session {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("session: %d %s", w.Code, w.Body)
		}
		var body struct {
			CSRF string `json:"csrf_token"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return session{w.Result().Cookies()[0], body.CSRF}
	}
	admin := readSession(request("POST", "/api/auth/setup", `{"username":"admin","password":"password123"}`, session{}))
	created := request("POST", "/api/users", `{"username":"reader","password":"initial-pass"}`, admin)
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	reader := readSession(request("POST", "/api/auth/login", `{"username":"reader","password":"initial-pass"}`, session{}))
	reader = readSession(request("POST", "/api/auth/change-password", `{"current_password":"initial-pass","new_password":"readerpass"}`, reader))
	basic := func(password string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/opds/v1.2/", nil)
		r.SetBasicAuth("reader", password)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := basic("readerpass"); w.Code != http.StatusOK {
		t.Fatalf("warm Basic: %d %s", w.Code, w.Body)
	}
	reset := request("POST", fmt.Sprintf("/api/users/%d/password", user.ID), `{"password":"replacement"}`, admin)
	if reset.Code != 200 || !strings.Contains(reset.Body.String(), `"password_committed":true`) {
		t.Fatalf("reset acknowledgment: %d %s", reset.Code, reset.Body)
	}
	if w := request("GET", "/api/auth/me", "", reader); w.Code != http.StatusUnauthorized {
		t.Fatalf("old browser session: %d %s", w.Code, w.Body)
	}
	if w := basic("readerpass"); w.Code != http.StatusUnauthorized {
		t.Fatalf("cached old Basic: %d %s", w.Code, w.Body)
	}
	if w := basic("replacement"); w.Code != http.StatusUnauthorized {
		t.Fatalf("reset credential must require password change: %d %s", w.Code, w.Body)
	}
	resetReader := readSession(request("POST", "/api/auth/login", `{"username":"reader","password":"replacement"}`, session{}))
	changed := request("POST", "/api/auth/change-password", `{"current_password":"replacement","new_password":"reader-final"}`, resetReader)
	if changed.Code != 200 || !strings.Contains(changed.Body.String(), `"password_committed":true`) {
		t.Fatalf("change: %d %s", changed.Code, changed.Body)
	}
	if w := basic("reader-final"); w.Code != http.StatusOK {
		t.Fatalf("new Basic after change: %d %s", w.Code, w.Body)
	}
}
