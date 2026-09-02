// 本文件守读通路的三条规则：字段的当前值与锁定标志都按系列**此刻**的状态算，
// 以及一批提案的字段行只换来一次查询。
//
// 算错，用户会看到一个「当前值：(空)」却在应用时被跳过的字段、或一个挂着锁徽章却被照写
// 的字段；字段行逐条取则是一个系列有几条待裁决提案、就多几次查询的 N+1。

package proposal

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// countingFieldRowDB 统计批量字段行查询被调了几次。不是行为替身：底下仍是同一个真库，
// 它只在调用经过时加一。
type countingFieldRowDB struct {
	Database
	batchCalls atomic.Int64
}

func (d *countingFieldRowDB) ListMetadataReviewFieldsByReviews(ctx context.Context, reviewIDs []int64) ([]database.MetadataReviewField, error) {
	d.batchCalls.Add(1)
	return d.Database.ListMetadataReviewFieldsByReviews(ctx, reviewIDs)
}

// fieldByName 从一条提案的字段里挑出某个字段。
func fieldByName(t *testing.T, fields []Field, name string) Field {
	t.Helper()
	for _, field := range fields {
		if field.Name == name {
			return field
		}
	}
	t.Fatalf("提案里找不到字段 %q，实际有 %v", name, fieldNames(fields))
	return Field{}
}

func fieldNames(fields []Field) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
	}
	return names
}

// TestListBySeriesLocksFieldsByCurrentLockSet：锁定标志按**当前**锁定集算，不是入队快照。
//
// 「先入队、后加锁」的字段行快照恒为 false。照快照渲染，界面上就没有锁徽章，
// 用户点了应用，该字段却在写入时被静默丢弃。
func TestListBySeriesLocksFieldsByCurrentLockSet(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	seedProposal(t, svc, series, fullResult())
	// 入队之后用户才加的锁。
	lockFields(t, store, series, "publisher")

	listed, err := svc.ListBySeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("ListBySeries: %v", err)
	}
	if len(listed.Proposals) != 1 {
		t.Fatalf("列出 %d 条待裁决提案，期望 1 条", len(listed.Proposals))
	}

	fields := listed.Proposals[0].Fields
	if locked := fieldByName(t, fields, "publisher"); !locked.Locked {
		t.Error("publisher 没有锁定标志 —— 用户会以为它能被应用，点下去却被静默丢弃")
	}
	if unlocked := fieldByName(t, fields, "title"); unlocked.Locked {
		t.Error("title 带上了锁定标志，但它并不在锁定集里 —— 整份锁被串到了别的字段上")
	}
}

// TestListBySeriesBatchesFieldRowLookups：三条待裁决提案只该换来一次字段行查询。
//
// 顺带守住分组不改变内容：字段仍按入队顺序排列，且没有被串到别的提案上——
// 三条提案的 title 提案值各不相同。
func TestListBySeriesBatchesFieldRowLookups(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	// 每条提案换一个来源条目与标题，签名互不相同，三次入队各建一条。
	wantTitle := map[int64]string{}
	wantFieldOrder := map[int64][]string{}
	for i := range 3 {
		result := fullResult()
		result.SourceID = 100 + i
		result.Title = fmt.Sprintf("Scraped Title %d", i)
		queued, err := svc.Queue(ctx, series, result, "bangumi", "Alpha", QueueOptions{})
		if err != nil {
			t.Fatalf("入队第 %d 条: %v", i+1, err)
		}
		if queued.Status != QueueQueued {
			t.Fatalf("第 %d 条入队得到 status=%q，用例需要三条各自独立的提案", i+1, queued.Status)
		}
		wantTitle[queued.Proposal.ID] = result.Title
		for _, field := range queued.Fields {
			wantFieldOrder[queued.Proposal.ID] = append(wantFieldOrder[queued.Proposal.ID], field.FieldName)
		}
	}

	counting := &countingFieldRowDB{Database: testDB{Store: store}}
	listed, err := NewService(counting).ListBySeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("ListBySeries: %v", err)
	}
	if len(listed.Proposals) != 3 {
		t.Fatalf("列出 %d 条待裁决提案，期望 3 条", len(listed.Proposals))
	}
	for _, item := range listed.Proposals {
		if got := fieldByName(t, item.Fields, "title").Proposed; got != wantTitle[item.Row.ID] {
			t.Errorf("提案 %d 的 title 提案值是 %q，期望 %q —— 字段行被分到了别的提案上",
				item.Row.ID, got, wantTitle[item.Row.ID])
		}
		if got := fieldNames(item.Fields); !slices.Equal(got, wantFieldOrder[item.Row.ID]) {
			t.Errorf("提案 %d 的字段顺序是 %v，期望 %v —— 分组把行的顺序打乱了",
				item.Row.ID, got, wantFieldOrder[item.Row.ID])
		}
	}

	if got := counting.batchCalls.Load(); got != 1 {
		t.Errorf("字段行查询调用了 %d 次，期望 1 次 —— 一个系列有几条待裁决提案就是几次额外查询", got)
	}
}

// TestInboxLocksFieldsPerSeries：收件箱一页跨多个系列，锁定集必须逐行各算各的。
//
// 共用一份锁定集会把一个系列的锁串到另一个系列的提案上：用户在别处看到不该有的锁徽章，
// 或者真被锁的字段反倒没有。
func TestInboxLocksFieldsPerSeries(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	locked := seedSeries(t, store, "Lib", "Series Locked")
	open := seedSeries(t, store, "Lib Two", "Series Open")

	seedProposal(t, svc, locked, fullResult())
	seedProposal(t, svc, open, fullResult())
	lockFields(t, store, locked, "publisher")

	page, err := svc.Inbox(ctx, InboxQuery{Limit: 30})
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("收件箱有 %d 条（total=%d），期望 2 条", len(page.Items), page.Total)
	}

	for _, item := range page.Items {
		got := fieldByName(t, item.Fields, "publisher").Locked
		want := item.Row.SeriesID == locked.ID
		if got != want {
			t.Errorf("系列 %d 的 publisher 锁定标志是 %v，期望 %v —— 锁被串到了别的系列上",
				item.Row.SeriesID, got, want)
		}
	}
}

// TestInboxPagesWithoutShrinkingTotal：Total 是**过滤后、分页前**的总数。
//
// 让它跟着一页的条数走，收件箱的分页控件就只剩一页，翻不到后面的提案。
func TestInboxPagesWithoutShrinkingTotal(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	for i := range 3 {
		series := seedSeries(t, store, fmt.Sprintf("Lib %d", i), fmt.Sprintf("Series %d", i))
		seedProposal(t, svc, series, fullResult())
	}

	first, err := svc.Inbox(ctx, InboxQuery{Limit: 2})
	if err != nil {
		t.Fatalf("Inbox 第一页: %v", err)
	}
	if len(first.Items) != 2 || first.Total != 3 {
		t.Fatalf("第一页有 %d 条、total=%d，期望 2 条、total=3", len(first.Items), first.Total)
	}

	second, err := svc.Inbox(ctx, InboxQuery{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("Inbox 第二页: %v", err)
	}
	if len(second.Items) != 1 || second.Total != 3 {
		t.Fatalf("第二页有 %d 条、total=%d，期望 1 条、total=3", len(second.Items), second.Total)
	}
	if first.Items[0].Row.ID == second.Items[0].Row.ID {
		t.Error("第二页返回了第一页的第一条 —— 偏移没有生效，后面的提案永远翻不到")
	}
}

// TestPendingCountCountsOnlyPending：角标只该数待裁决的那些。
//
// 把已裁决的也算进去，角标会常亮，用户点进收件箱却什么也没有。
func TestPendingCountCountsOnlyPending(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	alpha := seedSeries(t, store, "Lib", "Series Alpha")
	beta := seedSeries(t, store, "Lib Two", "Series Beta")

	seedProposal(t, svc, alpha, fullResult())
	rejected := seedProposal(t, svc, beta, fullResult())

	count, err := svc.PendingCount(ctx)
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("待裁决计数是 %d，期望 2", count)
	}

	rejectProposal(t, store, rejected.ID)
	count, err = svc.PendingCount(ctx)
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if count != 1 {
		t.Errorf("拒绝一条之后计数仍是 %d，期望 1 —— 角标不会随裁决动作下降", count)
	}
}

// markFieldRowLocked 把字段行上的 locked 快照直改成 1，复现老库遗留的那种行。
// 生产上写不出这种行——锁定字段不入队，新行的快照恒为 0。
func markFieldRowLocked(t *testing.T, store database.Store, proposalID int64, fieldName string) {
	t.Helper()
	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatalf("需要 *SqlStore 才能直改行，得到 %T", store)
	}
	res, err := sqlStore.DB().Exec(
		`UPDATE metadata_review_fields SET locked = 1 WHERE review_id = ? AND field_name = ?`,
		proposalID, fieldName)
	if err != nil {
		t.Fatalf("直改 locked: %v", err)
	}
	if rows, err := res.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("改到 %d 行（err=%v），期望 1 行", rows, err)
	}
}

// TestReadIgnoresStaleRowLockSnapshot：行上遗留的 locked=1 是陈旧数据，读通路不认它。
//
// 认它就会与裁决那边分成两个口径：字段挂着锁徽章、也被算进徽章计数，用户以为点应用
// 不会动它，点下去它却被写进了系列。用户把该字段解锁之后，徽章还会一直挂着。
func TestReadIgnoresStaleRowLockSnapshot(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	queued := seedProposal(t, svc, series, fullResult())
	markFieldRowLocked(t, store, queued.ID, "publisher")

	listed, err := svc.ListBySeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("ListBySeries: %v", err)
	}
	if len(listed.Proposals) != 1 {
		t.Fatalf("列出 %d 条待裁决提案，期望 1 条", len(listed.Proposals))
	}
	if field := fieldByName(t, listed.Proposals[0].Fields, "publisher"); field.Locked {
		t.Error("系列详情页给 publisher 挂了锁徽章 —— 它不在系列的锁定集里，应用会照写")
	}

	page, err := svc.Inbox(ctx, InboxQuery{Limit: 30})
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("收件箱有 %d 条，期望 1 条", len(page.Items))
	}
	if field := fieldByName(t, page.Items[0].Fields, "publisher"); field.Locked {
		t.Error("收件箱给 publisher 挂了锁徽章 —— 徽章计数也会把它算进去")
	}

	// 同口径的另一半：应用确实会把它写进系列。
	res, err := svc.Apply(ctx, queued.ID, ApplyModeAll)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Status != ApplyApplied {
		t.Fatalf("Status = %q，期望 %q", res.Status, ApplyApplied)
	}
	updated, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if updated.Publisher.String != "Scraped Publisher" {
		t.Errorf("publisher = %q —— 展示说锁着、应用却照写，两处口径不一致",
			updated.Publisher.String)
	}
}

// TestReadShowsCurrentSeriesValueNotQueueSnapshot：diff 面板上的「当前值」按系列此刻的值算。
//
// 摆的是入队瞬间的快照，用户在这之后的手工编辑就全看不见：一个已经填好的字段仍显示
// 「当前值：(空)」，而「只填空字段」会跳过它——展示与行为对不上，用户无从判断点下去会发生什么。
func TestReadShowsCurrentSeriesValueNotQueueSnapshot(t *testing.T) {
	// 七个字段各摆一个入队之后才有的值，收件箱那条通路漏取一列就会露馅。
	want := map[string]string{
		"title":     "用户改的标题",
		"summary":   "用户自己写的简介",
		"publisher": "用户填的出版社",
		"status":    "completed",
		"rating":    "3.5",
		"tags":      "kept-tag",
		"authors":   "Kept Author (story)",
	}

	cases := []struct {
		name   string
		fields func(t *testing.T, svc *Service, seriesID int64) []Field
	}{
		{
			name: "按系列列出",
			fields: func(t *testing.T, svc *Service, seriesID int64) []Field {
				listed, err := svc.ListBySeries(context.Background(), seriesID)
				if err != nil {
					t.Fatalf("ListBySeries: %v", err)
				}
				if len(listed.Proposals) != 1 {
					t.Fatalf("列出 %d 条待裁决提案，期望 1 条", len(listed.Proposals))
				}
				return listed.Proposals[0].Fields
			},
		},
		{
			name: "收件箱",
			fields: func(t *testing.T, svc *Service, seriesID int64) []Field {
				page, err := svc.Inbox(context.Background(), InboxQuery{Limit: 30})
				if err != nil {
					t.Fatalf("Inbox: %v", err)
				}
				if len(page.Items) != 1 {
					t.Fatalf("收件箱有 %d 条，期望 1 条", len(page.Items))
				}
				return page.Items[0].Fields
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newTestService(t)
			series := seedSeries(t, store, "Lib", "Series Alpha")
			seedProposal(t, svc, series, fullResult())

			// 入队之后用户在详情页改了一轮。
			series.Title = nullString("用户改的标题")
			series.Summary = nullString("用户自己写的简介")
			series.Publisher = nullString("用户填的出版社")
			series.Status = nullString("completed")
			series.Rating = nullFloat(3.5)
			updateSeries(t, store, series)
			seedTags(t, store, series.ID, "kept-tag")
			seedAuthors(t, store, series.ID, metadata.SeriesAuthor{Name: "Kept Author", Role: "story"})

			for _, field := range tc.fields(t, svc, series.ID) {
				if got := field.Current; got != want[field.Name] {
					t.Errorf("字段 %q 的当前值 = %q，期望 %q —— 展示的是入队瞬间的快照",
						field.Name, got, want[field.Name])
				}
			}
		})
	}
}
