// 本文件守入队的裁决：哪些差异该变成提案、哪些该被去重挡下，以及五种结局必须彼此可区分。
//
// 分错一种，用户就会为一次完全正常的刮削看到「服务器错误」，或者被迫反复拒绝同一份提案；
// 锁定字段若混进队列，用户点了应用会被静默丢弃，整条提案却算已处理。

package proposal

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// testDB 是用例侧的适配器，与装配处那个同形——本包只认 Queries 与 Database 两个接口。
type testDB struct{ store database.Store }

func (d testDB) ExecTx(ctx context.Context, fn func(Queries) error) error {
	return d.store.ExecTx(ctx, func(q *database.Queries) error { return fn(q) })
}

// newTestService 三步装配：migrate、开库、包适配器。真 SQLite——终态守卫、去重比对与
// 级联删除的语义都在 SQL 里，内存替身测出来的是另一套东西。
func newTestService(t *testing.T) (*Service, database.Store) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	return NewService(testDB{store: store}), store
}

func seedSeries(t *testing.T, store database.Store, libName, seriesName string) database.Series {
	t.Helper()
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{
		Name:         libName,
		Path:         filepath.Join(t.TempDir(), libName),
		ScanMode:     "none",
		ScanInterval: 60,
		ScanFormats:  "cbz,zip",
	})
	if err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        seriesName,
		Path:        filepath.Join(lib.Path, seriesName),
		NameInitial: database.SeriesInitial("", seriesName),
	})
	if err != nil {
		t.Fatalf("CreateSeries failed: %v", err)
	}
	return series
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: true}
}

// lockFields 把 locked_fields 改成 fields（逗号分隔），并回读改动后的系列。
func lockFields(t *testing.T, store database.Store, series database.Series, fields string) database.Series {
	t.Helper()
	series.LockedFields = nullString(fields)
	return updateSeries(t, store, series)
}

// updateSeries 原样写回 series 的元数据列，供用例精确摆出「当前值」。
func updateSeries(t *testing.T, store database.Store, series database.Series) database.Series {
	t.Helper()
	updated, err := store.UpdateSeriesMetadata(context.Background(), database.UpdateSeriesMetadataParams{
		ID:           series.ID,
		Title:        series.Title,
		Summary:      series.Summary,
		Publisher:    series.Publisher,
		Status:       series.Status,
		Rating:       series.Rating,
		Language:     series.Language,
		LockedFields: series.LockedFields,
		NameInitial:  database.SeriesInitialFromNullTitle(series.Title, series.Name),
	})
	if err != nil {
		t.Fatalf("UpdateSeriesMetadata failed: %v", err)
	}
	return updated
}

// rejectProposal 把一条提案推进到已拒绝，走的是生产上同一条 CAS 查询。
func rejectProposal(t *testing.T, store database.Store, reviewID int64) {
	t.Helper()
	rows, err := store.ResolvePendingMetadataReview(context.Background(), database.ResolvePendingMetadataReviewParams{
		Status: "rejected",
		ID:     reviewID,
	})
	if err != nil {
		t.Fatalf("ResolvePendingMetadataReview failed: %v", err)
	}
	if rows == 0 {
		t.Fatalf("提案 %d 不在待裁决状态，拒绝没有生效", reviewID)
	}
}

// fullResult 是一份会命中全部七个字段的刮削结果。
func fullResult() *metadata.SeriesMetadata {
	return &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Title:      "Scraped Title",
		Summary:    "Scraped summary",
		Publisher:  "Scraped Publisher",
		Status:     "ongoing",
		Rating:     4.5,
		Tags:       []string{"action", "drama"},
		Authors:    []metadata.SeriesAuthor{{Name: "Someone", Role: "story"}},
		SourceID:   42,
		SourceURL:  "https://bgm.tv/subject/42",
		Confidence: 0.9,
	}
}

func queuedFieldNames(fields []database.MetadataReviewField) map[string]bool {
	names := make(map[string]bool, len(fields))
	for _, f := range fields {
		names[f.FieldName] = true
	}
	return names
}

func TestQueueCreatesProposalForNewMetadata(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	res, err := svc.Queue(ctx, series, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Status != QueueQueued {
		t.Fatalf("Status = %q，期望 %q", res.Status, QueueQueued)
	}

	queued := queuedFieldNames(res.Fields)
	for _, want := range []string{"title", "summary", "publisher", "status", "rating", "tags", "authors"} {
		if !queued[want] {
			t.Errorf("字段 %q 没有入队 —— 有差异的字段被丢掉了", want)
		}
	}

	pending, err := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("ListPendingMetadataReviewsBySeries: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != res.Proposal.ID {
		t.Fatalf("库里有 %d 条待裁决提案，期望 1 条且与返回的 id 一致", len(pending))
	}
}

// TestQueueReportsNoChangesForEmptyResult：一份什么都没给的刮削结果不该占一条队列位。
func TestQueueReportsNoChangesForEmptyResult(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	res, err := svc.Queue(ctx, series, &metadata.SeriesMetadata{Provider: "bangumi", SourceID: 42}, "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Status != QueueNoChanges {
		t.Fatalf("Status = %q，期望 %q", res.Status, QueueNoChanges)
	}

	pending, _ := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if len(pending) != 0 {
		t.Fatalf("空提案入队了 %d 条", len(pending))
	}
}

// TestQueueReportsNoChangesWhenValuesMatch：与当前值一字不差的刮削结果同样不入队。
func TestQueueReportsNoChangesWhenValuesMatch(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	series.Title = nullString("Scraped Title")
	series.Summary = nullString("Scraped summary")
	series = updateSeries(t, store, series)

	result := &metadata.SeriesMetadata{
		Provider: "bangumi",
		Title:    "Scraped Title",
		Summary:  "Scraped summary",
		SourceID: 42,
	}
	res, err := svc.Queue(ctx, series, result, "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Status != QueueNoChanges {
		t.Fatalf("Status = %q，期望 %q", res.Status, QueueNoChanges)
	}
}

// TestQueueSkipsLockedFields：锁定字段不得进入待裁决队列。
func TestQueueSkipsLockedFields(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	series = lockFields(t, store, series, "title,summary")

	res, err := svc.Queue(ctx, series, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Status != QueueQueued {
		t.Fatalf("Status = %q，期望 %q", res.Status, QueueQueued)
	}

	queued := queuedFieldNames(res.Fields)
	for _, locked := range []string{"title", "summary"} {
		if queued[locked] {
			t.Errorf("锁定字段 %q 仍被入队 —— 用户点应用后它会被静默丢弃，整条提案却算已应用", locked)
		}
	}
	// 精确断言字段名集合，而不是「都没被锁」这种恒真判断。
	for _, want := range []string{"publisher", "status", "rating", "tags", "authors"} {
		if !queued[want] {
			t.Errorf("未锁定字段 %q 没有入队 —— 过滤过头了", want)
		}
	}
}

// TestQueueReportsAllFieldsLockedOnlyWhenLocksAreTheReason：「全被锁」与「没差异」必须分开。
//
// 两者对用户意味着完全不同的动作（去解锁 vs 无需处理）。判据是「不锁就会入队」：
// 一个既没差异又恰好被锁的字段不算数，否则一次毫无差异的刮削会被报成「差异字段均已锁定」。
func TestQueueReportsAllFieldsLockedOnlyWhenLocksAreTheReason(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	series = lockFields(t, store, series, "title")

	// 锁着的 title 与当前值相同：锁不是它没入队的原因。
	same := &metadata.SeriesMetadata{Provider: "bangumi", Title: series.Name, SourceID: 42}
	res, err := svc.Queue(ctx, series, same, "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Status != QueueNoChanges {
		t.Fatalf("Status = %q，期望 %q —— 「数据已是最新」被报成了「去解锁」", res.Status, QueueNoChanges)
	}

	// 锁着的 title 确有差异，而它是唯一有差异的字段。
	differing := &metadata.SeriesMetadata{Provider: "bangumi", Title: "A Different Title", SourceID: 42}
	res, err = svc.Queue(ctx, series, differing, "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Status != QueueAllFieldsLocked {
		t.Fatalf("Status = %q，期望 %q —— 用户看到「数据已是最新」，实际是该去解锁", res.Status, QueueAllFieldsLocked)
	}
}

// TestQueueReusesPendingProposalWithSameSignature：同一份数据再刮一次不该在收件箱里堆第二条。
func TestQueueReusesPendingProposalWithSameSignature(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	first, err := svc.Queue(ctx, series, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil || first.Status != QueueQueued {
		t.Fatalf("首次入队：status=%q err=%v", first.Status, err)
	}

	second, err := svc.Queue(ctx, series, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("二次入队: %v", err)
	}
	if second.Status != QueueReusedExisting {
		t.Fatalf("Status = %q，期望 %q", second.Status, QueueReusedExisting)
	}
	if second.Proposal.ID != first.Proposal.ID {
		t.Fatalf("复用的是提案 %d，首次建的是 %d", second.Proposal.ID, first.Proposal.ID)
	}
	if len(second.Fields) != len(first.Fields) {
		t.Fatalf("复用时返回了 %d 个字段，首次是 %d 个 —— 调用方拿不到这条提案的全貌",
			len(second.Fields), len(first.Fields))
	}

	pending, _ := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if len(pending) != 1 {
		t.Fatalf("待裁决队列里有 %d 条，期望 1 条", len(pending))
	}
}

// TestQueueKeepsDistinctSourcesSeparate：字段 diff 相同但来源条目不同的两次刮削各自入队。
//
// 按 diff 去重的话，用户选中第二条候选，界面上的来源超链接会指向第一条。
func TestQueueKeepsDistinctSourcesSeparate(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	// 同一作品的两个 Bangumi 条目：字段值一字不差，只有 SourceID/SourceURL 不同。
	first := &metadata.SeriesMetadata{
		Provider:  "bangumi",
		Title:     "Same Title",
		Summary:   "Same summary",
		SourceID:  111,
		SourceURL: "https://bgm.tv/subject/111",
	}
	other := &metadata.SeriesMetadata{
		Provider:  "bangumi",
		Title:     "Same Title",
		Summary:   "Same summary",
		SourceID:  333,
		SourceURL: "https://bgm.tv/subject/333",
	}

	firstRes, err := svc.Queue(ctx, series, first, "bangumi", "Alpha", QueueOptions{})
	if err != nil || firstRes.Status != QueueQueued {
		t.Fatalf("首个来源入队：status=%q err=%v", firstRes.Status, err)
	}
	otherRes, err := svc.Queue(ctx, series, other, "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("另一来源入队: %v", err)
	}
	if otherRes.Status != QueueQueued || otherRes.Proposal.ID == firstRes.Proposal.ID {
		t.Fatalf("另一来源得到 status=%q id=%d（首个 id=%d）—— 被按字段 diff 去重掉了",
			otherRes.Status, otherRes.Proposal.ID, firstRes.Proposal.ID)
	}
	if otherRes.Proposal.SourceUrl != "https://bgm.tv/subject/333" {
		t.Fatalf("提案的 source_url 是 %q —— 用户点开会跳到另一个条目", otherRes.Proposal.SourceUrl)
	}

	pending, _ := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if len(pending) != 2 {
		t.Fatalf("待裁决队列里有 %d 条，期望两个来源各一条", len(pending))
	}
}

// TestQueueDedupIgnoresLockedFieldsInSignature：入队后加锁不该制造重复的待裁决提案。
//
// 新一轮刮削产出的差异已经把当前锁定字段筛掉了，比对签名时若不同口径地筛，两边永远对不上，
// 每次刮削都会为同一个系列再堆一条几乎相同的提案。
func TestQueueDedupIgnoresLockedFieldsInSignature(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	first, err := svc.Queue(ctx, series, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil || first.Status != QueueQueued {
		t.Fatalf("首次入队：status=%q err=%v", first.Status, err)
	}

	relocked := lockFields(t, store, series, "title")
	second, err := svc.Queue(ctx, relocked, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("二次入队: %v", err)
	}
	if second.Status != QueueReusedExisting || second.Proposal.ID != first.Proposal.ID {
		t.Fatalf("加锁后再刮削得到 status=%q id=%d（首次 id=%d）—— 收件箱里会堆同一个系列的重复条目",
			second.Status, second.Proposal.ID, first.Proposal.ID)
	}
}

// TestQueueReportsRejectedBefore：拒绝过的提案不得被下一次刮削原样塞回来。
func TestQueueReportsRejectedBefore(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	first, err := svc.Queue(ctx, series, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("首次入队: %v", err)
	}
	rejectProposal(t, store, first.Proposal.ID)

	res, err := svc.Queue(ctx, series, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("二次入队: %v", err)
	}
	if res.Status != QueueRejectedBefore {
		t.Fatalf("Status = %q，期望 %q —— 拒绝过的提案又被塞回队列，用户得反复拒同一条",
			res.Status, QueueRejectedBefore)
	}

	pending, _ := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if len(pending) != 0 {
		t.Fatalf("待裁决队列里又出现了 %d 条 —— 「拒绝」这个动作没有任何持久效果", len(pending))
	}
}

// TestQueueIgnoreRejectedRequeues：误拒之后必须还有路把数据重新加回来，
// 否则「拒绝」就成了不可撤销的黑名单。
func TestQueueIgnoreRejectedRequeues(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	first, err := svc.Queue(ctx, series, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("首次入队: %v", err)
	}
	rejectProposal(t, store, first.Proposal.ID)

	res, err := svc.Queue(ctx, series, fullResult(), "bangumi", "Alpha", QueueOptions{IgnoreRejected: true})
	if err != nil {
		t.Fatalf("强制入队: %v", err)
	}
	if res.Status != QueueQueued {
		t.Fatalf("Status = %q，期望 %q —— 误拒之后就没有回头路了", res.Status, QueueQueued)
	}
	pending, _ := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if len(pending) != 1 {
		t.Fatalf("强制入队后待裁决队列有 %d 条，期望 1 条", len(pending))
	}
}

// TestQueueAcceptsDifferentContentAfterRejection 是反向护栏：
// 去重只该挡住**逐字相同**的提案，内容变了就必须照常入队。
func TestQueueAcceptsDifferentContentAfterRejection(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	first, err := svc.Queue(ctx, series, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("首次入队: %v", err)
	}
	rejectProposal(t, store, first.Proposal.ID)

	changed := fullResult()
	changed.Summary = "A completely different summary"
	res, err := svc.Queue(ctx, series, changed, "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("内容不同的入队: %v", err)
	}
	if res.Status != QueueQueued {
		t.Fatalf("Status = %q，期望 %q —— 去重挡过头，数据源更新后用户再也收不到提案",
			res.Status, QueueQueued)
	}
}

// TestQueueDedupIsScopedToSeries：一个系列的待裁决与拒绝记录都不该影响另一个系列。
func TestQueueDedupIsScopedToSeries(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	seriesA := seedSeries(t, store, "LibA", "Series Alpha")
	seriesB := seedSeries(t, store, "LibB", "Series Beta")

	first, err := svc.Queue(ctx, seriesA, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil {
		t.Fatalf("A 首次入队: %v", err)
	}
	// A 上另留一条待裁决（换 source_id 才是新提案），确保 B 的去重也不会撞上它。
	otherSource := fullResult()
	otherSource.SourceID = 77
	if _, err := svc.Queue(ctx, seriesA, otherSource, "bangumi", "Alpha", QueueOptions{}); err != nil {
		t.Fatalf("A 二次入队: %v", err)
	}
	rejectProposal(t, store, first.Proposal.ID)

	res, err := svc.Queue(ctx, seriesB, fullResult(), "bangumi", "Beta", QueueOptions{})
	if err != nil {
		t.Fatalf("B 入队: %v", err)
	}
	if res.Status != QueueQueued {
		t.Fatalf("B 的入队被 A 的记录挡下了：status=%q —— 去重必须按系列隔离", res.Status)
	}
}
