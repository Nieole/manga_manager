package api

import (
	"context"
	"encoding/json"
	"errors"
	"manga-manager/internal/accounts"
	"manga-manager/internal/database"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type pausedCredentialRead struct {
	database.Store
	first   atomic.Bool
	loaded  chan struct{}
	release chan struct{}
}

func (s *pausedCredentialRead) GetUserByUsername(ctx context.Context, name string) (database.User, error) {
	user, err := s.Store.GetUserByUsername(ctx, name)
	if s.first.CompareAndSwap(false, true) {
		close(s.loaded)
		<-s.release
	}
	return user, err
}

func TestPasswordCommitPreventsOldInFlightBasicCredentialsFromRepopulatingCache(t *testing.T) {
	c, store, _, _ := newTestController(t)
	ctx := context.Background()
	hash, _ := hashPassword("password1")
	user, err := store.CreateUser(ctx, database.CreateUserParams{Username: "reader", PasswordHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	paused := &pausedCredentialRead{Store: store, loaded: make(chan struct{}), release: make(chan struct{})}
	c.store = paused
	protocol := c.requireBasicAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := func(password string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/opds", nil)
		req.SetBasicAuth(user.Username, password)
		protocol.ServeHTTP(rec, req)
		return rec.Code
	}
	done := make(chan int, 1)
	go func() { done <- request("password1") }()
	<-paused.loaded
	_, err = accounts.NewPasswords(store, hashPassword, verifyPassword, c.auth.invalidateBasicAuthForUser).Change(ctx, accounts.Actor{ID: user.ID}, accounts.ChangePasswordInput{CurrentPassword: "password1", NewPassword: "password2"})
	close(paused.release)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if got := request("password1"); got != http.StatusUnauthorized {
		t.Fatalf("old in-flight credentials repopulated Basic cache: status=%d", got)
	}
	if got := request("password2"); got != http.StatusNoContent {
		t.Fatalf("new credentials rejected: status=%d", got)
	}
}

func TestPasswordChangeRollsBackWhenSessionRevocationFails(t *testing.T) {
	c, store, _, _ := newTestController(t)
	hash, _ := hashPassword("password1")
	user, err := store.CreateUser(context.Background(), database.CreateUserParams{Username: "Exact", PasswordHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(context.Background(), database.Session{ID: "old", UserID: user.ID, CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec("CREATE TRIGGER reject_revoke BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'revoke failed');END"); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	c.changePassword(rec, withUserContext(httptest.NewRequest(http.MethodPost, "/", jsonBody(`{"current_password":"password1","new_password":"password2"}`)), user))
	if rec.Code != 500 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := c.authenticateUser(context.Background(), "Exact", "password1"); !ok {
		t.Fatal("failed revocation changed password")
	}
	if _, _, err := store.GetSessionWithUser(context.Background(), "old", time.Now()); err != nil {
		t.Fatal(err)
	}
}
func TestPasswordChangeSessionFailureReportsCommittedAndRejectsOldCredentials(t *testing.T) {
	c, store, _, _ := newTestController(t)
	hash, _ := hashPassword("password1")
	user, err := store.CreateUser(context.Background(), database.CreateUserParams{Username: " Exact ", PasswordHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(context.Background(), database.Session{ID: "old", UserID: user.ID, CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.resolveBasicAuthUser(context.Background(), user.Username, "password1"); !ok {
		t.Fatal("initial basic")
	}
	c.store = failedSetupSession{store}
	rec := httptest.NewRecorder()
	c.changePassword(rec, withUserContext(httptest.NewRequest(http.MethodPost, "/", jsonBody(`{"current_password":"password1","new_password":"password2"}`)), user))
	var body struct {
		Committed     bool `json:"password_committed"`
		LoginRequired bool `json:"login_required"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != 500 || !body.Committed || !body.LoginRequired {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := c.resolveBasicAuthUser(context.Background(), user.Username, "password1"); ok {
		t.Fatal("old Basic cache remains eligible")
	}
	if _, _, err := store.GetSessionWithUser(context.Background(), "old", time.Now()); err == nil {
		t.Fatal("old session remains eligible")
	}
	if _, ok := c.authenticateUser(context.Background(), user.Username, "password2"); !ok {
		t.Fatal("new password rejected")
	}
}

func TestPasswordResetAuthorizationAndProtocolEligibility(t *testing.T) {
	c, store, _, _ := newTestController(t)
	ctx := context.Background()
	admin := mkTestUser(t, store, "admin", "admin")
	hash, _ := hashPassword("password1")
	target, err := store.CreateUser(ctx, database.CreateUserParams{Username: "Target", PasswordHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	device, err := store.CreateKOReaderAccount(ctx, database.CreateKOReaderAccountParams{Username: "Target", SyncKey: "device-secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, database.Session{ID: "old", UserID: target.ID, CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetKOReaderAccountUser(ctx, device.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	protocol := c.requireBasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	basic := func(password string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/opds", nil)
		req.SetBasicAuth(target.Username, password)
		protocol.ServeHTTP(rec, req)
		return rec
	}
	if rec := basic("password1"); rec.Code != 204 {
		t.Fatalf("warm basic=%d", rec.Code)
	}
	service := accounts.NewPasswords(store, hashPassword, verifyPassword, c.auth.invalidateBasicAuthForUser)
	if _, err := service.Reset(ctx, accounts.Actor{ID: target.ID}, admin.ID, "password2"); !errors.Is(err, accounts.ErrForbidden) {
		t.Fatalf("nonadmin reset=%v", err)
	}
	if _, err := store.DB().Exec("CREATE TRIGGER reject_reset_revoke BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'revoke failed');END"); err != nil {
		t.Fatal(err)
	}
	if out, err := service.Reset(ctx, accounts.Actor{ID: admin.ID}, target.ID, "password2"); err == nil || out.Committed {
		t.Fatalf("reset=%+v %v", out, err)
	}
	if rec := basic("password1"); rec.Code != 204 {
		t.Fatalf("rolled-back basic=%d", rec.Code)
	}
	if _, err := store.DB().Exec("DROP TRIGGER reject_reset_revoke"); err != nil {
		t.Fatal(err)
	}
	out, err := service.Reset(ctx, accounts.Actor{ID: admin.ID}, target.ID, "password2")
	if err != nil || !out.Committed || !out.User.MustChangePassword {
		t.Fatalf("reset=%+v %v", out, err)
	}
	if rec := basic("password1"); rec.Code != 401 {
		t.Fatalf("cached old basic=%d", rec.Code)
	}
	if rec := basic("password2"); rec.Code != 401 || rec.Header().Get("WWW-Authenticate") != basicAuthChallengePasswordChange {
		t.Fatalf("temporary credential=%d %s", rec.Code, rec.Body.String())
	}
	if _, _, err := store.GetSessionWithUser(ctx, "old", time.Now()); err == nil {
		t.Fatal("old session survives")
	}
	if _, err := service.Change(ctx, accounts.Actor{ID: target.ID}, accounts.ChangePasswordInput{CurrentPassword: "password1", NewPassword: "password3"}); !errors.Is(err, accounts.ErrCurrentPassword) {
		t.Fatalf("stale password accepted: %v", err)
	}
	if _, err := service.Change(ctx, accounts.Actor{ID: target.ID}, accounts.ChangePasswordInput{CurrentPassword: "password2", NewPassword: "password3"}); err != nil {
		t.Fatal(err)
	}
	if rec := basic("password3"); rec.Code != 204 {
		t.Fatalf("final basic=%d", rec.Code)
	}
	owner, err := store.GetKOReaderAccountUserID(ctx, "Target")
	if err != nil || owner != target.ID {
		t.Fatalf("device owner=%d %v", owner, err)
	}
	retained, err := store.GetKOReaderAccountByID(ctx, device.ID)
	if err != nil || retained.SyncKey != "device-secret" {
		t.Fatalf("device changed=%+v err=%v", retained, err)
	}
}
