// 守「作品群由系列关系推导而来，不由人工建立」：system_franchise 合集的四个写入端点必须拒绝，
// 且不得出现在 /api/collections 这份「可加入的合集」清单里。
//
// 放行的后果是静默的：加进去的系列会被下一次重建删掉，删掉的合集会被原样建回但换了 id
// ——而合集 id 对外暴露（Mihon、OPDS），漂移会让客户端记的书库条目集体失效。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

// seedFranchiseCollection 造两个有关系的系列并重建，返回作品群合集 id 与两个系列 id。
func seedFranchiseCollection(t *testing.T, controller *Controller, store database.Store) (int64, []int64) {
	t.Helper()
	_, ids := seedFranchiseSeries(t, store, 2)
	addRelation(t, store, ids[0], ids[1])
	if err := controller.RebuildFranchiseCollections(context.Background()); err != nil {
		t.Fatalf("重建作品群失败: %v", err)
	}
	snaps := readFranchises(t, store)
	if len(snaps) != 1 {
		t.Fatalf("期望造出 1 个作品群合集，实得 %d", len(snaps))
	}
	return snaps[0].ID, ids
}

// seedManualCollection 造一个普通手工合集，返回其 id。
func seedManualCollection(t *testing.T, controller *Controller, name string) int64 {
	t.Helper()
	id, err := controller.store.CreateSimpleCollection(context.Background(), database.CreateSimpleCollectionParams{
		Name: name,
	})
	if err != nil {
		t.Fatalf("建普通合集失败: %v", err)
	}
	return id
}

func TestFranchiseCollectionRejectsManualEdits(t *testing.T) {
	t.Run("往作品群里加系列被拒且成员数不变", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		collectionID, _ := seedFranchiseCollection(t, controller, store)
		_, outsider, _ := seedBookFixture(t, store, rootDir, "Lib Outside", "Series Outside", "o.cbz", 3)

		rec := httptest.NewRecorder()
		controller.addSeriesToCollection(rec, requestWithRouteParam(
			http.MethodPost, "/api/collections/1/series",
			[]byte(`{"series_ids":[`+strconv.FormatInt(outsider.ID, 10)+`]}`),
			"collectionId", strconv.FormatInt(collectionID, 10)))

		if rec.Code != http.StatusForbidden {
			t.Fatalf("期望 403（作品群只读），实得 %d：放行会让这条成员在下一次重建时被静默删掉", rec.Code)
		}
		members, err := store.ListCollectionSeriesIDs(context.Background(), collectionID)
		if err != nil {
			t.Fatalf("读回成员失败: %v", err)
		}
		if len(members) != 2 {
			t.Fatalf("被拒后成员数应仍为 2，实得 %d", len(members))
		}
	})

	t.Run("删除作品群被拒且合集还在", func(t *testing.T) {
		controller, store, _, _ := newTestController(t)
		collectionID, _ := seedFranchiseCollection(t, controller, store)

		rec := httptest.NewRecorder()
		controller.deleteCollection(rec, requestWithRouteParam(
			http.MethodDelete, "/api/collections/1", nil,
			"collectionId", strconv.FormatInt(collectionID, 10)))

		if rec.Code != http.StatusForbidden {
			t.Fatalf("期望 403（作品群只读），实得 %d：放行会让重建把它换个 id 建回来", rec.Code)
		}
		if snaps := readFranchises(t, store); len(snaps) != 1 || snaps[0].ID != collectionID {
			t.Fatalf("被拒后作品群合集应原样保留 id=%d，实得 %+v", collectionID, snaps)
		}
	})

	t.Run("从作品群移除系列被拒", func(t *testing.T) {
		controller, store, _, _ := newTestController(t)
		collectionID, ids := seedFranchiseCollection(t, controller, store)

		rec := httptest.NewRecorder()
		controller.removeSeriesFromCollection(rec, requestWithRouteParams(
			http.MethodDelete, "/api/collections/1/series/1", nil, map[string]string{
				"collectionId": strconv.FormatInt(collectionID, 10),
				"seriesId":     strconv.FormatInt(ids[0], 10),
			}))

		if rec.Code != http.StatusForbidden {
			t.Fatalf("期望 403（作品群只读），实得 %d", rec.Code)
		}
	})

	t.Run("改名作品群被拒", func(t *testing.T) {
		controller, store, _, _ := newTestController(t)
		collectionID, _ := seedFranchiseCollection(t, controller, store)

		rec := httptest.NewRecorder()
		controller.updateCollection(rec, requestWithRouteParam(
			http.MethodPut, "/api/collections/1", []byte(`{"name":"我起的名字"}`),
			"collectionId", strconv.FormatInt(collectionID, 10)))

		if rec.Code != http.StatusForbidden {
			t.Fatalf("期望 403（作品群只读），实得 %d：重建的 upsert 会把名字覆盖回去", rec.Code)
		}
	})

	// 反向判据：闸门只能挡住推导型合集，普通合集的增删改必须一如既往。
	t.Run("普通合集的增删改不退化", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, series, _ := seedBookFixture(t, store, rootDir, "Lib A", "Series Alpha", "a.cbz", 6)
		collectionID := seedManualCollection(t, controller, "我的收藏")

		addRec := httptest.NewRecorder()
		controller.addSeriesToCollection(addRec, requestWithRouteParam(
			http.MethodPost, "/api/collections/1/series",
			[]byte(`{"series_ids":[`+strconv.FormatInt(series.ID, 10)+`]}`),
			"collectionId", strconv.FormatInt(collectionID, 10)))
		if addRec.Code != http.StatusOK {
			t.Fatalf("普通合集加系列应 200，实得 %d", addRec.Code)
		}

		updateRec := httptest.NewRecorder()
		controller.updateCollection(updateRec, requestWithRouteParam(
			http.MethodPut, "/api/collections/1", []byte(`{"name":"改过的收藏"}`),
			"collectionId", strconv.FormatInt(collectionID, 10)))
		if updateRec.Code != http.StatusOK {
			t.Fatalf("普通合集改名应 200，实得 %d", updateRec.Code)
		}

		removeRec := httptest.NewRecorder()
		controller.removeSeriesFromCollection(removeRec, requestWithRouteParams(
			http.MethodDelete, "/api/collections/1/series/1", nil, map[string]string{
				"collectionId": strconv.FormatInt(collectionID, 10),
				"seriesId":     strconv.FormatInt(series.ID, 10),
			}))
		if removeRec.Code != http.StatusOK {
			t.Fatalf("普通合集移除系列应 200，实得 %d", removeRec.Code)
		}

		deleteRec := httptest.NewRecorder()
		controller.deleteCollection(deleteRec, requestWithRouteParam(
			http.MethodDelete, "/api/collections/1", nil,
			"collectionId", strconv.FormatInt(collectionID, 10)))
		if deleteRec.Code != http.StatusOK {
			t.Fatalf("普通合集删除应 200，实得 %d", deleteRec.Code)
		}
	})

	// 智能书架快照（smart_snapshot）与 AI 分组（ai_grouping）是一次性固化的产物，
	// 落地后归用户所有、没有任何重建会覆盖它们——闸门不能顺手把它们一起锁上。
	t.Run("一次性固化的快照合集仍可编辑", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, series, _ := seedBookFixture(t, store, rootDir, "Lib A", "Series Alpha", "a.cbz", 6)
		created, err := controller.store.CreateCollection(context.Background(), database.CreateCollectionParams{
			Name:       "快照",
			SourceType: "smart_snapshot",
		})
		if err != nil {
			t.Fatalf("建快照合集失败: %v", err)
		}

		addRec := httptest.NewRecorder()
		controller.addSeriesToCollection(addRec, requestWithRouteParam(
			http.MethodPost, "/api/collections/1/series",
			[]byte(`{"series_ids":[`+strconv.FormatInt(series.ID, 10)+`]}`),
			"collectionId", strconv.FormatInt(created.ID, 10)))
		if addRec.Code != http.StatusOK {
			t.Fatalf("快照合集加系列应 200，实得 %d", addRec.Code)
		}
		_ = store
	})
}

func TestListCollectionsHidesFranchise(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	franchiseID, _ := seedFranchiseCollection(t, controller, store)
	manualID := seedManualCollection(t, controller, "我的收藏")

	rec := httptest.NewRecorder()
	controller.listCollections(rec, httptest.NewRequest(http.MethodGet, "/api/collections/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应 200，实得 %d", rec.Code)
	}

	var items []Collection
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("解析合集列表失败: %v", err)
	}
	seen := make(map[int64]string, len(items))
	for _, item := range items {
		seen[item.ID] = item.SourceType
	}
	if _, ok := seen[franchiseID]; ok {
		t.Fatalf("/api/collections 不该列出作品群合集 id=%d：它是「加入合集」弹窗的数据源，列出来就点得到", franchiseID)
	}
	if _, ok := seen[manualID]; !ok {
		t.Fatalf("/api/collections 必须仍列出普通合集 id=%d，实得 %+v", manualID, seen)
	}
}

// 作品群 id 在「人工删除被拒 → 再次重建」这条路径前后必须一致。
func TestFranchiseIDStableAcrossRejectedDeleteAndRebuild(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	collectionID, _ := seedFranchiseCollection(t, controller, store)

	rec := httptest.NewRecorder()
	controller.deleteCollection(rec, requestWithRouteParam(
		http.MethodDelete, "/api/collections/1", nil,
		"collectionId", strconv.FormatInt(collectionID, 10)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("期望删除被拒 403，实得 %d", rec.Code)
	}

	if err := controller.RebuildFranchiseCollections(ctx); err != nil {
		t.Fatalf("再次重建失败: %v", err)
	}
	snaps := readFranchises(t, store)
	if len(snaps) != 1 || snaps[0].ID != collectionID {
		t.Fatalf("重建后作品群 id 应仍为 %d，实得 %+v —— Mihon/OPDS 按 id 记的条目会失效", collectionID, snaps)
	}
}
