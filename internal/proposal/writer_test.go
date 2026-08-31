// 本文件守元数据写入器：**锁定字段**一律不被外部数据覆盖、也不留下来源沿革；
// 标签与作者按名去重后落库；Bangumi 外链只在该数据源下写、且重复写入不产生第二条。
//
// 锁失效就是用户手工改过的元数据被外部源悄悄抹掉——这是本包里唯一直接改写系列的地方。

package proposal

import (
	"context"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// writeMetadata 在一次事务里调用写入器，与生产上的调用形态一致（裁决与写入同事务）。
// opts 没给 WrittenFields 时按 result 里有值的集合字段补上——生产上交出的正是这一份。
func writeMetadata(t *testing.T, store database.Store, series database.Series, result *metadata.SeriesMetadata, opts applyOptions) {
	t.Helper()
	ctx := context.Background()
	if opts.WrittenFields == nil {
		opts.WrittenFields = map[string]bool{
			"tags":    len(result.Tags) > 0,
			"authors": len(result.Authors) > 0,
		}
	}
	db := testDB{Store: store}
	if err := db.ExecTx(ctx, func(q Queries) error {
		return applyMetadata(ctx, q, series, result, opts)
	}); err != nil {
		t.Fatalf("applyMetadata: %v", err)
	}
}

func provenanceByField(t *testing.T, store database.Store, seriesID int64) map[string]database.SeriesMetadataProvenance {
	t.Helper()
	rows, err := store.GetSeriesMetadataProvenance(context.Background(), seriesID)
	if err != nil {
		t.Fatalf("GetSeriesMetadataProvenance: %v", err)
	}
	byField := make(map[string]database.SeriesMetadataProvenance, len(rows))
	for _, row := range rows {
		byField[row.FieldName] = row
	}
	return byField
}

func bangumiOptions() applyOptions {
	return applyOptions{
		ProviderName: "bangumi",
		SourceURL:    "https://bgm.tv/subject/12345",
		Confidence:   0.91,
	}
}

// TestApplyMetadataHonorsLockedFields：锁住的字段既不被覆盖，也不留来源沿革。
//
// 沿革一并断言是因为它是审计链：写了沿革却没写值，用户会在界面上看到「这个值来自 Bangumi」，
// 而实际显示的是他自己填的内容。
func TestApplyMetadataHonorsLockedFields(t *testing.T) {
	_, store := newTestService(t)
	series := seedSeries(t, store, "Lib", "Series Alpha")
	series.Title = nullString("Locked Title")
	series.Summary = nullString("Old summary")
	series.Publisher = nullString("Old publisher")
	series.Rating = nullFloat(7.2)
	series.LockedFields = nullString("title,publisher,tags")
	series = updateSeries(t, store, series)

	writeMetadata(t, store, series, &metadata.SeriesMetadata{
		Title:     "New Title",
		Summary:   "New summary",
		Publisher: "New publisher",
		Rating:    8.8,
		Tags:      []string{"Action", "Mystery"},
		SourceID:  12345,
	}, bangumiOptions())

	updated, err := store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if updated.Title.String != "Locked Title" {
		t.Errorf("锁定的 title 被写成 %q —— 用户手工填的元数据被外部源抹掉了", updated.Title.String)
	}
	if updated.Publisher.String != "Old publisher" {
		t.Errorf("锁定的 publisher 被写成 %q", updated.Publisher.String)
	}
	// 反向护栏：未锁字段必须照常写入，否则「锁」被实现成了「什么都不写」。
	if updated.Summary.String != "New summary" {
		t.Errorf("未锁定的 summary 没有写入（got %q）", updated.Summary.String)
	}
	if updated.Rating.Float64 != 8.8 {
		t.Errorf("未锁定的 rating 没有写入（got %v）", updated.Rating.Float64)
	}

	tags, err := store.GetTagsForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetTagsForSeries: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("锁定的 tags 被写入了 %d 个标签", len(tags))
	}

	prov := provenanceByField(t, store, series.ID)
	for _, locked := range []string{"title", "publisher", "tags"} {
		if _, ok := prov[locked]; ok {
			t.Errorf("锁定字段 %q 留下了来源沿革 —— 界面会声称这个值来自外部源，实际却是用户自己填的", locked)
		}
	}
	if _, ok := prov["summary"]; !ok {
		t.Error("写入的 summary 没有留下来源沿革")
	}
}

// TestApplyMetadataRecordsProvenance：写进去的每个字段都要记下来源、外链与置信度。
func TestApplyMetadataRecordsProvenance(t *testing.T) {
	svc, store := newTestService(t)
	series := seedSeries(t, store, "Lib", "Series Alpha")

	// 沿革的 review_id 带外键，得挂在一条真实提案上。
	queued, err := svc.Queue(context.Background(), series, fullResult(), "bangumi", "Alpha", QueueOptions{})
	if err != nil || queued.Status != QueueQueued {
		t.Fatalf("入队：status=%q err=%v", queued.Status, err)
	}
	proposalID := queued.Proposal.ID

	opts := bangumiOptions()
	opts.ProposalID = &proposalID
	writeMetadata(t, store, series, &metadata.SeriesMetadata{
		Title:    "New Title",
		Summary:  "New summary",
		SourceID: 12345,
	}, opts)

	prov := provenanceByField(t, store, series.ID)
	title, ok := prov["title"]
	if !ok {
		t.Fatalf("title 没有沿革记录，实际有 %v", prov)
	}
	if title.Source != "bangumi" || title.SourceUrl != "https://bgm.tv/subject/12345" {
		t.Errorf("title 沿革的来源是 %q / %q，与写入时给的不符", title.Source, title.SourceUrl)
	}
	if title.Confidence != 0.91 {
		t.Errorf("title 沿革的置信度是 %v，期望 0.91", title.Confidence)
	}
	if !title.ReviewID.Valid || title.ReviewID.Int64 != proposalID {
		t.Errorf("title 沿革没有挂到提案 %d 上（got %+v）—— 审计链断在这里，"+
			"没法从系列的某个值追回是哪一条提案写的", proposalID, title.ReviewID)
	}
}

// TestApplyMetadataUpsertsTagsAndAuthorsWithDedup：同名标签/作者只落一条，空白项丢弃。
func TestApplyMetadataUpsertsTagsAndAuthorsWithDedup(t *testing.T) {
	_, store := newTestService(t)
	series := seedSeries(t, store, "Lib", "Series Alpha")

	writeMetadata(t, store, series, &metadata.SeriesMetadata{
		Tags: []string{"Action", "Mystery", "Action", " "},
		Authors: []metadata.SeriesAuthor{
			{Name: "Someone", Role: "story"},
			{Name: "Someone", Role: "story"},
			{Name: "Other", Role: "art"},
			{Name: "  ", Role: "art"},
		},
		SourceID: 12345,
	}, bangumiOptions())

	tags, err := store.GetTagsForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetTagsForSeries: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("系列上有 %d 个标签，期望 2 个（重复的 Action 与空白项不该各占一条）：%+v", len(tags), tags)
	}

	authors, err := store.GetAuthorsForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetAuthorsForSeries: %v", err)
	}
	if len(authors) != 2 {
		t.Errorf("系列上有 %d 位作者，期望 2 位：%+v", len(authors), authors)
	}

	// 关联表靠 INSERT OR IGNORE 自己就不会重复，所以还要看沿革的值——那串文本是写入器
	// 自己拼的，去重没做的话它会写成「Someone (story) / Someone (story) / ...」，
	// 用户在系列页上就会看到同一个人被列两遍。
	prov := provenanceByField(t, store, series.ID)
	if got := prov["authors"].Value; got != "Other (art) / Someone (story)" {
		t.Errorf("作者沿革记成 %q，期望去重后的 \"Other (art) / Someone (story)\"", got)
	}
}

// TestApplyMetadataWritesBangumiLinkOnce：外链只在 Bangumi 这个数据源下写，且不重复。
//
// 不去重的话，每刮削一次就多一条一模一样的「Bangumi」外链，系列页上越堆越长。
func TestApplyMetadataWritesBangumiLinkOnce(t *testing.T) {
	_, store := newTestService(t)
	ctx := context.Background()
	series := seedSeries(t, store, "Lib", "Series Alpha")

	result := &metadata.SeriesMetadata{Title: "New Title", SourceID: 12345}
	writeMetadata(t, store, series, result, bangumiOptions())

	links, err := store.GetLinksForSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetLinksForSeries: %v", err)
	}
	if len(links) != 1 || links[0].Name != "Bangumi" || links[0].Url != "https://bgm.tv/subject/12345" {
		t.Fatalf("外链 = %+v，期望一条指向 bgm.tv/subject/12345 的 Bangumi 链接", links)
	}

	// 同一份数据再写一次：外链不该翻倍。
	updated, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	writeMetadata(t, store, updated, result, bangumiOptions())
	links, err = store.GetLinksForSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetLinksForSeries: %v", err)
	}
	if len(links) != 1 {
		t.Errorf("重复写入后有 %d 条外链 —— 每刮削一次系列页上就多一条一样的链接", len(links))
	}
}

// TestApplyMetadataSkipsSourceLinkForOtherProviders：只有 Bangumi 能拼出 bgm.tv 外链。
//
// 显示名（如 "Ollama LLM"）里带着的条目号与 bgm.tv 毫无关系，写下去就是一条指向错误条目的链接。
func TestApplyMetadataSkipsSourceLinkForOtherProviders(t *testing.T) {
	_, store := newTestService(t)
	series := seedSeries(t, store, "Lib", "Series Alpha")

	writeMetadata(t, store, series, &metadata.SeriesMetadata{Title: "New Title", SourceID: 12345}, applyOptions{
		ProviderName: "Ollama LLM",
		Confidence:   0.6,
	})

	links, err := store.GetLinksForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetLinksForSeries: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("非 Bangumi 数据源写下了 %+v —— 这条链接指向的 bgm.tv 条目与本次刮削无关", links)
	}
}
