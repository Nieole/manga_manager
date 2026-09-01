// 守「哪里都不许出现两个同名合集」这条不变量在智能书架固化入口上的执行：查重必须与创建同处
// 一个事务，撞名判 409，同时反向守住正常固化的 201 与成员齐全。

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"manga-manager/internal/database"
)

// preemptingSnapshotStore 在开事务之前抢先建出一个同名合集，精确复现「查到不存在、真正写入时
// 名字已被别人占掉」的那个窗口——两个标签页各点一次固化、固化与建合集撞车都是这个形态。
// 底下仍是同一个真库，它只负责在正确的时刻插入一次真实的并发写。
type preemptingSnapshotStore struct {
	database.Store
	once    sync.Once
	preempt func()
}

func (s *preemptingSnapshotStore) ExecTx(ctx context.Context, fn func(*database.Queries) error) error {
	s.once.Do(s.preempt)
	return s.Store.ExecTx(ctx, fn)
}

// seedSmartSnapshotFixture 建一个命中两个系列的智能书架，返回它的 id 与命中的系列 id。
func seedSmartSnapshotFixture(t *testing.T, filterName string) (*Controller, database.Store, int64, []int64) {
	t.Helper()

	controller, store, _, rootDir := newTestController(t)
	lib, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	_, seriesB, _ := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)

	ctx := context.Background()
	db := store.(*database.SqlStore).DB()
	tag, err := store.UpsertTag(ctx, "Action")
	if err != nil {
		t.Fatalf("UpsertTag: %v", err)
	}
	// 两个系列都归到同一个资料库并打同一个标签，书架才会命中两条——「成员齐全」这条判据要有东西可判。
	for _, series := range []database.Series{seriesA, seriesB} {
		if _, err := db.ExecContext(ctx, `UPDATE series SET library_id = ? WHERE id = ?`, lib.ID, series.ID); err != nil {
			t.Fatalf("归拢系列资料库: %v", err)
		}
		if err := store.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: series.ID, TagID: tag.ID}); err != nil {
			t.Fatalf("LinkSeriesTag: %v", err)
		}
	}

	res, err := db.ExecContext(ctx, `
		INSERT INTO smart_filters (library_id, name, active_tag, sort_by_field, sort_dir, page_size)
		VALUES (?, ?, ?, ?, ?, ?)
	`, lib.ID, filterName, "Action", "name", "asc", 30)
	if err != nil {
		t.Fatalf("插入智能书架: %v", err)
	}
	filterID, _ := res.LastInsertId()
	return controller, store, filterID, []int64{seriesA.ID, seriesB.ID}
}

func postSmartSnapshot(t *testing.T, controller *Controller, filterID int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := requestWithRouteParam(http.MethodPost, "/api/collection-views/smart/1/snapshot", []byte(body), "filterId", strconv.FormatInt(filterID, 10))
	rec := httptest.NewRecorder()
	controller.snapshotSmartCollection(rec, req)
	return rec
}

func TestSnapshotSmartCollectionRejectsDuplicateName(t *testing.T) {
	t.Run("查重与创建之间被抢先", func(t *testing.T) {
		controller, store, filterID, _ := seedSmartSnapshotFixture(t, "Action in A")
		ctx := context.Background()

		// 抢先者走的是同一条写入通路，只是提前一步提交；它落在 ExecTx 开事务之前，
		// 也就是固化路径「查完重、还没写」的那一刻，不靠 sleep 赛跑。
		controller.store = &preemptingSnapshotStore{Store: store, preempt: func() {
			if err := store.ExecTx(ctx, func(q *database.Queries) error {
				_, err := q.CreateCollection(ctx, database.CreateCollectionParams{
					Name:        "Frozen Action",
					Description: sql.NullString{String: "raced", Valid: true},
					SourceType:  "manual",
				})
				return err
			}); err != nil {
				t.Errorf("抢先建合集失败: %v", err)
			}
		}}

		rec := postSmartSnapshot(t, controller, filterID, `{"name":"Frozen Action","description":"snapshot"}`)
		if rec.Code != http.StatusConflict {
			t.Errorf("状态码 = %d，期望 %d —— 被抢先与事前撞名是同一件事", rec.Code, http.StatusConflict)
		}
		if got := countCollectionsNamed(t, store, "Frozen Action"); got != 1 {
			t.Errorf("名为 Frozen Action 的合集数 = %d，期望 1 —— 查重与创建之间的窗口漏出了第二个同名合集", got)
		}
	})

	t.Run("事前就撞名", func(t *testing.T) {
		controller, store, filterID, _ := seedSmartSnapshotFixture(t, "Action in A")
		db := store.(*database.SqlStore).DB()
		if _, err := db.ExecContext(context.Background(), `INSERT INTO collections (name, description) VALUES (?, ?)`, "Frozen Action", "existing"); err != nil {
			t.Fatalf("插入同名合集: %v", err)
		}

		rec := postSmartSnapshot(t, controller, filterID, `{"name":"Frozen Action","description":"snapshot"}`)
		if rec.Code != http.StatusConflict {
			t.Errorf("状态码 = %d，期望 %d", rec.Code, http.StatusConflict)
		}
		if got := countCollectionsNamed(t, store, "Frozen Action"); got != 1 {
			t.Errorf("名为 Frozen Action 的合集数 = %d，期望 1", got)
		}
	})

	t.Run("正常固化仍然建得出且成员齐全", func(t *testing.T) {
		controller, store, filterID, seriesIDs := seedSmartSnapshotFixture(t, "Action in A")
		rec := postSmartSnapshot(t, controller, filterID, `{"name":"Frozen Action","description":"snapshot"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("状态码 = %d body=%s，期望 %d", rec.Code, rec.Body.String(), http.StatusCreated)
		}
		var created map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
			t.Fatalf("解析固化响应: %v", err)
		}
		collectionID := int64(created["id"].(float64))

		var name, sourceType string
		var memberCount int
		db := store.(*database.SqlStore).DB()
		row := db.QueryRowContext(context.Background(), `
			SELECT c.name, c.source_type, COUNT(cs.series_id)
			FROM collections c
			LEFT JOIN collection_series cs ON cs.collection_id = c.id
			WHERE c.id = ?
			GROUP BY c.id
		`, collectionID)
		if err := row.Scan(&name, &sourceType, &memberCount); err != nil {
			t.Fatalf("回读固化出的合集: %v", err)
		}
		if name != "Frozen Action" || sourceType != "smart_snapshot" || memberCount != len(seriesIDs) {
			t.Fatalf("固化结果退化: name=%q source=%q members=%d，期望成员 %d", name, sourceType, memberCount, len(seriesIDs))
		}
	})

	t.Run("回退到书架名时也裁掉首尾空白", func(t *testing.T) {
		// 请求不带名称就回退到书架名。裁白与建合集入口同口径，否则 " Action in A" 与
		// "Action in A" 会并存成两条，列表里看不出差别。
		controller, store, filterID, _ := seedSmartSnapshotFixture(t, "  Action in A  ")
		rec := postSmartSnapshot(t, controller, filterID, `{}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("状态码 = %d body=%s，期望 %d", rec.Code, rec.Body.String(), http.StatusCreated)
		}
		if got := countCollectionsNamed(t, store, "Action in A"); got != 1 {
			t.Errorf("名为 Action in A（已裁白）的合集数 = %d，期望 1", got)
		}
	})
}
