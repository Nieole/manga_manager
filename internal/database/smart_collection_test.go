package database

import "testing"

// TestSmartCollectionCompletedUsesGreaterEqual 回归：完成态智能合集应与常规库列表口径一致，用 >= 而非 =。
// 当 completed_book_count 因统计口径短暂超过 book_count 时，用 = 会漏掉这些其实已读完的系列。
func TestSmartCollectionCompletedUsesGreaterEqual(t *testing.T) {
	store := newStoreForTest(t)
	ctx, libID, seriesID, _, _ := seedUserProgressFixture(t, store)
	db := store.(*SqlStore).db

	// 制造 completed_book_count(3) > book_count(2) 的口径错位。series_stats 行由 CreateBook 建立。
	if _, err := db.ExecContext(ctx, `UPDATE series_stats SET completed_book_count = 3 WHERE series_id = ?`, seriesID); err != nil {
		t.Fatalf("set stats: %v", err)
	}

	rows, _, err := store.SearchSmartCollectionSeries(ctx, SmartCollectionFilter{LibraryID: libID, ReadState: "completed"}, 50, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.ID == seriesID {
			found = true
		}
	}
	if !found {
		t.Fatalf("completed smart collection should include a series whose completed_book_count >= book_count")
	}
}
