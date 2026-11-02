package bootstrap_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"manga-manager/internal/bootstrap"
	"manga-manager/internal/parser"
)

// The real host must end a browser's event stream before net/http drains ordinary
// requests. Closing only the background broker after drain would deadlock shutdown.
func TestShutdownClosesLiveHostSSEBeforeDrainingHTTPAndRetiringResources(t *testing.T) {
	store, cfg := fixture(t)
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	setup := httptest.NewRecorder()
	router.ServeHTTP(setup, httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewBufferString(`{"username":"admin","password":"correct horse battery staple"}`)))
	if setup.Code != http.StatusOK || len(setup.Result().Cookies()) == 0 {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}

	entered, release, sseReturned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/status":
			close(entered)
			<-release
		case "/api/events":
			defer close(sseReturned)
		}
		router.ServeHTTP(w, r)
	}))
	defer server.Close()
	defer unblock()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(setup.Result().Cookies()[0])
	// A bounded client is a failure cleanup, never the shutdown success signal.
	client := server.Client()
	client.Timeout = 10 * time.Second
	stream, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK || stream.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events=%d %s", stream.StatusCode, stream.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(stream.Body)
	if frame, err := reader.ReadString('\n'); err != nil || frame != "retry: 5000\n" {
		t.Fatalf("initial SSE frame=%q err=%v", frame, err)
	}
	streamClosed := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reader); streamClosed <- err }()
	response := make(chan error, 1)
	go func() {
		res, err := client.Get(server.URL + "/api/auth/status")
		if err == nil {
			_, err = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			if err == nil && res.StatusCode != http.StatusOK {
				err = fmt.Errorf("auth status=%d", res.StatusCode)
			}
		}
		response <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("ordinary HTTP request did not enter")
	}
	draining, permitDrain := make(chan struct{}), make(chan struct{})
	var drainOnce sync.Once
	allowDrain := func() { drainOnce.Do(func() { close(permitDrain) }) }
	defer allowDrain()
	stopped := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		stopped <- process.Shutdown(ctx, func(ctx context.Context) error {
			close(draining)
			select {
			case <-permitDrain:
			case <-ctx.Done():
				return ctx.Err()
			}
			return server.Config.Shutdown(ctx)
		})
	}()
	select {
	case <-draining:
	case <-ctx.Done():
		t.Fatal("shutdown did not start HTTP drain")
	}
	// Require both the handler and client stream to finish while the drain barrier
	// is still closed. A client timeout or forced client-side Close cannot pass.
	select {
	case <-sseReturned:
	case <-ctx.Done():
		t.Fatal("live host SSE did not exit before HTTP drain")
	}
	select {
	case err := <-streamClosed:
		if err != nil {
			t.Fatalf("SSE closure=%v", err)
		}
	case <-ctx.Done():
		t.Fatal("SSE response did not end cleanly")
	}
	select {
	case <-process.Application.Engine.Stopping():
		t.Fatal("engine stopped before ordinary HTTP drained")
	default:
	}
	if parser.PoolMaxSize() == 0 {
		t.Fatal("resources retired while ordinary HTTP was active")
	}
	allowDrain()
	unblock()
	select {
	case err := <-response:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("ordinary response did not finish")
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("shutdown did not finish")
	}
	select {
	case <-process.Application.Engine.Stopping():
	default:
		t.Fatal("engine not stopped after HTTP drain")
	}
	if parser.PoolMaxSize() != 0 {
		t.Fatal("resources not retired after drain")
	}
}
