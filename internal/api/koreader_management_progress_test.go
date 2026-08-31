package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"manga-manager/internal/database"
)

// TestKOReaderManagementEndpointsWithStoredProgress 覆盖「koreader_progress 表里已有进度记录」这一前提。
// 聚合时间列（MAX(updated_at)）在标量子查询里没有 decltype，驱动按 string 返回，
// 三个 KOReader 管理端点各自扫描这些列，任一处扫进 time.Time 都会让整个面板 500。
func TestKOReaderManagementEndpointsWithStoredProgress(t *testing.T) {
	controller, store, _, _ := newTestController(t)

	account := createTestKOReaderAccount(t, controller, "reader")
	if _, err := store.UpsertKOReaderProgress(context.Background(), database.UpsertKOReaderProgressParams{
		Username:   account.Username,
		Document:   "/mnt/koreader/Unknown/Vol1/Book.cbz",
		Progress:   "p-1",
		Percentage: 0.42,
		Device:     "Boox",
		DeviceID:   "DEVICE-X",
		Timestamp:  time.Now().Unix(),
		RawPayload: `{"document":"unknown"}`,
	}); err != nil {
		t.Fatalf("UpsertKOReaderProgress failed: %v", err)
	}

	// CURRENT_TIMESTAMP 写的是 UTC 秒级文本，窗口放宽以容忍慢机器。
	now := time.Now().UTC()
	assertFresh := func(t *testing.T, label string, value time.Time) {
		t.Helper()
		if value.IsZero() {
			t.Fatalf("%s 解析出零值时间", label)
		}
		if delta := now.Sub(value.UTC()); delta < -time.Minute || delta > 10*time.Minute {
			t.Fatalf("%s 解析出的时间 %s 偏离当前时间 %s 太远", label, value, now)
		}
	}

	t.Run("设置页 KOReader 面板返回可用的最近同步时间", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.getKOReaderSettings(rec, httptest.NewRequest(http.MethodGet, "/api/system/koreader", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected koreader settings 200, got %d body=%s", rec.Code, rec.Body.String())
		}

		var resp KOReaderSystemResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode koreader settings failed: %v", err)
		}
		if resp.Stats.MatchedProgressCount+resp.Stats.UnmatchedProgressCount != 1 {
			t.Fatalf("expected 1 progress row in stats, got %+v", resp.Stats)
		}
		if !resp.Stats.LatestSyncAt.Valid {
			t.Fatalf("latest_sync_at 应带回真实时间，实际是空值：%+v", resp.Stats)
		}
		assertFresh(t, "latest_sync_at", resp.Stats.LatestSyncAt.Time)
	})

	t.Run("账号列表返回可用的最近使用时间", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.listKOReaderAccounts(rec, httptest.NewRequest(http.MethodGet, "/api/system/koreader/accounts", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected koreader accounts 200, got %d body=%s", rec.Code, rec.Body.String())
		}

		var accounts []KOReaderAccountResponse
		if err := json.NewDecoder(rec.Body).Decode(&accounts); err != nil {
			t.Fatalf("decode koreader accounts failed: %v", err)
		}
		if len(accounts) != 1 {
			t.Fatalf("expected 1 account, got %d", len(accounts))
		}
		if accounts[0].LastUsedAt == nil {
			t.Fatalf("last_used_at 应带回真实时间，实际缺失：%+v", accounts[0])
		}
		lastUsedAt, err := time.Parse(time.RFC3339, *accounts[0].LastUsedAt)
		if err != nil {
			t.Fatalf("last_used_at 不是 RFC3339：%q (%v)", *accounts[0].LastUsedAt, err)
		}
		assertFresh(t, "last_used_at", lastUsedAt)
	})

	t.Run("连接中心返回 KOReader 账号计数", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.getClientConnections(rec, httptest.NewRequest(http.MethodGet, "/api/system/client-connections", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected client connections 200, got %d body=%s", rec.Code, rec.Body.String())
		}

		var resp ClientConnectionsResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode client connections failed: %v", err)
		}
		if resp.Status.KOReaderAccountCount != 1 {
			t.Fatalf("expected 1 koreader account in status, got %+v", resp.Status)
		}
	})
}
