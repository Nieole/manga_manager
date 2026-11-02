package api

import (
	"context"
	"database/sql"
	"errors"
	"manga-manager/internal/accounts"
	"manga-manager/internal/database"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

type adminCountBarrier struct {
	database.Store
	arrived sync.WaitGroup
}

func (s *adminCountBarrier) CountAdmins(ctx context.Context) (int64, error) {
	n, err := s.Store.CountAdmins(ctx)
	s.arrived.Done()
	s.arrived.Wait()
	return n, err
}
func TestConcurrentAccountDemotionsPreserveAdministrator(t *testing.T) {
	c, store, _, _ := newTestController(t)
	a := mkTestUser(t, store, "a", "admin")
	b := mkTestUser(t, store, "b", "admin")
	barrier := &adminCountBarrier{Store: store}
	barrier.arrived.Add(2)
	c.admin.management = accounts.NewManagement(barrier, hashPassword)
	codes := make(chan int, 2)
	for _, user := range []database.User{a, b} {
		go func(user database.User) {
			rec := httptest.NewRecorder()
			req := requestWithRouteParam(http.MethodPatch, "/", []byte(`{"role":"regular"}`), "userId", strconv.FormatInt(user.ID, 10))
			c.admin.updateUser(rec, withUserContext(req, user))
			codes <- rec.Code
		}(user)
	}
	x, y := <-codes, <-codes
	if (x != 200 || y != 403) && (x != 403 || y != 200) {
		t.Fatalf("statuses=%d,%d", x, y)
	}
	n, err := store.CountAdmins(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("admins=%d err=%v", n, err)
	}
}
func TestAccountManagementAuthorizesActorWithoutRouteMiddleware(t *testing.T) {
	c, store, _, _ := newTestController(t)
	actor := mkTestUser(t, store, "regular", "regular")
	target := mkTestUser(t, store, "target", "regular")
	rec := httptest.NewRecorder()
	c.admin.updateUser(rec, withUserContext(requestWithRouteParam(http.MethodPatch, "/", []byte(`{"display_name":"changed"}`), "userId", strconv.FormatInt(target.ID, 10)), actor))
	if rec.Code != 403 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestConcurrentCrossAccountDeletesAndStaleActorKeepAdministrator(t *testing.T) {
	_, store, _, _ := newTestController(t)
	a := mkTestUser(t, store, "a", "admin")
	b := mkTestUser(t, store, "b", "admin")
	service := accounts.NewManagement(store, hashPassword)
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() { <-start; done <- service.Delete(context.Background(), accounts.Actor{ID: a.ID}, b.ID) }()
	go func() { <-start; done <- service.Delete(context.Background(), accounts.Actor{ID: b.ID}, a.ID) }()
	close(start)
	first, second := <-done, <-done
	if first != nil && second != nil {
		t.Fatalf("neither command committed: %v %v", first, second)
	}
	if first == nil && second == nil {
		t.Fatal("both administrators deleted")
	}
	remaining, err := store.CountAdmins(context.Background())
	if err != nil || remaining != 1 {
		t.Fatalf("admins=%d err=%v", remaining, err)
	}
}
func TestAccountManagementFreshAuthorizationAndSelfDelete(t *testing.T) {
	_, store, _, _ := newTestController(t)
	a := mkTestUser(t, store, "a", "admin")
	b := mkTestUser(t, store, "b", "admin")
	service := accounts.NewManagement(store, hashPassword)
	if err := service.Delete(context.Background(), accounts.Actor{ID: a.ID}, a.ID); !errors.Is(err, accounts.ErrSelfDelete) {
		t.Fatalf("self delete=%v", err)
	}
	if _, err := service.Update(context.Background(), accounts.Actor{ID: a.ID}, b.ID, accounts.UpdateInput{Role: "regular"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), accounts.Actor{ID: b.ID}, accounts.CreateInput{Username: "intruder", Password: "password1"}); !errors.Is(err, accounts.ErrForbidden) {
		t.Fatalf("stale actor=%v", err)
	}
	user, err := service.Create(context.Background(), accounts.Actor{ID: a.ID}, accounts.CreateInput{Username: " Exact ", Password: "password1"})
	if err != nil || user.Username != " Exact " || !user.MustChangePassword {
		t.Fatalf("created=%+v err=%v", user, err)
	}
}

func TestAccountManagementDeletePreservesSessionAndDeviceCascade(t *testing.T) {
	_, store, _, _ := newTestController(t)
	admin := mkTestUser(t, store, "admin", "admin")
	target := mkTestUser(t, store, "target", "regular")
	ctx := context.Background()
	if err := store.CreateSession(ctx, database.Session{ID: "owned-session", UserID: target.ID, CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	device, err := store.CreateKOReaderAccount(ctx, database.CreateKOReaderAccountParams{Username: "device", SyncKey: "key", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetKOReaderAccountUser(ctx, device.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if err := accounts.NewManagement(store, hashPassword).Delete(ctx, accounts.Actor{ID: admin.ID}, target.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetSessionWithUser(ctx, "owned-session", time.Now()); !errors.Is(err, database.ErrSessionNotFound) {
		t.Fatalf("session=%v", err)
	}
	if _, err := store.GetKOReaderAccountByUsername(ctx, "device"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("device=%v", err)
	}
}
