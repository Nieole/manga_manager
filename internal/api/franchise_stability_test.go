// 业务说明：本文件守卫 franchise（系列关系派生的系统合集）的身份稳定性。
//
// 这条重建路径在**每次**新增/删除/修改系列关系时都会跑一遍，而它此前是「整体 DELETE
// 再全部重建」：用户加一条关系，所有 franchise 合集就换一遍 id。而合集 id 是对外暴露的
// ——Mihon 的 /mihon/collections/{id}/series 与 OPDS 的 /opds/collections/{id} 都按它取数，
// 客户端书库里记下的条目会集体失效；站内收藏的合集链接同理。
//
// 另一半是合集名：它取自连通分量的「第一个」成员，而那个「第一个」来自 Go 的 map 迭代
// ——顺序随机。同一批关系每次重建都可能算出不同的名字，用户看到合集名无缘无故变来变去。

package api

import (
	"context"
	"testing"

	"manga-manager/internal/database"
)

// seedFranchiseSeries 造 n 个同库系列，返回其 id（升序）。
func seedFranchiseSeries(t *testing.T, store database.Store, count int) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	_, first, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series 0", "s0.cbz", 5)

	ids := []int64{first.ID}
	for i := 1; i < count; i++ {
		name := "Series " + string(rune('A'+i))
		series, err := store.CreateSeries(ctx, database.CreateSeriesParams{
			LibraryID:   first.LibraryID,
			Name:        name,
			Path:        t.TempDir(),
			NameInitial: database.SeriesInitial("", name),
		})
		if err != nil {
			t.Fatalf("CreateSeries %d: %v", i, err)
		}
		ids = append(ids, series.ID)
	}
	return first.LibraryID, ids
}

func addRelation(t *testing.T, store database.Store, source, target int64) {
	t.Helper()
	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatalf("需要 *SqlStore 播种，得到 %T", store)
	}
	if _, err := sqlStore.DB().ExecContext(context.Background(),
		`INSERT INTO series_relations (source_series_id, target_series_id, relation_type) VALUES (?, ?, 'spinoff')`,
		source, target); err != nil {
		t.Fatalf("insert relation: %v", err)
	}
}

// franchiseSnapshot 读回全部 franchise 合集的 (id, name, 成员集合)。
type franchiseSnapshot struct {
	ID      int64
	Name    string
	Members []int64
}

func readFranchises(t *testing.T, store database.Store) []franchiseSnapshot {
	t.Helper()
	ctx := context.Background()
	sqlStore := store.(*database.SqlStore)
	rows, err := sqlStore.DB().QueryContext(ctx,
		`SELECT id, name FROM collections WHERE source_type = 'system_franchise' ORDER BY id`)
	if err != nil {
		t.Fatalf("query collections: %v", err)
	}
	defer rows.Close()

	var out []franchiseSnapshot
	for rows.Next() {
		var snap franchiseSnapshot
		if err := rows.Scan(&snap.ID, &snap.Name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	for i := range out {
		members, err := store.ListCollectionSeriesIDs(ctx, out[i].ID)
		if err != nil {
			t.Fatalf("ListCollectionSeriesIDs: %v", err)
		}
		out[i].Members = members
	}
	return out
}

func TestFranchiseRebuildKeepsCollectionIdentity(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	_, ids := seedFranchiseSeries(t, store, 5)
	// 两个互不相连的 franchise：{ids0,ids1} 与 {ids2,ids3}。
	addRelation(t, store, ids[0], ids[1])
	addRelation(t, store, ids[2], ids[3])

	if err := controller.RebuildFranchiseCollections(ctx); err != nil {
		t.Fatalf("首次重建: %v", err)
	}
	before := readFranchises(t, store)
	if len(before) != 2 {
		t.Fatalf("期望 2 个 franchise 合集，实际 %d", len(before))
	}

	// 关系没变，重建三次：id 与名字都必须原样不动。
	for i := 0; i < 3; i++ {
		if err := controller.RebuildFranchiseCollections(ctx); err != nil {
			t.Fatalf("第 %d 次重建: %v", i+2, err)
		}
		after := readFranchises(t, store)
		if len(after) != len(before) {
			t.Fatalf("第 %d 次重建后合集数变成 %d", i+2, len(after))
		}
		for j := range before {
			if after[j].ID != before[j].ID {
				t.Fatalf("第 %d 次重建后合集 id 从 %d 变成 %d —— Mihon/OPDS 客户端按 id 记的书库条目会集体失效",
					i+2, before[j].ID, after[j].ID)
			}
			if after[j].Name != before[j].Name {
				t.Fatalf("第 %d 次重建后合集名从 %q 变成 %q —— 名字取自随机的 map 迭代起点",
					i+2, before[j].Name, after[j].Name)
			}
		}
	}
}

func TestFranchiseRebuildKeepsUnrelatedCollectionsStableWhenOneChanges(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	_, ids := seedFranchiseSeries(t, store, 6)
	addRelation(t, store, ids[0], ids[1]) // franchise A
	addRelation(t, store, ids[2], ids[3]) // franchise B
	if err := controller.RebuildFranchiseCollections(ctx); err != nil {
		t.Fatalf("首次重建: %v", err)
	}
	before := readFranchises(t, store)
	if len(before) != 2 {
		t.Fatalf("期望 2 个合集，实际 %d", len(before))
	}
	idByFirstMember := map[int64]int64{}
	for _, snap := range before {
		idByFirstMember[snap.Members[0]] = snap.ID
	}

	// 只给 franchise B 加一个成员：A 的 id 不该受影响。
	// 这正是「加一条关系就把所有合集 id 洗一遍」的反面。
	addRelation(t, store, ids[3], ids[4])
	if err := controller.RebuildFranchiseCollections(ctx); err != nil {
		t.Fatalf("二次重建: %v", err)
	}
	after := readFranchises(t, store)
	if len(after) != 2 {
		t.Fatalf("期望仍是 2 个合集，实际 %d", len(after))
	}
	for _, snap := range after {
		want, ok := idByFirstMember[snap.Members[0]]
		if !ok {
			continue
		}
		if snap.ID != want {
			t.Fatalf("最小成员为 %d 的合集 id 从 %d 变成 %d —— 只改了另一个 franchise，这个不该动",
				snap.Members[0], want, snap.ID)
		}
	}
	// 新成员确实进了 B。
	for _, snap := range after {
		if snap.Members[0] != ids[2] {
			continue
		}
		if len(snap.Members) != 3 {
			t.Fatalf("franchise B 的成员数 = %d，期望 3", len(snap.Members))
		}
	}
}

func TestFranchiseRebuildDropsCollectionsThatNoLongerExist(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	_, ids := seedFranchiseSeries(t, store, 4)
	addRelation(t, store, ids[0], ids[1])
	addRelation(t, store, ids[2], ids[3])
	if err := controller.RebuildFranchiseCollections(ctx); err != nil {
		t.Fatalf("首次重建: %v", err)
	}
	if got := len(readFranchises(t, store)); got != 2 {
		t.Fatalf("期望 2 个合集，实际 %d", got)
	}

	// 删掉第二组关系：那个合集应当消失，而不是留成一个空壳。
	sqlStore := store.(*database.SqlStore)
	if _, err := sqlStore.DB().ExecContext(ctx,
		`DELETE FROM series_relations WHERE source_series_id = ?`, ids[2]); err != nil {
		t.Fatalf("删关系: %v", err)
	}
	if err := controller.RebuildFranchiseCollections(ctx); err != nil {
		t.Fatalf("二次重建: %v", err)
	}
	after := readFranchises(t, store)
	if len(after) != 1 {
		t.Fatalf("期望只剩 1 个合集，实际 %d —— 过期的系统合集必须被清掉，否则会留下空壳", len(after))
	}
	if after[0].Members[0] != ids[0] {
		t.Fatalf("留下的是最小成员 %d 的合集，期望 %d", after[0].Members[0], ids[0])
	}
}

func TestFranchiseRebuildRemovesMembersThatLeft(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	_, ids := seedFranchiseSeries(t, store, 4)
	addRelation(t, store, ids[0], ids[1])
	addRelation(t, store, ids[1], ids[2])
	if err := controller.RebuildFranchiseCollections(ctx); err != nil {
		t.Fatalf("首次重建: %v", err)
	}
	before := readFranchises(t, store)
	if len(before) != 1 || len(before[0].Members) != 3 {
		t.Fatalf("期望 1 个合集 3 个成员，实际 %+v", before)
	}

	// 断开 ids[2]：它应当从成员里被移除，而合集本身（id）不变。
	sqlStore := store.(*database.SqlStore)
	if _, err := sqlStore.DB().ExecContext(ctx,
		`DELETE FROM series_relations WHERE source_series_id = ? AND target_series_id = ?`,
		ids[1], ids[2]); err != nil {
		t.Fatalf("删关系: %v", err)
	}
	if err := controller.RebuildFranchiseCollections(ctx); err != nil {
		t.Fatalf("二次重建: %v", err)
	}
	after := readFranchises(t, store)
	if len(after) != 1 {
		t.Fatalf("期望仍是 1 个合集，实际 %d", len(after))
	}
	if after[0].ID != before[0].ID {
		t.Fatalf("成员变化不该换合集 id（%d -> %d）", before[0].ID, after[0].ID)
	}
	if len(after[0].Members) != 2 {
		t.Fatalf("成员数 = %d，期望 2 —— 差集删除没生效，离开的成员留在合集里", len(after[0].Members))
	}
}

// TestFranchiseRebuildReplacesLegacyKeylessRows 覆盖升级路径：
// 升级前建的行没有 source_key，必须被换成带键的行，而不是与新行并存。
func TestFranchiseRebuildReplacesLegacyKeylessRows(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	_, ids := seedFranchiseSeries(t, store, 3)
	addRelation(t, store, ids[0], ids[1])

	// 手工造一条「旧版」行：source_type 对得上但没有键。
	sqlStore := store.(*database.SqlStore)
	if _, err := sqlStore.DB().ExecContext(ctx,
		`INSERT INTO collections (name, description, source_type, source_key) VALUES ('Legacy Franchise', '', 'system_franchise', '')`); err != nil {
		t.Fatalf("播种旧行: %v", err)
	}

	if err := controller.RebuildFranchiseCollections(ctx); err != nil {
		t.Fatalf("重建: %v", err)
	}

	after := readFranchises(t, store)
	if len(after) != 1 {
		t.Fatalf("期望只剩 1 个合集，实际 %d —— 无键的旧行必须被清掉，否则界面上会出现两份同一个 franchise", len(after))
	}
	if after[0].Name == "Legacy Franchise" {
		t.Fatal("留下的是那条无键旧行")
	}
}
