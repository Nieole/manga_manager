// 本文件守批量应用的事务边界：一次请求里逐条应用，前一条写进系列的值对后一条可见。
//
// 整批共用一份入队时的快照，同一系列上的第二条提案就会把第一条刚写进去的值盖掉——
// 而「只填空字段」是批量条的默认模式，用户全选一次就丢数据。

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/proposal"
)

// queueSummaryProposal 入队一条只提案 summary 的提案：一次应用写了什么，读系列的简介就知道。
func queueSummaryProposal(t *testing.T, controller *Controller, series database.Series, sourceID int, summary string) database.MetadataReview {
	t.Helper()
	queued, err := controller.proposals.Queue(context.Background(), series, &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Summary:    summary,
		SourceID:   sourceID,
		SourceURL:  fmt.Sprintf("https://bgm.tv/subject/%d", sourceID),
		Confidence: 0.8,
	}, "bangumi", series.Name, proposal.QueueOptions{})
	if err != nil {
		t.Fatalf("入队: %v", err)
	}
	if queued.Status != proposal.QueueQueued {
		t.Fatalf("入队得到 status=%q，用例需要一条新建的提案", queued.Status)
	}
	return queued.Proposal
}

// TestBulkApplyFillEmptyKeepsFirstWriteOfSameSeries：同一系列两条提案一次全选，
// 先写进去的值必须留住，后一条落 Skipped 桶且仍待裁决。
func TestBulkApplyFillEmptyKeepsFirstWriteOfSameSeries(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Alpha", "book.cbz", 10)

	first := queueSummaryProposal(t, controller, series, 11, "Bangumi 的简介")
	second := queueSummaryProposal(t, controller, series, 22, "AniList 的简介")

	body, err := json.Marshal(metadataReviewBulkRequest{
		ReviewIDs: []int64{first.ID, second.ID},
		Mode:      "fill_empty",
	})
	if err != nil {
		t.Fatalf("构造请求体: %v", err)
	}
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

	updated, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if updated.Summary.String != "Bangumi 的简介" {
		t.Errorf("简介 = %q，期望 %q —— 后一条提案盖掉了前一条刚写进去的值",
			updated.Summary.String, "Bangumi 的简介")
	}
	if len(resp.Applied) != 1 || resp.Applied[0] != first.ID {
		t.Errorf("Applied = %v，期望只有第一条（%d）", resp.Applied, first.ID)
	}
	if len(resp.Skipped) != 1 || resp.Skipped[0] != second.ID {
		t.Errorf("Skipped = %v，期望第二条（%d）—— 它已无字段可写，报成已应用会让用户以为处理完了",
			resp.Skipped, second.ID)
	}
	// 收件箱是这个流程的主界面：第二条必须还在，且它展示的「当前值」是系列此刻的简介——
	// 仍旧摆着入队时的空值，用户就会看到一个「当前值：(空)」却没被填上的字段。
	inbox := listInbox(t, controller)
	item, ok := inboxItem(inbox, second.ID)
	if !ok {
		t.Fatalf("第二条不在收件箱里 —— 一个字段都没写却把它消费掉了")
	}
	field, ok := inboxField(item, "summary")
	if !ok {
		t.Fatalf("第二条上没有 summary 字段，实际有 %d 个", len(item.Fields))
	}
	if field.Current != "Bangumi 的简介" {
		t.Errorf("收件箱展示的当前值 = %q，期望 %q —— 展示的是入队瞬间的快照，与应用时的判据不一致",
			field.Current, "Bangumi 的简介")
	}
}

// listInbox 走收件箱那条 HTTP 入口取一页。
func listInbox(t *testing.T, controller *Controller) metadataReviewInboxResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	controller.listMetadataReviewInbox(rec,
		httptest.NewRequest(http.MethodGet, "/api/metadata/reviews/inbox?limit=30", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("收件箱 HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var resp metadataReviewInboxResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析收件箱响应: %v", err)
	}
	return resp
}

func inboxItem(page metadataReviewInboxResponse, reviewID int64) (metadataReviewInboxItemView, bool) {
	for _, item := range page.Items {
		if item.ID == reviewID {
			return item, true
		}
	}
	return metadataReviewInboxItemView{}, false
}

func inboxField(item metadataReviewInboxItemView, name string) (metadataReviewFieldView, bool) {
	for _, field := range item.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return metadataReviewFieldView{}, false
}
