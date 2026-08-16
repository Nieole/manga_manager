// 本文件守系列详情页的提案加载不随提案条数发起额外查询：字段行必须一次批量取回，
// 与收件箱同一条通路。

package api

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"manga-manager/internal/database"
)

// countingReviewFieldStore 统计两版字段行查询各被调用了几次。
type countingReviewFieldStore struct {
	database.Store
	perReviewCalls atomic.Int64
	batchCalls     atomic.Int64
}

func (s *countingReviewFieldStore) ListMetadataReviewFields(ctx context.Context, reviewID int64) ([]database.MetadataReviewField, error) {
	s.perReviewCalls.Add(1)
	return s.Store.ListMetadataReviewFields(ctx, reviewID)
}

func (s *countingReviewFieldStore) ListMetadataReviewFieldsByReviews(ctx context.Context, reviewIDs []int64) ([]database.MetadataReviewField, error) {
	s.batchCalls.Add(1)
	return s.Store.ListMetadataReviewFieldsByReviews(ctx, reviewIDs)
}

// TestLoadSeriesMetadataReviewBatchesFieldLookups：三条待裁决提案只该换来一次字段行查询。
//
// 顺带守住分组不改变响应：字段仍按入队顺序排列，且没有被串到别的提案上——三条提案的
// title 提案值各不相同。
func TestLoadSeriesMetadataReviewBatchesFieldLookups(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series Alpha", "a.cbz", 10)

	// 每条提案换一个 source_id 与标题，签名互不相同，三次入队各建一条。
	wantTitle := map[int64]string{}
	wantFieldOrder := map[int64][]string{}
	for i := 0; i < 3; i++ {
		result := fullScrapeResult()
		result.SourceID = 100 + i
		result.Title = fmt.Sprintf("Scraped Title %d", i)
		review, fields, isNew, err := controller.queueMetadataReview(ctx, series, result, "bangumi", "Alpha")
		if err != nil {
			t.Fatalf("入队第 %d 条: %v", i+1, err)
		}
		if !isNew {
			t.Fatalf("第 %d 条入队复用了既有提案，用例需要三条各自独立的提案", i+1)
		}
		wantTitle[review.ID] = result.Title
		for _, field := range fields {
			wantFieldOrder[review.ID] = append(wantFieldOrder[review.ID], field.FieldName)
		}
	}

	counting := &countingReviewFieldStore{Store: store}
	controller.store = counting

	payload, err := controller.loadSeriesMetadataReview(ctx, series.ID)
	if err != nil {
		t.Fatalf("loadSeriesMetadataReview: %v", err)
	}
	if len(payload.Reviews) != 3 {
		t.Fatalf("期望 3 条待裁决提案，实际 %d", len(payload.Reviews))
	}
	for _, review := range payload.Reviews {
		var gotTitle string
		gotOrder := make([]string, 0, len(review.Fields))
		for _, field := range review.Fields {
			gotOrder = append(gotOrder, field.Name)
			if field.Name == "title" {
				gotTitle = field.Proposed
			}
		}
		if gotTitle != wantTitle[review.ID] {
			t.Errorf("提案 %d 的 title 提案值是 %q，期望 %q —— 字段行被分到了别的提案上",
				review.ID, gotTitle, wantTitle[review.ID])
		}
		if !slices.Equal(gotOrder, wantFieldOrder[review.ID]) {
			t.Errorf("提案 %d 的字段顺序是 %v，期望 %v —— 分组把行的顺序打乱了",
				review.ID, gotOrder, wantFieldOrder[review.ID])
		}
	}

	if got := counting.perReviewCalls.Load(); got != 0 {
		t.Errorf("逐条取字段行 %d 次 —— 一个系列有几条待裁决提案就是几次额外查询", got)
	}
	if got := counting.batchCalls.Load(); got != 1 {
		t.Errorf("批量查询调用了 %d 次，期望 1 次", got)
	}
}
