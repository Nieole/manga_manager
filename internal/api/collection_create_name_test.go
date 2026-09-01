// 守「哪里都不许出现两个同名合集」这条不变量在建合集正门上的执行：名称去首尾空白、
// 空白名拒绝、重名按 SQLite NOCASE 口径判 409，同时反向守住正常建合集与描述字段不退化。

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/database"
)

func TestCreateCollectionNameHygiene(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	db := controller.store.(*database.SqlStore).DB()

	post := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/collections/", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		controller.createCollection(rec, req)
		return rec
	}

	t.Run("纯空白名被拒", func(t *testing.T) {
		rec := post(t, `{"name":"   ","description":"blank"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("空白名应当 400，实际 %d body=%s", rec.Code, rec.Body.String())
		}
		var count int
		if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM collections`).Scan(&count); err != nil {
			t.Fatalf("统计合集失败: %v", err)
		}
		if count != 0 {
			t.Fatalf("空白名不该落库，实际有 %d 条", count)
		}
	})

	t.Run("首尾空白被裁掉后落库", func(t *testing.T) {
		rec := post(t, `{"name":"  Sci-Fi  ","description":"  picked  "}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("期望 201，实际 %d body=%s", rec.Code, rec.Body.String())
		}
		var created map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
			t.Fatalf("解析建合集响应失败: %v", err)
		}
		if created["name"] != "Sci-Fi" {
			t.Fatalf("响应里的名称应当已裁掉空白，实际 %#v", created["name"])
		}
		id := int64(created["id"].(float64))
		var name, description string
		row := db.QueryRowContext(context.Background(), `SELECT name, COALESCE(description, '') FROM collections WHERE id = ?`, id)
		if err := row.Scan(&name, &description); err != nil {
			t.Fatalf("回读合集失败: %v", err)
		}
		if name != "Sci-Fi" {
			t.Fatalf("落库名称应当已裁掉空白，实际 %q", name)
		}
		if description != "picked" {
			t.Fatalf("落库描述应当已裁掉空白，实际 %q", description)
		}
	})

	t.Run("同名建号得 409", func(t *testing.T) {
		rec := post(t, `{"name":"Sci-Fi"}`)
		if rec.Code != http.StatusConflict {
			t.Fatalf("同名应当 409，实际 %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("大小写不同的同名也得 409", func(t *testing.T) {
		// 与 CollectionNameExists 的 `name = ? COLLATE NOCASE` 同口径：ASCII 大小写不敏感。
		rec := post(t, `{"name":"sci-fi"}`)
		if rec.Code != http.StatusConflict {
			t.Fatalf("大小写不同的同名应当 409，实际 %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("裁掉空白后撞名也得 409", func(t *testing.T) {
		rec := post(t, `{"name":"   Sci-Fi"}`)
		if rec.Code != http.StatusConflict {
			t.Fatalf("裁白后撞名应当 409，实际 %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("正常建合集与描述字段不退化", func(t *testing.T) {
		rec := post(t, `{"name":"Fantasy","description":"long running"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("不同名的合集应当建得成，实际 %d body=%s", rec.Code, rec.Body.String())
		}
		var created map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
			t.Fatalf("解析建合集响应失败: %v", err)
		}
		id := int64(created["id"].(float64))
		var name, description string
		row := db.QueryRowContext(context.Background(), `SELECT name, COALESCE(description, '') FROM collections WHERE id = ?`, id)
		if err := row.Scan(&name, &description); err != nil {
			t.Fatalf("回读合集失败: %v", err)
		}
		if name != "Fantasy" || description != "long running" {
			t.Fatalf("正常建合集退化了: name=%q description=%q", name, description)
		}
		// 空描述仍应落成 NULL，而不是被 TrimSpace 顺手改写成空串。
		emptyRec := post(t, `{"name":"Horror"}`)
		if emptyRec.Code != http.StatusCreated {
			t.Fatalf("不带描述的合集应当建得成，实际 %d body=%s", emptyRec.Code, emptyRec.Body.String())
		}
		var emptyCreated map[string]any
		if err := json.NewDecoder(emptyRec.Body).Decode(&emptyCreated); err != nil {
			t.Fatalf("解析建合集响应失败: %v", err)
		}
		var descIsNull bool
		emptyRow := db.QueryRowContext(context.Background(), `SELECT description IS NULL FROM collections WHERE id = ?`, int64(emptyCreated["id"].(float64)))
		if err := emptyRow.Scan(&descIsNull); err != nil {
			t.Fatalf("回读描述失败: %v", err)
		}
		if !descIsNull {
			t.Fatalf("空描述应当落成 NULL")
		}
	})
}
