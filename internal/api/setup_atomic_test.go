package api

import (
	"context"
	"encoding/json"
	"errors"
	"manga-manager/internal/accounts"
	"manga-manager/internal/database"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type concurrentSetupCount struct {
	database.Store
	arrived sync.WaitGroup
}

func (s *concurrentSetupCount) CountUsers(ctx context.Context) (int64, error) {
	n, err := s.Store.CountUsers(ctx)
	s.arrived.Done()
	s.arrived.Wait()
	return n, err
}
func TestSetupAtomicallyCreatesOnlyOneAdministrator(t *testing.T) {
	c, store, _, _ := newTestController(t)
	barrier := &concurrentSetupCount{Store: store}
	barrier.arrived.Add(2)
	c.store = barrier
	codes := make(chan int, 2)
	for _, name := range []string{"admin-a", "admin-b"} {
		go func(name string) {
			rec := httptest.NewRecorder()
			c.setupAdmin(rec, httptest.NewRequest(http.MethodPost, "/", jsonBody(`{"username":"`+name+`","password":"password1"}`)))
			codes <- rec.Code
		}(name)
	}
	a, b := <-codes, <-codes
	if (a != 200 || b != 409) && (a != 409 || b != 200) {
		t.Fatalf("statuses=%d,%d", a, b)
	}
	n, err := store.CountUsers(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("users=%d err=%v", n, err)
	}
}

type failedSetupSession struct{ database.Store }

func (failedSetupSession) CreateSession(context.Context, database.Session) error {
	return errors.New("session failed")
}
func TestSetupSessionFailurePreservesCommittedAccountAndExactUsername(t *testing.T) {
	c, store, _, _ := newTestController(t)
	c.store = failedSetupSession{store}
	rec := httptest.NewRecorder()
	c.setupAdmin(rec, httptest.NewRequest(http.MethodPost, "/", jsonBody(`{"username":" admin ","password":"password1"}`)))
	var body struct {
		Committed bool `json:"account_committed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 500 || !body.Committed || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetUserByUsername(context.Background(), " admin "); err != nil {
		t.Fatal(err)
	}
	c.store = store
	login := httptest.NewRecorder()
	c.login(login, httptest.NewRequest(http.MethodPost, "/", jsonBody(`{"username":" admin ","password":"password1"}`)))
	if login.Code != 200 {
		t.Fatalf("login=%d %s", login.Code, login.Body.String())
	}
}
func TestSetupClaimRollbackWarnsWithoutUndoingAdministrator(t *testing.T) {
	_, store, _, root := newTestController(t)
	_, _, book := seedBookFixture(t, store, root, "Library", "Series", "01.cbz", 10)
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec("UPDATE books SET last_read_page=5,last_read_at=CURRENT_TIMESTAMP WHERE id=?", book.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TRIGGER reject_claim BEFORE INSERT ON user_series_progress BEGIN SELECT RAISE(ABORT,'claim failed');END"); err != nil {
		t.Fatal(err)
	}
	out, err := accounts.NewInitialization(store, hashPassword).Setup(context.Background(), accounts.SetupInput{Username: "Admin", Password: "password1"})
	if err != nil || !out.Committed || len(out.Warnings) != 1 || out.Warnings[0].Step != "reading_progress" {
		t.Fatalf("result=%+v err=%v", out, err)
	}
	if _, found, err := store.GetUserBookProgress(context.Background(), out.User.ID, book.ID); err != nil || found {
		t.Fatalf("partial claim found=%v err=%v", found, err)
	}
	if _, err := db.Exec("DROP TRIGGER reject_claim"); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateGlobalProgressToUser(context.Background(), out.User.ID); err != nil {
		t.Fatal(err)
	}
	if progress, found, err := store.GetUserBookProgress(context.Background(), out.User.ID, book.ID); err != nil || !found || progress.LastReadPage.Int64 != 5 {
		t.Fatalf("retry=%+v found=%v err=%v", progress, found, err)
	}
}

func TestSetupClaimsLegacyActivityAndDeviceAccountWithoutMergingCredentials(t *testing.T) {
	_, store, _, root := newTestController(t)
	_, _, book := seedBookFixture(t, store, root, "Library", "Series", "01.cbz", 10)
	device, err := store.CreateKOReaderAccount(context.Background(), database.CreateKOReaderAccountParams{Username: "Admin", SyncKey: "device-secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.(*database.SqlStore).DB().Exec("INSERT INTO reading_activity(book_id,date,pages_read) VALUES(?,date('now'),3)", book.ID); err != nil {
		t.Fatal(err)
	}
	out, err := accounts.NewInitialization(store, hashPassword).Setup(context.Background(), accounts.SetupInput{Username: "Admin", Password: "password1"})
	if err != nil || !out.Committed || len(out.Warnings) != 0 {
		t.Fatalf("result=%+v err=%v", out, err)
	}
	owner, err := store.GetKOReaderAccountUserID(context.Background(), "Admin")
	if err != nil || owner != out.User.ID {
		t.Fatalf("owner=%d err=%v", owner, err)
	}
	current, err := store.GetKOReaderAccountByUsername(context.Background(), "Admin")
	if err != nil || current.ID != device.ID || current.SyncKey != device.SyncKey {
		t.Fatalf("device=%+v err=%v", current, err)
	}
	activity, err := store.GetUserActivityHeatmap(context.Background(), out.User.ID, "-7 days")
	if err != nil || len(activity) != 1 {
		t.Fatalf("activity=%+v err=%v", activity, err)
	}
	if _, ok := accounts.Authenticate(context.Background(), store, accounts.Credentials{Username: "Admin", Password: "device-secret"}, dummyPasswordHash, verifyPassword); ok {
		t.Fatal("device credentials authenticated as site user")
	}
}
