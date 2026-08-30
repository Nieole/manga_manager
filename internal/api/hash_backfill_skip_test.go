// 守两处哈希回填在**指纹算不出来**那条出口上的不变量：这一本跳过，而**分页游标必须照常推进**。
// 批次按 id > afterID 切，游标停住即死循环——坏书排在批次末尾时任务再也走不到尽头。
// 因此游标推进与 continue 必须挂闭包捕获的 hashErr，不得挂**磁盘作业**入口的返回值。

package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/database"
)

func TestHashBackfillSkipsUnreadableBookAndAdvancesCursor(t *testing.T) {
	cases := []struct {
		name string
		run  func(context.Context, *Controller) (int, int, error)
	}{
		{
			name: "快速哈希",
			run: func(ctx context.Context, c *Controller) (int, int, error) {
				return c.runRebuildFileIdentities(ctx, 500, newRecordingTaskHandle(c.diskWork))
			},
		},
		{
			name: "全量哈希",
			run: func(ctx context.Context, c *Controller) (int, int, error) {
				return c.runBackfillFullHashesLowPriority(ctx, 500, 0, newRecordingTaskHandle(c.diskWork))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 坏书必须排在**最后**：它指向一个不存在的文件，指纹必然算不出来，而只有它是这一批的
			// 末尾时，「跳过时不推游标」才真的会把循环钉死——排在中间的话，后面那本好书会替它把
			// 游标推过去，缺陷被顺手掩盖。
			good := seedIdentityCandidates(t, 1)[0]
			bad := database.BookIdentityCandidate{ID: good.ID + 1, Path: filepath.Join(t.TempDir(), "missing.cbz")}
			store := &maintenanceStore{candidates: []database.BookIdentityCandidate{good, bad}}
			c, _, _ := newMaintenanceRig(t, store)

			// 超时当守卫：游标失守时这两条是超时失败，而不是把整个包挂死。
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			updated, total, err := tc.run(ctx, c)
			if err != nil {
				t.Fatalf("回填返回 %v, want nil —— 超时说明坏书上的分页游标没推进，同一本被永远切回来", err)
			}
			if updated != 1 || total != 2 {
				t.Fatalf("回填结果为 updated=%d total=%d, want 1/2 —— 坏书该跳过、好书该落库", updated, total)
			}
			if len(store.updated) != 1 || store.updated[0].ID != good.ID {
				t.Fatalf("落库了 %+v, want 只有 id=%d 那本好书", store.updated, good.ID)
			}
		})
	}
}
