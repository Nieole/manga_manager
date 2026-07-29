// 业务说明：本文件守卫「部分应用不会让提案永久消失」。
//
// bulk-apply 的默认模式是 fill_empty：只写当前值为空的字段。但此前它写完子集就把**整条**
// review 标成 applied，而收件箱只查 pending——被筛掉的提案从此在界面上彻底消失，
// 用户既看不到也没法再应用，除非重新刮削一遍。单条 apply 遇到部分锁定字段时同理。
//
// 现在只有全部提案都处理完才关单；否则删掉已写入的字段行，让 review 带着剩下的提案继续 pending。
// 删行而不是留着，是因为已应用字段的 current_value 已经过时，留下会在收件箱里陈列一个
// 「当前值 → 提案值」都相同的假 diff。

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// seedPartialApplyFixture 造一个「title 已有值、summary/publisher 为空」的系列，
// 并入队一条同时提案这三个字段的审核。fill_empty 会写后两个、筛掉 title。
func seedPartialApplyFixture(t *testing.T, controller *Controller, store database.Store, name string) (database.Series, database.MetadataReview) {
	t.Helper()
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib-"+name, name, "book-"+name+".cbz", 10)

	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatalf("需要 *SqlStore 播种，得到 %T", store)
	}
	if _, err := sqlStore.DB().ExecContext(ctx,
		`UPDATE series SET title = ?, summary = '', publisher = '' WHERE id = ?`,
		"Existing Title", series.ID); err != nil {
		t.Fatalf("播种系列失败: %v", err)
	}
	series, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}

	review, _, _, err := controller.queueMetadataReview(ctx, series, &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Title:      "External Title",
		Summary:    "External summary",
		Publisher:  "External Publisher",
		SourceID:   7,
		SourceURL:  "https://bgm.tv/subject/7",
		Confidence: 0.8,
	}, "bangumi", name)
	if err != nil {
		t.Fatalf("queueMetadataReview: %v", err)
	}
	return series, review
}

// TestFillEmptyKeepsUnappliedProposals 是本条的核心判据。
func TestFillEmptyKeepsUnappliedProposals(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, review := seedPartialApplyFixture(t, controller, store, "Alpha")

	body := []byte(`{"review_ids":[` + strconv.FormatInt(review.ID, 10) + `],"mode":"fill_empty"}`)
	rec := httptest.NewRecorder()
	controller.bulkApplyMetadataReviews(rec,
		httptest.NewRequest(http.MethodPost, "/api/metadata/reviews/bulk-apply", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var resp metadataReviewBulkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if len(resp.Partial) != 1 {
		t.Errorf("期望落 Partial 桶，实际 %+v —— 报成 Applied 会让用户以为已经处理完了", resp)
	}

	// 关键：review 必须仍是 pending，被筛掉的 title 提案还在。
	reloaded, err := store.GetMetadataReview(ctx, review.ID)
	if err != nil {
		t.Fatalf("GetMetadataReview: %v", err)
	}
	if reloaded.Status != "pending" {
		t.Fatalf("review 被关单成 %q —— 收件箱只查 pending，被筛掉的 title 提案就此永久消失，"+
			"用户想应用它只能重新刮削一遍", reloaded.Status)
	}

	fields, err := store.ListMetadataReviewFields(ctx, review.ID)
	if err != nil {
		t.Fatalf("ListMetadataReviewFields: %v", err)
	}
	names := map[string]bool{}
	for _, f := range fields {
		names[f.FieldName] = true
	}
	if !names["title"] {
		t.Error("被筛掉的 title 提案不见了")
	}
	// 已应用的字段行要删掉：留着会在收件箱里陈列一个「当前值 == 提案值」的假 diff。
	for _, applied := range []string{"summary", "publisher"} {
		if names[applied] {
			t.Errorf("已应用的 %q 仍留在待审字段里 —— 它的 current_value 已经过时，"+
				"用户会看到一条自己和自己 diff 的假提案", applied)
		}
	}

	// 值确实写进去了（反向护栏：别为了保住 review 就不写数据）。
	updated, err := store.GetSeries(ctx, review.SeriesID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if updated.Summary.String != "External summary" || updated.Publisher.String != "External Publisher" {
		t.Errorf("未被筛掉的字段没有写入：summary=%q publisher=%q",
			updated.Summary.String, updated.Publisher.String)
	}
	if updated.Title.String != "Existing Title" {
		t.Errorf("fill_empty 覆盖了已有的 title：%q", updated.Title.String)
	}
}

// TestSecondApplyClosesReviewOnceNothingRemains：把剩下的提案也应用掉之后，review 才关单。
//
// 这条同时钉住「幂等」：第二次 apply 走 mode=all，此时 summary/publisher 的行已被删除，
// 只剩 title，不该重复写入已经写过的字段。
func TestSecondApplyClosesReviewOnceNothingRemains(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, review := seedPartialApplyFixture(t, controller, store, "Beta")

	first := []byte(`{"review_ids":[` + strconv.FormatInt(review.ID, 10) + `],"mode":"fill_empty"}`)
	rec := httptest.NewRecorder()
	controller.bulkApplyMetadataReviews(rec,
		httptest.NewRequest(http.MethodPost, "/api/metadata/reviews/bulk-apply", bytes.NewReader(first)))
	if rec.Code != http.StatusOK {
		t.Fatalf("首次 HTTP %d: %s", rec.Code, rec.Body.String())
	}

	// 在两次 apply 之间手工改掉一个已应用字段：第二次不该再碰它。
	// 用具体值而不是比 updated_at——SQLite 的 CURRENT_TIMESTAMP 只有秒精度，比时间戳测不出来。
	sqlStore := store.(*database.SqlStore)
	if _, err := sqlStore.DB().ExecContext(ctx,
		`UPDATE series SET publisher = ? WHERE id = ?`, "Manual Publisher", review.SeriesID); err != nil {
		t.Fatalf("手工改 publisher 失败: %v", err)
	}

	second := []byte(`{"review_ids":[` + strconv.FormatInt(review.ID, 10) + `],"mode":"all"}`)
	rec = httptest.NewRecorder()
	controller.bulkApplyMetadataReviews(rec,
		httptest.NewRequest(http.MethodPost, "/api/metadata/reviews/bulk-apply", bytes.NewReader(second)))
	if rec.Code != http.StatusOK {
		t.Fatalf("二次 HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var resp metadataReviewBulkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if len(resp.Applied) != 1 {
		t.Errorf("剩余提案都处理完了，应当落 Applied 桶，实际 %+v", resp)
	}

	reloaded, err := store.GetMetadataReview(ctx, review.ID)
	if err != nil {
		t.Fatalf("GetMetadataReview: %v", err)
	}
	if reloaded.Status != "applied" {
		t.Errorf("提案全部处理完却没有关单（status=%q）—— 收件箱会一直挂着一条空 review",
			reloaded.Status)
	}

	updated, err := store.GetSeries(ctx, review.SeriesID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if updated.Title.String != "External Title" {
		t.Errorf("第二次没有写入剩下的 title：%q", updated.Title.String)
	}
	if updated.Publisher.String != "Manual Publisher" {
		t.Errorf("publisher 被二次写入覆盖成 %q —— 已应用的字段行应当已被删除，不该再次生效",
			updated.Publisher.String)
	}
}

// TestSingleApplyReportsPartialWhenSomeFieldsLocked：单条 apply 也走同一套语义。
func TestSingleApplyReportsPartialWhenSomeFieldsLocked(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, review := seedPartialApplyFixture(t, controller, store, "Gamma")

	// 入队之后锁住 summary：它不会被写入，因此 review 不能关单。
	lockSeriesFields(t, store, review.SeriesID, "summary")

	rec := httptest.NewRecorder()
	controller.applyMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
		"reviewId", strconv.FormatInt(review.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if outcome, _ := resp["outcome"].(string); outcome != "partial" {
		t.Errorf("outcome = %q，期望 partial", outcome)
	}

	reloaded, err := store.GetMetadataReview(ctx, review.ID)
	if err != nil {
		t.Fatalf("GetMetadataReview: %v", err)
	}
	if reloaded.Status != "pending" {
		t.Errorf("review 被关单成 %q —— 锁定字段的提案会随之消失，用户解锁后也找不回来",
			reloaded.Status)
	}
	fields, err := store.ListMetadataReviewFields(ctx, review.ID)
	if err != nil {
		t.Fatalf("ListMetadataReviewFields: %v", err)
	}
	if len(fields) != 1 || fields[0].FieldName != "summary" {
		t.Errorf("剩余待审字段 = %+v，期望只剩被锁的 summary", fields)
	}
}
