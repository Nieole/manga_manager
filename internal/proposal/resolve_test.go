// 本文件守裁决的分类：一次应用到底写了多少、提案该不该关单、被抢先时是否整体回滚。
//
// 分错一种，用户就会为一次完全正常的操作看到「服务器错误」；关单关早了，被跳过的字段
// 会在只查待裁决的收件箱里永久消失，用户解锁后也找不回来；终态守卫漏了，会留下一行
// 同时带应用时间与拒绝时间的提案，而系列的来源沿革还指着它。

package proposal

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// preemptingDB 在开事务之前抢先把提案推进到终态，精确复现「事务外读到待裁决、真正写入时
// 已被别人处理掉」的那个窗口——并发的应用/拒绝、用户在两个标签页里各点一次都是这个形态。
// 这不是行为替身：底下仍是同一个真库，它只负责在正确的时刻插入一个真实的并发写。
type preemptingDB struct {
	Database
	once    sync.Once
	preempt func()
}

func (d *preemptingDB) ExecTx(ctx context.Context, fn func(Queries) error) error {
	d.once.Do(d.preempt)
	return d.Database.ExecTx(ctx, fn)
}

// partialResult 是一份「title 有当前值、summary/publisher 当前为空」的刮削结果：
// fill_empty 会写后两个、筛掉 title，正是部分应用的形态。
func partialResult() *metadata.SeriesMetadata {
	return &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Title:      "External Title",
		Summary:    "External summary",
		Publisher:  "External Publisher",
		SourceID:   7,
		SourceURL:  "https://bgm.tv/subject/7",
		Confidence: 0.8,
	}
}

// seedProposal 入队一条提案并返回它。
func seedProposal(t *testing.T, svc *Service, series database.Series, result *metadata.SeriesMetadata) database.MetadataReview {
	t.Helper()
	queued, err := svc.Queue(context.Background(), series, result, "bangumi", series.Name, QueueOptions{})
	if err != nil {
		t.Fatalf("入队: %v", err)
	}
	if queued.Status != QueueQueued {
		t.Fatalf("入队得到 status=%q，用例需要一条新建的提案", queued.Status)
	}
	return queued.Proposal
}

func proposalStatus(t *testing.T, store database.Store, id int64) (status string, appliedAt, rejectedAt sql.NullTime) {
	t.Helper()
	proposal, err := store.GetMetadataReview(context.Background(), id)
	if err != nil {
		t.Fatalf("GetMetadataReview: %v", err)
	}
	return proposal.Status, proposal.AppliedAt, proposal.RejectedAt
}

func remainingFieldNames(t *testing.T, store database.Store, proposalID int64) map[string]bool {
	t.Helper()
	fields, err := store.ListMetadataReviewFieldsByReviews(context.Background(), []int64{proposalID})
	if err != nil {
		t.Fatalf("ListMetadataReviewFieldsByReviews: %v", err)
	}
	names := make(map[string]bool, len(fields))
	for _, field := range fields {
		names[field.FieldName] = true
	}
	return names
}

// TestApplyWritesEveryFieldAndClosesProposal 是正向基线：没什么可筛的时候，全写、关单。
func TestApplyWritesEveryFieldAndClosesProposal(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	proposal := seedProposal(t, svc, series, fullResult())

	res, err := svc.Apply(ctx, proposal.ID, ApplyModeAll)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Status != ApplyApplied {
		t.Fatalf("Status = %q，期望 %q", res.Status, ApplyApplied)
	}
	if len(res.Remaining) != 0 {
		t.Errorf("Remaining = %v，全写完了不该还有剩", res.Remaining)
	}

	updated, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if updated.Title.String != "Scraped Title" || updated.Publisher.String != "Scraped Publisher" {
		t.Errorf("提案的值没有写进系列：title=%q publisher=%q", updated.Title.String, updated.Publisher.String)
	}

	status, appliedAt, rejectedAt := proposalStatus(t, store, proposal.ID)
	if status != "applied" {
		t.Errorf("提案全部处理完却没有关单（status=%q）—— 收件箱会一直挂着一条空提案", status)
	}
	if !appliedAt.Valid || rejectedAt.Valid {
		t.Errorf("终态时间戳 = applied:%v rejected:%v，期望只有应用时间", appliedAt.Valid, rejectedAt.Valid)
	}
}

// TestApplyFillEmptyKeepsUnappliedFields 是部分应用的核心判据：
// 提案必须保持待裁决，且只有已写入的那些字段行被删掉。
func TestApplyFillEmptyKeepsUnappliedFields(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	proposal := seedProposal(t, svc, series, partialResult())

	res, err := svc.Apply(ctx, proposal.ID, ApplyModeFillEmpty)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Status != ApplyPartial {
		t.Fatalf("Status = %q，期望 %q —— 报成已应用会让用户以为处理完了", res.Status, ApplyPartial)
	}

	status, _, _ := proposalStatus(t, store, proposal.ID)
	if status != "pending" {
		t.Fatalf("提案被关单成 %q —— 收件箱只查待裁决，被筛掉的 title 就此永久消失，"+
			"用户想应用它只能重新刮削一遍", status)
	}

	names := remainingFieldNames(t, store, proposal.ID)
	if !names["title"] {
		t.Error("被筛掉的 title 字段行不见了")
	}
	for _, applied := range []string{"summary", "publisher"} {
		if names[applied] {
			t.Errorf("已应用的 %q 仍留在字段行里 —— 它的 current_value 已经过时，"+
				"用户会看到一条自己和自己 diff 的假提案", applied)
		}
	}

	// 反向护栏：别为了保住提案就不写数据。
	updated, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if updated.Summary.String != "External summary" || updated.Publisher.String != "External Publisher" {
		t.Errorf("当前值为空的字段没有写入：summary=%q publisher=%q",
			updated.Summary.String, updated.Publisher.String)
	}
	if updated.Title.Valid {
		t.Errorf("fill_empty 写了当前值非空的 title：%q", updated.Title.String)
	}
}

// TestApplyClosesProposalOnceNothingRemains：剩下的字段还能再应用一次，之后提案才关单。
//
// 同时钉住幂等：第二次走 all 模式时，上一次已写入的字段行已被删除，不该再次生效。
func TestApplyClosesProposalOnceNothingRemains(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	proposal := seedProposal(t, svc, series, partialResult())

	if res, err := svc.Apply(ctx, proposal.ID, ApplyModeFillEmpty); err != nil || res.Status != ApplyPartial {
		t.Fatalf("首次应用：status=%q err=%v", res.Status, err)
	}

	// 两次应用之间手工改掉一个已写入的字段：第二次不该再碰它。
	// 用具体值而不是比 updated_at——SQLite 的 CURRENT_TIMESTAMP 只有秒精度，比时间戳测不出来。
	current, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	current.Publisher = nullString("Manual Publisher")
	updateSeries(t, store, current)

	res, err := svc.Apply(ctx, proposal.ID, ApplyModeAll)
	if err != nil {
		t.Fatalf("二次应用: %v", err)
	}
	if res.Status != ApplyApplied {
		t.Fatalf("Status = %q，期望 %q —— 剩余字段应用完就该关单", res.Status, ApplyApplied)
	}

	status, _, _ := proposalStatus(t, store, proposal.ID)
	if status != "applied" {
		t.Errorf("提案没有关单（status=%q）", status)
	}
	updated, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if updated.Title.String != "External Title" {
		t.Errorf("第二次没有写入剩下的 title：%q", updated.Title.String)
	}
	if updated.Publisher.String != "Manual Publisher" {
		t.Errorf("publisher 被二次写入覆盖成 %q —— 已应用的字段行本该已被删除，不该再次生效",
			updated.Publisher.String)
	}
}

// TestApplyFiltersFieldsLockedAfterQueueing：入队时没锁、之后才加的锁同样算数。
//
// 锁定字段被写进去就是用户手工改过的元数据被外部源抹掉；而只跳过写入却照样关单，
// 会让被跳过的提案在收件箱里永久消失。两条都得成立。
func TestApplyFiltersFieldsLockedAfterQueueing(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	proposal := seedProposal(t, svc, series, fullResult())

	before, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	lockFields(t, store, series, "title")

	res, err := svc.Apply(ctx, proposal.ID, ApplyModeAll)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Status != ApplyPartial {
		t.Fatalf("Status = %q，期望 %q —— 被锁的 title 还没处理，提案不能算全部应用",
			res.Status, ApplyPartial)
	}

	updated, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if updated.Title != before.Title {
		t.Errorf("锁定的 title 被写成 %q（原本 %q）", updated.Title.String, before.Title.String)
	}
	if updated.Publisher.String != "Scraped Publisher" || updated.Summary.String != "Scraped summary" {
		t.Errorf("未锁定的字段没有写入：publisher=%q summary=%q —— 过滤过头了",
			updated.Publisher.String, updated.Summary.String)
	}

	status, _, _ := proposalStatus(t, store, proposal.ID)
	if status != "pending" {
		t.Errorf("提案被关单成 %q —— 被锁字段的提案会随之消失，用户解锁后也找不回来", status)
	}
	if names := remainingFieldNames(t, store, proposal.ID); !names["title"] {
		t.Error("被锁的 title 字段行被删掉了")
	}
}

// TestApplyReportsAllFieldsLockedWhenEverythingIsFiltered：全被锁时要明确报出来，
// 不能一个字段没写却回一个「成功」。用户需要知道该去解锁。
func TestApplyReportsAllFieldsLockedWhenEverythingIsFiltered(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	proposal := seedProposal(t, svc, series, fullResult())

	before, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	lockFields(t, store, series, "title,summary,publisher,status,rating,tags,authors")

	res, err := svc.Apply(ctx, proposal.ID, ApplyModeAll)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Status != ApplyLockedSkipped {
		t.Fatalf("Status = %q，期望 %q —— 静默成功会让用户以为数据已经写进去了",
			res.Status, ApplyLockedSkipped)
	}

	updated, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if updated.Title != before.Title || updated.Publisher != before.Publisher {
		t.Error("一个字段都不该写，却写进去了")
	}
	if status, _, _ := proposalStatus(t, store, proposal.ID); status != "pending" {
		t.Errorf("零写入却把提案消费成 %q —— 被跳过的提案再也找不回来", status)
	}
}

// TestApplyReportsNoChangesWhenModeFiltersEverything：fill_empty 遇上当前值都非空时，
// 这是「无需处理」而不是故障。
func TestApplyReportsNoChangesWhenModeFiltersEverything(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	series.Summary = nullString("Existing summary")
	series.Publisher = nullString("Existing publisher")
	series = updateSeries(t, store, series)
	proposal := seedProposal(t, svc, series, partialResult())

	res, err := svc.Apply(ctx, proposal.ID, ApplyModeFillEmpty)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Status != ApplyNoChanges {
		t.Fatalf("Status = %q，期望 %q", res.Status, ApplyNoChanges)
	}
	if status, _, _ := proposalStatus(t, store, proposal.ID); status != "pending" {
		t.Errorf("什么都没写却把提案消费成 %q", status)
	}
}

// TestApplyReportsConflictWhenPreempted 把被抢先的两条发现路径钉成同一个结局。
//
// 「已经被别人处理过了」对用户就是一件事：分成两种分类，两个入口迟早会各挑一种去解释，
// 同一件事在单条与批量里显示成不同的结果。两条子用例都要求整体回滚。
func TestApplyReportsConflictWhenPreempted(t *testing.T) {
	cases := []struct {
		name string
		// apply 拿到一个已入队提案，负责在裁决前后制造抢先，并返回裁决结果。
		apply func(t *testing.T, svc *Service, store database.Store, reviewID int64) ApplyResult
	}{
		{
			name: "前置检查发现已非待裁决",
			apply: func(t *testing.T, svc *Service, store database.Store, reviewID int64) ApplyResult {
				rejectProposal(t, store, reviewID)
				res, err := svc.Apply(context.Background(), reviewID, ApplyModeAll)
				if err != nil {
					t.Fatalf("Apply: %v", err)
				}
				return res
			},
		},
		{
			name: "事务内 CAS 撞上",
			apply: func(t *testing.T, svc *Service, store database.Store, reviewID int64) ApplyResult {
				// 加载与写入之间被抢先：前置检查读到的仍是待裁决的旧快照，
				// 只有事务内那道 CAS 能挡住它。
				preempting := &preemptingDB{
					Database: testDB{Store: store},
					preempt:  func() { rejectProposal(t, store, reviewID) },
				}
				res, err := NewService(preempting).Apply(context.Background(), reviewID, ApplyModeAll)
				if err != nil {
					t.Fatalf("Apply: %v", err)
				}
				return res
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newTestService(t)
			ctx := context.Background()
			series := seedSeries(t, store, "Lib", "Series Alpha")
			proposal := seedProposal(t, svc, series, fullResult())

			before, err := store.GetSeries(ctx, series.ID)
			if err != nil {
				t.Fatalf("GetSeries: %v", err)
			}

			res := tc.apply(t, svc, store, proposal.ID)
			if res.Status != ApplyConflict {
				t.Fatalf("Status = %q，期望 %q", res.Status, ApplyConflict)
			}

			after, err := store.GetSeries(ctx, series.ID)
			if err != nil {
				t.Fatalf("GetSeries: %v", err)
			}
			if after.Title != before.Title || after.Publisher != before.Publisher || after.Summary != before.Summary {
				t.Errorf("冲突时元数据仍被写入了（title %q -> %q）—— 事务没有整体回滚",
					before.Title.String, after.Title.String)
			}
			prov, err := store.GetSeriesMetadataProvenance(ctx, series.ID)
			if err != nil {
				t.Fatalf("GetSeriesMetadataProvenance: %v", err)
			}
			if len(prov) != 0 {
				t.Errorf("留下了 %d 条来源沿革，且指向一条已被拒绝的提案 —— 审计链自相矛盾", len(prov))
			}

			status, appliedAt, rejectedAt := proposalStatus(t, store, proposal.ID)
			if status != "rejected" {
				t.Errorf("status = %q，期望 rejected —— 抢先者的结果被覆写了", status)
			}
			// 审计链的核心不变量：两个终态时间戳不能同时非空。
			if appliedAt.Valid && rejectedAt.Valid {
				t.Error("applied_at 与 rejected_at 同时非空 —— 这一行既「已应用」又「已拒绝」，" +
					"而系列的来源沿革还指着它")
			}
			if !rejectedAt.Valid {
				t.Error("rejected_at 丢了")
			}
		})
	}
}

func TestApplyReportsNotFound(t *testing.T) {
	svc, _ := newTestService(t)

	res, err := svc.Apply(context.Background(), 4242, ApplyModeAll)
	if err != nil {
		t.Fatalf("Apply 把「提案不存在」报成了故障：%v", err)
	}
	if res.Status != ApplyNotFound {
		t.Fatalf("Status = %q，期望 %q", res.Status, ApplyNotFound)
	}
}

func TestRejectMarksProposalRejected(t *testing.T) {
	svc, store := newTestService(t)
	series := seedSeries(t, store, "Lib", "Series Alpha")
	proposal := seedProposal(t, svc, series, fullResult())

	res, err := svc.Reject(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if res.Status != RejectRejected {
		t.Fatalf("Status = %q，期望 %q", res.Status, RejectRejected)
	}
	status, appliedAt, rejectedAt := proposalStatus(t, store, proposal.ID)
	if status != "rejected" || !rejectedAt.Valid || appliedAt.Valid {
		t.Errorf("拒绝后 status=%q applied=%v rejected=%v", status, appliedAt.Valid, rejectedAt.Valid)
	}
}

// TestRejectReportsConflictOnTerminalProposal：已进终态的提案不得被再次改写。
//
// 漏了这道守卫，一条已应用的提案会被改成已拒绝，而应用时间因 CASE 的 ELSE 分支被保留，
// 行上两个时间戳同时非空。
func TestRejectReportsConflictOnTerminalProposal(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	proposal := seedProposal(t, svc, series, fullResult())

	if res, err := svc.Apply(ctx, proposal.ID, ApplyModeAll); err != nil || res.Status != ApplyApplied {
		t.Fatalf("播种已应用：status=%q err=%v", res.Status, err)
	}

	res, err := svc.Reject(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if res.Status != RejectConflict {
		t.Fatalf("Status = %q，期望 %q", res.Status, RejectConflict)
	}
	status, appliedAt, rejectedAt := proposalStatus(t, store, proposal.ID)
	if status != "applied" {
		t.Errorf("终态被改写成了 %q", status)
	}
	if appliedAt.Valid && rejectedAt.Valid {
		t.Error("applied_at 与 rejected_at 同时非空 —— 审计链自相矛盾")
	}
}

// TestRejectDoesNotRefreshTerminalTimestamp：重复拒绝不该顶掉原始的拒绝时间。
//
// 单独成例是因为 SQLite 的 CURRENT_TIMESTAMP 只有秒精度：直接比对两次拒绝的时间戳
// 在缺陷代码下也会「相同」，是一条永不报红的死断言。这里先把它改成一个远古值。
func TestRejectDoesNotRefreshTerminalTimestamp(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	proposal := seedProposal(t, svc, series, fullResult())

	if res, err := svc.Reject(ctx, proposal.ID); err != nil || res.Status != RejectRejected {
		t.Fatalf("首次拒绝：status=%q err=%v", res.Status, err)
	}

	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatalf("需要 *SqlStore 才能直改行，得到 %T", store)
	}
	if _, err := sqlStore.DB().Exec(
		`UPDATE metadata_reviews SET rejected_at = ? WHERE id = ?`, "2000-01-01 00:00:00", proposal.ID); err != nil {
		t.Fatalf("改写 rejected_at: %v", err)
	}

	if res, err := svc.Reject(ctx, proposal.ID); err != nil || res.Status != RejectConflict {
		t.Fatalf("重复拒绝：status=%q err=%v，期望 %q", res.Status, err, RejectConflict)
	}

	var got string
	if err := sqlStore.DB().QueryRow(
		`SELECT rejected_at FROM metadata_reviews WHERE id = ?`, proposal.ID).Scan(&got); err != nil {
		t.Fatalf("读回 rejected_at: %v", err)
	}
	// 驱动读回来会规范化成 RFC3339（2000-01-01T00:00:00Z），比日期部分即可判别是否被顶掉。
	if len(got) < 10 || got[:10] != "2000-01-01" {
		t.Fatalf("rejected_at 被第二次拒绝顶成了 %q —— 原始拒绝时间丢失，审计链断了", got)
	}
}

func TestRejectReportsNotFound(t *testing.T) {
	svc, _ := newTestService(t)

	res, err := svc.Reject(context.Background(), 4242)
	if err != nil {
		t.Fatalf("Reject 把「提案不存在」报成了故障：%v", err)
	}
	if res.Status != RejectNotFound {
		t.Fatalf("Status = %q，期望 %q", res.Status, RejectNotFound)
	}
}
