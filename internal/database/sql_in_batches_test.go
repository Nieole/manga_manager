// 本文件守卫 IN (...) 展开的分批。
//
// SQLite 的 SQLITE_MAX_VARIABLE_NUMBER 是 32766，超过就直接报 "too many SQL variables"。
// 批量编辑与进度叠加的 ID 数量都由用户的选区决定，不分批意味着「选中三万多个系列」
// 这种完全合理的操作会整个失败。

package database

import (
	"context"
	"testing"
)

func TestSQLInBatchesChunking(t *testing.T) {
	// 只覆盖切分算术本身；「语句真的被分批执行了」由下面跑真库的大 N 用例间接保证。
	cases := []struct {
		n           int
		wantBatches int
		wantLast    int
	}{
		{0, 0, 0},
		{1, 1, 1},
		{sqlInBatchSize, 1, sqlInBatchSize},
		{sqlInBatchSize + 1, 2, 1},
		{40000, 80, 0},
	}
	for _, tc := range cases {
		ids := make([]int64, tc.n)
		for i := range ids {
			ids[i] = int64(i + 1)
		}
		var batches []int
		if err := sqlInBatches(ids, func(_ string, args []interface{}) error {
			batches = append(batches, len(args))
			return nil
		}); err != nil {
			t.Fatalf("n=%d: %v", tc.n, err)
		}
		if len(batches) != tc.wantBatches {
			t.Fatalf("n=%d 分了 %d 批, want %d", tc.n, len(batches), tc.wantBatches)
		}
		total := 0
		for _, b := range batches {
			if b > sqlInBatchSize {
				t.Fatalf("n=%d 出现了 %d 个参数的批次，超过上限 %d", tc.n, b, sqlInBatchSize)
			}
			total += b
		}
		if total != tc.n {
			t.Fatalf("n=%d 分批后参数总数 %d，与输入不符", tc.n, total)
		}
		if tc.wantLast > 0 && batches[len(batches)-1] != tc.wantLast {
			t.Fatalf("n=%d 最后一批 %d 个, want %d", tc.n, batches[len(batches)-1], tc.wantLast)
		}
	}
}

// TestBulkOperationsExceedVariableLimit 是真正的判据：用超过 SQLite 变量上限的 ID 数
// 跑真实的进度查询。未分批时调用会直接报 "too many SQL variables"。
func TestBulkOperationsExceedVariableLimit(t *testing.T) {
	// 32766 是 SQLITE_MAX_VARIABLE_NUMBER；取 40000 确保越过它。
	const idCount = 40000

	t.Run("GetUserBookProgressMap", func(t *testing.T) {
		store := newExecTxTestStore(t)
		ctx := context.Background()
		user, err := store.CreateUser(ctx, CreateUserParams{
			Username: "reader", PasswordHash: "x", Role: "regular", DisplayName: "R",
		})
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}

		ids := make([]int64, idCount)
		for i := range ids {
			ids[i] = int64(i + 1)
		}
		got, err := store.GetUserBookProgressMap(ctx, user.ID, ids)
		if err != nil {
			t.Fatalf("GetUserBookProgressMap(%d ids): %v", idCount, err)
		}
		if len(got) != 0 {
			t.Fatalf("没有任何进度记录时应返回空 map，得到 %d 条", len(got))
		}
	})
}
