// 本文件守卫 KOReader 设备冲突列表里的 id 语义。
//
// 这个列表是 koreader_progress 与 koreader_sync_events 的 UNION ALL，两张表各有
// 独立的 AUTOINCREMENT 序列、都从 1 开始——同一个 id 同时是一条进度和一条事件的合法
// 主键，不是理论可能，是常态。id 不能是裸整数：前端会拿它当进度主键传给「重置进度」，
// 删掉的会是另一台设备的阅读进度。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/database"
)

func TestKOReaderConflictIDsAreNotBareProgressPrimaryKeys(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatalf("需要 *SqlStore 播种，得到 %T", store)
	}
	db := sqlStore.DB()

	// 一条未匹配的进度（koreader_progress.id = 1）。
	if _, err := db.ExecContext(ctx, `
		INSERT INTO koreader_progress (id, username, document, progress, percentage, device, device_id, book_id)
		VALUES (1, 'reader', 'docA', '1', 0.5, 'Kobo', 'dev-a', NULL)
	`); err != nil {
		t.Fatalf("播种进度: %v", err)
	}
	// 一条同步失败事件（koreader_sync_events.id = 1，与上面那条进度**同号**）。
	if _, err := db.ExecContext(ctx, `
		INSERT INTO koreader_sync_events (id, direction, username, document, status, message)
		VALUES (1, 'push', 'reader', 'docB', 'auth_failed', 'bad key')
	`); err != nil {
		t.Fatalf("播种事件: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.getKOReaderDeviceDiagnostics(rec,
		httptest.NewRequest(http.MethodGet, "/api/system/koreader/devices", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Conflicts []map[string]any `json:"conflicts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if len(payload.Conflicts) != 2 {
		t.Fatalf("期望 2 条冲突，实际 %d：%s", len(payload.Conflicts), rec.Body.String())
	}

	for _, item := range payload.Conflicts {
		// 判据一（先跑，且非破坏性）：id 绝不能是裸数字。
		// 是裸数字就意味着它会被当成 koreader_progress 主键传给重置端点。
		if raw, isNumber := item["id"].(float64); isNumber {
			t.Fatalf("conflicts[].id 仍是裸数字 %v —— 两张表的主键会重号，"+
				"前端把它当进度主键传给「重置进度」会删掉另一台设备的阅读进度", raw)
		}
		if _, hasSource := item["source_table"].(string); !hasSource {
			t.Errorf("缺 source_table —— 客户端无从判断这一行来自哪张表：%v", item)
		}
	}

	// 判据二：事件那一行不该带上同号进度的 progress_id。
	// docB 上没有任何进度记录，所以它的 progress_id 必须缺失。
	var eventItem map[string]any
	for _, item := range payload.Conflicts {
		if item["type"] == "sync_error" {
			eventItem = item
		}
	}
	if eventItem == nil {
		t.Fatal("找不到 sync_error 那一行")
	}
	if pid, ok := eventItem["progress_id"]; ok {
		t.Errorf("事件行带上了 progress_id=%v —— docB 上根本没有进度记录，"+
			"这个值来自同号的另一条记录，拿它去删就是删错人的数据", pid)
	}

	// 反向护栏：真正的未匹配进度行必须带上可用的 progress_id，否则「重置」功能就废了。
	var progressItem map[string]any
	for _, item := range payload.Conflicts {
		if item["type"] == "unmatched_progress" {
			progressItem = item
		}
	}
	if progressItem == nil {
		t.Fatal("找不到 unmatched_progress 那一行")
	}
	pid, ok := progressItem["progress_id"].(float64)
	if !ok || int64(pid) != 1 {
		t.Errorf("未匹配进度行的 progress_id = %v，期望 1 —— 没有它「重置进度」就没法用",
			progressItem["progress_id"])
	}
}
