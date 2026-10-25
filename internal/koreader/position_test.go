package koreader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func matchedPositionFixture(t *testing.T) (*Service, database.Store, Credentials, database.Book, int64) {
	t.Helper()
	service, store, root := newTestService(t, config.KOReaderMatchModeFilePath)
	_, book := seedServiceBook(t, store, root, "Library", "Series", "Book.cbz")
	if _, _, err := service.RebuildBookIdentities(context.Background(), RebuildOptions{BatchSize: 10}, &fakeTaskHandle{}); err != nil {
		t.Fatal(err)
	}
	user := createServiceTestUser(t, store, "reader", database.RoleRegular)
	account, err := store.CreateKOReaderAccount(context.Background(), database.CreateKOReaderAccountParams{Username: "device", SyncKey: "secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetKOReaderAccountUser(context.Background(), account.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	return service, store, Credentials{Username: "device", Key: HashKey("secret")}, book, user.ID
}

func devicePosition(percentage float64) ProgressPayload {
	return ProgressPayload{Document: "Series/Book.cbz", Progress: "/page", Percentage: percentage, Device: "reader", DeviceID: "device-1"}
}

func TestMatchedDevicePushAllowsBackwardCurrentPosition(t *testing.T) {
	service, store, creds, book, uid := matchedPositionFixture(t)
	ctx := context.Background()
	for _, percent := range []float64{.75, .25} {
		result, err := service.SaveProgress(ctx, creds, devicePosition(percent))
		if err != nil || !result.Matched {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	p, found, err := store.GetUserBookProgress(ctx, uid, book.ID)
	if err != nil || !found || p.LastReadPage.Int64 != 8 {
		t.Fatalf("p=%+v found=%v err=%v", p, found, err)
	}
}

func TestMatchedPullUsesOriginalPreciseDeviceOccurrence(t *testing.T) {
	service, store, creds, book, uid := matchedPositionFixture(t)
	ctx := context.Background()
	if _, err := store.DB().Exec(`UPDATE books SET path_fingerprint=NULL,path_fingerprint_no_ext=NULL,filename_fingerprint=NULL WHERE id=?`, book.ID); err != nil {
		t.Fatal(err)
	}
	occurred := time.Date(2026, 10, 20, 10, 0, 0, 123456700, time.UTC)
	service.now = func() time.Time { return occurred }
	saved, err := service.SaveProgress(ctx, creds, devicePosition(.25))
	if err != nil || saved.Matched {
		t.Fatalf("unmatched result=%+v err=%v", saved, err)
	}
	if _, _, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 10}, &fakeTaskHandle{}); err != nil {
		t.Fatal(err)
	}
	pulled, err := service.GetProgress(ctx, creds, devicePosition(.25).Document)
	if err != nil || !pulled.BookID.Valid {
		t.Fatalf("pulled=%+v err=%v", pulled, err)
	}
	p, found, err := store.GetUserBookProgress(ctx, uid, book.ID)
	if err != nil || !found || p.LastReadPage.Int64 != 8 || !p.LastReadAt.Time.Equal(occurred) {
		t.Fatalf("p=%+v found=%v err=%v want time=%v", p, found, err, occurred)
	}
}

func TestMatchedSamePageDeviceReportRefreshesTimeWithoutCrossingUsers(t *testing.T) {
	service, store, creds, book, uid := matchedPositionFixture(t)
	ctx := context.Background()
	other := createServiceTestUser(t, store, "other", database.RoleRegular)
	first := time.Date(2026, 10, 20, 10, 0, 0, 111, time.UTC)
	if err := store.SetUserBookProgress(ctx, other.ID, book.ID, 20, first.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{first, first.Add(time.Second)} {
		service.now = func() time.Time { return at }
		result, err := service.SaveProgress(ctx, creds, devicePosition(.25))
		if err != nil || !result.PositionApplied {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	p, found, err := store.GetUserBookProgress(ctx, uid, book.ID)
	if err != nil || !found || !p.LastReadAt.Time.Equal(first.Add(time.Second)) {
		t.Fatalf("p=%+v found=%v err=%v", p, found, err)
	}
	p, found, err = store.GetUserBookProgress(ctx, other.ID, book.ID)
	if err != nil || !found || p.LastReadPage.Int64 != 20 {
		t.Fatalf("other user changed: %+v found=%v err=%v", p, found, err)
	}
	global, err := store.GetBook(ctx, book.ID)
	if err != nil || global.LastReadPage.Valid {
		t.Fatalf("global leaked: %+v err=%v", global.LastReadPage, err)
	}
}

func TestDeviceReplayAfterClearAndRestartCannotRestoreOldPosition(t *testing.T) {
	for _, reconcile := range []bool{false, true} {
		for _, anonymous := range []bool{false, true} {
			t.Run(fmt.Sprintf("reconcile=%v anonymous=%v", reconcile, anonymous), func(t *testing.T) {
				service, store, creds, book, uid := matchedPositionFixture(t)
				ctx := context.Background()
				if anonymous {
					uid = 0
					account, err := store.GetKOReaderAccountByUsername(ctx, creds.Username)
					if err != nil {
						t.Fatal(err)
					}
					if err := store.SetKOReaderAccountUser(ctx, account.ID, 0); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := store.DB().Exec(`UPDATE books SET path_fingerprint=NULL,path_fingerprint_no_ext=NULL,filename_fingerprint=NULL WHERE id=?`, book.ID); err != nil {
					t.Fatal(err)
				}
				occurred := time.Date(2026, 10, 20, 10, 0, 0, 123456789, time.UTC)
				service.now = func() time.Time { return occurred }
				result, err := service.SaveProgress(ctx, creds, devicePosition(.75))
				if err != nil || result.Matched {
					t.Fatalf("unmatched=%+v err=%v", result, err)
				}
				clearAt := occurred.Add(time.Hour)
				if _, err := reading.New(store, nil).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{book.ID}, IsRead: false, OccurredAt: &clearAt, OperationID: "clear-device"}); err != nil {
					t.Fatal(err)
				}
				cfg := service.cfg
				path := cfg.Snapshot().Database.Path
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				if err := database.Migrate(path); err != nil {
					t.Fatal(err)
				}
				reopened, err := database.NewStore(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = reopened.Close() })
				service = NewService(reopened, cfg)
				if _, _, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 10}, &fakeTaskHandle{}); err != nil {
					t.Fatal(err)
				}
				if reconcile {
					updated, total, err := service.ReconcileProgress(ctx, 10, &fakeTaskHandle{})
					if err != nil || updated != 1 || total != 1 {
						t.Fatalf("reconcile=%d/%d err=%v", updated, total, err)
					}
				} else {
					record, err := service.GetProgress(ctx, creds, devicePosition(.75).Document)
					if err != nil || !record.BookID.Valid {
						t.Fatalf("pull=%+v err=%v", record, err)
					}
				}
				if uid > 0 {
					if _, found, err := reopened.GetUserBookProgress(ctx, uid, book.ID); err != nil || found {
						t.Fatalf("old device restored cleared user state: %v %v", found, err)
					}
				} else {
					b, err := reopened.GetBook(ctx, book.ID)
					if err != nil || b.LastReadPage.Valid {
						t.Fatalf("old device restored cleared global state: %+v %v", b, err)
					}
				}
				service.now = func() time.Time { return clearAt.Add(time.Hour) }
				result, err = service.SaveProgress(ctx, creds, devicePosition(.25))
				if err != nil || !result.PositionApplied {
					t.Fatalf("new device report blocked: %+v err=%v", result, err)
				}
			})
		}
	}
}

func TestMatchedDeviceActivityWarningIsNotAPositionFailure(t *testing.T) {
	service, store, creds, book, uid := matchedPositionFixture(t)
	ctx := context.Background()
	if _, err := store.DB().Exec(`CREATE TRIGGER fail_device_activity BEFORE INSERT ON reading_activity BEGIN SELECT RAISE(ABORT,'activity unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := service.SaveProgress(ctx, creds, devicePosition(.25))
	if err != nil || !result.PositionApplied || result.ProjectionError != nil || len(result.ActivityWarnings) != 1 || result.Record.ID <= 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	p, found, err := store.GetUserBookProgress(ctx, uid, book.ID)
	if err != nil || !found || p.LastReadPage.Int64 != 8 {
		t.Fatalf("p=%+v found=%v err=%v", p, found, err)
	}
	now := time.Now()
	stats, err := store.GetUserPeriodStats(ctx, uid, now.Year(), int(now.Month()))
	if err != nil || stats.Pages != 8 || stats.BooksTouched != 1 {
		t.Fatalf("independent user statistics not attempted: %+v err=%v", stats, err)
	}
	issue, err := store.GetLatestKOReaderFailure(ctx)
	if err != nil || issue.Status != "progress_activity_warning" || !strings.Contains(issue.Message, "position saved") {
		t.Fatalf("diagnostic=%+v err=%v", issue, err)
	}
}

func TestDeviceCanonicalSaveFailureDoesNotHideCommittedBookPosition(t *testing.T) {
	service, store, creds, book, uid := matchedPositionFixture(t)
	ctx := context.Background()
	if _, err := store.DB().Exec(`CREATE TRIGGER fail_device_record BEFORE INSERT ON koreader_progress BEGIN SELECT RAISE(ABORT,'device record unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := service.SaveProgress(ctx, creds, devicePosition(.25))
	if err == nil || !result.PositionApplied || result.ProjectionError != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	p, found, readErr := store.GetUserBookProgress(ctx, uid, book.ID)
	if readErr != nil || !found || p.LastReadPage.Int64 != 8 {
		t.Fatalf("committed position lost: %+v found=%v err=%v", p, found, readErr)
	}
	if _, readErr := store.GetKOReaderProgress(ctx, creds.Username, devicePosition(.25).Document); !errors.Is(readErr, sql.ErrNoRows) {
		t.Fatalf("canonical record unexpectedly saved: %v", readErr)
	}
}

func TestUnknownPageCountKeepsDeviceRecordWithoutInventingBookPages(t *testing.T) {
	service, store, creds, book, uid := matchedPositionFixture(t)
	ctx := context.Background()
	if _, err := store.DB().Exec(`UPDATE books SET page_count=0 WHERE id=?`, book.ID); err != nil {
		t.Fatal(err)
	}
	result, err := service.SaveProgress(ctx, creds, devicePosition(1))
	if err != nil || !result.Matched || result.PositionApplied || result.Record.Percentage != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, found, err := store.GetUserBookProgress(ctx, uid, book.ID); err != nil || found {
		t.Fatalf("unknown book acquired invented page: %v %v", found, err)
	}
}

func TestLegacyDeviceRecordsKeepTheirOriginalTimestampAfterUpgrade(t *testing.T) {
	service, store, creds, book, uid := matchedPositionFixture(t)
	ctx := context.Background()
	if _, err := store.DB().Exec(`UPDATE books SET path_fingerprint=NULL,path_fingerprint_no_ext=NULL,filename_fingerprint=NULL WHERE id=?`, book.ID); err != nil {
		t.Fatal(err)
	}
	original := time.Date(2026, 10, 20, 10, 0, 0, 123456789, time.UTC)
	service.now = func() time.Time { return original }
	if result, err := service.SaveProgress(ctx, creds, devicePosition(.25)); err != nil || result.Matched {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := store.DB().Exec(`ALTER TABLE koreader_progress DROP COLUMN received_at_ns`); err != nil {
		t.Fatal(err)
	}
	cfg := service.cfg
	path := cfg.Snapshot().Database.Path
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	service = NewService(reopened, cfg)
	if _, _, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 10}, &fakeTaskHandle{}); err != nil {
		t.Fatal(err)
	}
	record, err := service.GetProgress(ctx, creds, devicePosition(.25).Document)
	if err != nil || record.ReceivedAtNS != 0 || record.Timestamp != original.Unix() {
		t.Fatalf("legacy record changed=%+v err=%v", record, err)
	}
	p, found, err := reopened.GetUserBookProgress(ctx, uid, book.ID)
	if err != nil || !found || !p.LastReadAt.Time.Equal(original.Truncate(time.Second)) {
		t.Fatalf("old projection became fresh: %+v found=%v err=%v", p, found, err)
	}
}

type unavailableOwnerStore struct{ database.Store }

func (unavailableOwnerStore) GetKOReaderAccountUserID(context.Context, string) (int64, error) {
	return 0, errors.New("owner lookup unavailable")
}

func TestDeviceOwnerLookupFailureCannotFallBackToGlobalReading(t *testing.T) {
	service, store, creds, book, _ := matchedPositionFixture(t)
	ctx := context.Background()
	service = NewService(unavailableOwnerStore{Store: store}, service.cfg)
	result, err := service.SaveProgress(ctx, creds, devicePosition(.25))
	if err == nil || result.PositionApplied {
		t.Fatalf("unresolved owner was accepted: %+v err=%v", result, err)
	}
	global, err := store.GetBook(ctx, book.ID)
	if err != nil || global.LastReadPage.Valid {
		t.Fatalf("owner lookup leaked reading globally: %+v err=%v", global, err)
	}
}

func TestFreshDeviceReportsInSameReceiveTickDoNotBecomeHistoricalReplays(t *testing.T) {
	service, store, creds, book, uid := matchedPositionFixture(t)
	ctx := context.Background()
	tick := time.Date(2026, 10, 25, 23, 59, 59, 999999999, time.Local)
	service.now = func() time.Time { return tick }
	for _, percentage := range []float64{.75, .25, .25} {
		result, err := service.SaveProgress(ctx, creds, devicePosition(percentage))
		if err != nil || !result.PositionApplied {
			t.Fatalf("fresh percentage=%v result=%+v err=%v", percentage, result, err)
		}
	}
	clear, err := reading.New(store, nil, reading.WithClock(func() time.Time { return tick })).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{book.ID}, IsRead: false})
	if err != nil || len(clear.CommittedBookIDs) != 1 {
		t.Fatalf("clear=%+v err=%v", clear, err)
	}
	result, err := service.SaveProgress(ctx, creds, devicePosition(.125))
	if err != nil || !result.PositionApplied {
		t.Fatalf("new push after clear=%+v err=%v", result, err)
	}
	p, found, err := store.GetUserBookProgress(ctx, uid, book.ID)
	if err != nil || !found || p.LastReadPage.Int64 != 4 || !p.LastReadAt.Time.Equal(tick) {
		t.Fatalf("p=%+v found=%v err=%v", p, found, err)
	}
	// An old, initially unmatched record at that same tick is still a replay.
	if _, err := store.DB().Exec(`UPDATE books SET path_fingerprint=NULL,path_fingerprint_no_ext=NULL,filename_fingerprint=NULL WHERE id=?`, book.ID); err != nil {
		t.Fatal(err)
	}
	old, err := service.SaveProgress(ctx, creds, devicePosition(.75))
	if err != nil || old.Matched {
		t.Fatalf("unmatched old record=%+v err=%v", old, err)
	}
	if _, err := reading.New(store, nil, reading.WithClock(func() time.Time { return tick })).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{book.ID}, IsRead: false}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 10}, &fakeTaskHandle{}); err != nil {
		t.Fatal(err)
	}
	replayed, err := service.GetProgress(ctx, creds, devicePosition(.75).Document)
	if err != nil || !replayed.BookID.Valid {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	if _, found, err := store.GetUserBookProgress(ctx, uid, book.ID); err != nil || found {
		t.Fatalf("same-tick old record revived clear: %v %v", found, err)
	}
	fresh, err := service.SaveProgress(ctx, creds, devicePosition(.125))
	if err != nil || !fresh.PositionApplied {
		t.Fatalf("fresh report blocked: %+v err=%v", fresh, err)
	}
	days, err := store.GetActivityHeatmap(ctx, "2026-10-25")
	if err != nil || len(days) != 1 || days[0].Date != "2026-10-25" {
		t.Fatalf("synthetic next reading day=%+v err=%v", days, err)
	}
}
