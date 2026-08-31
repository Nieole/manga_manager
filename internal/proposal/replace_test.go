// 本文件守 tags/authors 的替换语义：接受一条提案之后，系列上这两个集合就等于提案值。
//
// 只补不删的话，刮削建议「去掉某个标签」永远不生效、沿革记下的却是提案值，而且下一轮
// 刮削会照原样再生成一条同样的提案，用户反复看到、反复应用同一条东西。

package proposal

import (
	"context"
	"errors"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// seedTags 给系列挂上标签，摆出「系列上已有的比刮削源多」这个前提。
func seedTags(t *testing.T, store database.Store, seriesID int64, names ...string) {
	t.Helper()
	ctx := context.Background()
	for _, name := range names {
		tag, err := store.UpsertTag(ctx, name)
		if err != nil {
			t.Fatalf("UpsertTag(%q): %v", name, err)
		}
		if err := store.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: seriesID, TagID: tag.ID}); err != nil {
			t.Fatalf("LinkSeriesTag(%q): %v", name, err)
		}
	}
}

// seedAuthors 给系列挂上作者。
func seedAuthors(t *testing.T, store database.Store, seriesID int64, authors ...metadata.SeriesAuthor) {
	t.Helper()
	ctx := context.Background()
	for _, a := range authors {
		author, err := store.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: a.Name, Role: a.Role})
		if err != nil {
			t.Fatalf("UpsertAuthor(%q): %v", a.Name, err)
		}
		if err := store.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: seriesID, AuthorID: author.ID}); err != nil {
			t.Fatalf("LinkSeriesAuthor(%q): %v", a.Name, err)
		}
	}
}

// seriesTagText / seriesAuthorText 用与入队完全相同的口径读出系列的真实内容，
// 因此可以直接与沿革的值、与提案值逐字比较。
func seriesTagText(t *testing.T, store database.Store, seriesID int64) string {
	t.Helper()
	tags, err := store.GetTagsForSeries(context.Background(), seriesID)
	if err != nil {
		t.Fatalf("GetTagsForSeries: %v", err)
	}
	return joinTags(tags)
}

func seriesAuthorText(t *testing.T, store database.Store, seriesID int64) string {
	t.Helper()
	authors, err := store.GetAuthorsForSeries(context.Background(), seriesID)
	if err != nil {
		t.Fatalf("GetAuthorsForSeries: %v", err)
	}
	return joinAuthors(authors)
}

// shrinkingResult 是一份「比系列现有内容少一个标签、并纠正一位署名」的刮削结果——
// Bangumi/AniList 与用户手上那份不一致时的常态。
func shrinkingResult() *metadata.SeriesMetadata {
	return &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Tags:       []string{"Action", "Drama"},
		Authors:    []metadata.SeriesAuthor{{Name: "Someone", Role: "story"}, {Name: "Third", Role: "art"}},
		SourceID:   42,
		SourceURL:  "https://bgm.tv/subject/42",
		Confidence: 0.9,
	}
}

// seedShrinkingSeries 摆出前提：系列上有三个标签两位作者，刮削源只给出两个标签、
// 且把画师换成了另一个人。
func seedShrinkingSeries(t *testing.T, store database.Store) database.Series {
	t.Helper()
	series := seedSeries(t, store, "Lib", "Series Alpha")
	seedTags(t, store, series.ID, "Action", "Drama", "Romance")
	seedAuthors(t, store, series.ID,
		metadata.SeriesAuthor{Name: "Someone", Role: "story"},
		metadata.SeriesAuthor{Name: "Wrong", Role: "art"},
	)
	return series
}

// TestApplyRemovesTagsAndAuthorsMissingFromProposal：应用一条提案之后，系列上的
// 标签与作者等于提案值——多出来的那些被删掉，署名错误的作者被换掉。
func TestApplyRemovesTagsAndAuthorsMissingFromProposal(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedShrinkingSeries(t, store)

	proposal := seedProposal(t, svc, series, shrinkingResult())
	result, err := svc.Apply(ctx, proposal.ID, ApplyModeAll)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Status != ApplyApplied {
		t.Fatalf("Apply status=%q，期望 applied", result.Status)
	}

	if got := seriesTagText(t, store, series.ID); got != "Action / Drama" {
		t.Errorf("系列标签 = %q，期望 %q —— 提案建议去掉的 Romance 还在，"+
			"用户点了应用界面报成功、标签一个没少", got, "Action / Drama")
	}
	if got := seriesAuthorText(t, store, series.ID); got != "Someone (story) / Third (art)" {
		t.Errorf("系列作者 = %q，期望 %q —— 署名错误的 Wrong 没有被纠正掉",
			got, "Someone (story) / Third (art)")
	}
}

// TestApplyRecordsProvenanceMatchingSeriesContent：沿革记的必须是系列的真实内容。
//
// 记提案值而系列是另一回事的话，审计链在说谎：界面会声称这串标签来自 Bangumi，
// 实际显示的却是并集。
func TestApplyRecordsProvenanceMatchingSeriesContent(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedShrinkingSeries(t, store)

	proposal := seedProposal(t, svc, series, shrinkingResult())
	if _, err := svc.Apply(ctx, proposal.ID, ApplyModeAll); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	prov := provenanceByField(t, store, series.ID)
	if got, want := prov["tags"].Value, seriesTagText(t, store, series.ID); got != want {
		t.Errorf("tags 沿革记成 %q，系列实际是 %q —— 审计链与系列内容对不上", got, want)
	}
	if got, want := prov["authors"].Value, seriesAuthorText(t, store, series.ID); got != want {
		t.Errorf("authors 沿革记成 %q，系列实际是 %q", got, want)
	}
}

// TestApplyConvergesSoRescrapeQueuesNothing：同一份数据第二次刮削不再生成提案。
//
// 这是本 bug 最烦人的后果：应用之后 current 仍是并集、proposed 仍是那份子集，
// 定时全库刮削于是把同一条提案一遍遍塞回收件箱。
func TestApplyConvergesSoRescrapeQueuesNothing(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedShrinkingSeries(t, store)

	proposal := seedProposal(t, svc, series, shrinkingResult())
	if _, err := svc.Apply(ctx, proposal.ID, ApplyModeAll); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	applied, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	again, err := svc.Queue(ctx, applied, shrinkingResult(), "bangumi", applied.Name, QueueOptions{})
	if err != nil {
		t.Fatalf("再次入队: %v", err)
	}
	if again.Status != QueueNoChanges {
		t.Errorf("同一份数据第二次刮削得到 status=%q，期望 no_changes —— "+
			"应用没让系列收敛到提案值，用户会反复看到、反复应用同一条提案", again.Status)
	}
}

// TestApplyKeepsLockedCollectionsIntact：锁住 tags 时整个字段跳过，既不写也不清空。
//
// 替换语义下漏掉这一条的后果比只补不删还糟：用户加锁保住的标签会被整批删掉。
func TestApplyKeepsLockedCollectionsIntact(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedShrinkingSeries(t, store)

	proposal := seedProposal(t, svc, series, shrinkingResult())
	locked := lockFields(t, store, series, "tags")
	if locked.LockedFields.String != "tags" {
		t.Fatalf("加锁没生效：locked_fields=%q", locked.LockedFields.String)
	}

	result, err := svc.Apply(ctx, proposal.ID, ApplyModeAll)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Status != ApplyPartial {
		t.Fatalf("Apply status=%q，期望 partial（tags 被锁、authors 仍要写）", result.Status)
	}

	if got := seriesTagText(t, store, series.ID); got != "Action / Drama / Romance" {
		t.Errorf("锁住的 tags 变成了 %q —— 用户加锁保住的标签被整批删掉了", got)
	}
	if _, ok := provenanceByField(t, store, series.ID)["tags"]; ok {
		t.Error("锁住的 tags 留下了来源沿革")
	}
	if got := seriesAuthorText(t, store, series.ID); got != "Someone (story) / Third (art)" {
		t.Errorf("未锁定的 authors 没有替换成提案值（got %q）", got)
	}
}

// TestApplyFillEmptyLeavesUntouchedCollectionsAlone：本次没在写的集合字段不受影响。
//
// fill_empty 只补当前值为空的字段，tags 因此不在这次写入里。把「本次没有 tags」
// 当成「提案说标签为空」，会让一次纯补空的应用顺手清空系列的全部标签。
func TestApplyFillEmptyLeavesUntouchedCollectionsAlone(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedShrinkingSeries(t, store)

	result := shrinkingResult()
	result.Summary = "External summary"
	proposal := seedProposal(t, svc, series, result)

	applied, err := svc.Apply(ctx, proposal.ID, ApplyModeFillEmpty)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if applied.Status != ApplyPartial {
		t.Fatalf("Apply status=%q，期望 partial（只补空的 summary）", applied.Status)
	}

	if got := seriesTagText(t, store, series.ID); got != "Action / Drama / Romance" {
		t.Errorf("本次没在写的 tags 变成了 %q —— 一次补空把系列的标签清空了", got)
	}
	if got := seriesAuthorText(t, store, series.ID); got != "Someone (story) / Wrong (art)" {
		t.Errorf("本次没在写的 authors 变成了 %q", got)
	}
}

// failingQueries 让某一次关联写入失败，复现「DB 抖动」。
type failingQueries struct {
	Queries
	failLinkTag bool
}

var errLinkFailed = errors.New("link failed")

func (q failingQueries) LinkSeriesTag(ctx context.Context, arg database.LinkSeriesTagParams) error {
	if q.failLinkTag {
		return errLinkFailed
	}
	return q.Queries.LinkSeriesTag(ctx, arg)
}

// failingDB 把事务体拿到的 Queries 换成会失败的那个，其余仍是同一个真库。
type failingDB struct {
	Database
	failLinkTag bool
}

func (d failingDB) ExecTx(ctx context.Context, fn func(Queries) error) error {
	return d.Database.ExecTx(ctx, func(q Queries) error {
		return fn(failingQueries{Queries: q, failLinkTag: d.failLinkTag})
	})
}

// TestApplyRollsBackWhenLinkWriteFails：关联写入失败必须整体回滚。
//
// 错误被吞掉时后果是三重的：标签没落库、沿革却记下了、提案照样被关单成已应用。
func TestApplyRollsBackWhenLinkWriteFails(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	series := seedShrinkingSeries(t, store)
	proposal := seedProposal(t, svc, series, shrinkingResult())

	failing := NewService(failingDB{Database: testDB{Store: store}, failLinkTag: true})
	if _, err := failing.Apply(ctx, proposal.ID, ApplyModeAll); err == nil {
		t.Fatal("关联写入失败时 Apply 返回了 nil —— 错误被吞掉了")
	}

	if got := seriesTagText(t, store, series.ID); got != "Action / Drama / Romance" {
		t.Errorf("回滚后系列标签 = %q，期望原样保留 —— 清空生效了、重建没生效", got)
	}
	if prov := provenanceByField(t, store, series.ID); len(prov) != 0 {
		t.Errorf("回滚后仍留下 %d 条沿革 —— 审计链记下了一次没有发生的写入", len(prov))
	}
	if status, _, _ := proposalStatus(t, store, proposal.ID); status != "pending" {
		t.Errorf("回滚后提案状态 = %q，期望仍待裁决", status)
	}
}
