// 本文件守「只填空字段」的判据：空不空按系列**此刻**的值算，不按字段行上入队瞬间的快照。
//
// 照快照判，这个模式会把用户后来手工写进去的值、以及同一批里前一条提案刚写进去的值
// 覆盖掉——而它的全部意义就是不覆盖已有数据。tags 与 authors 是整体替换的集合，
// 系列上只要还有一个成员就不算空：写进去等于把这些成员删掉。

package proposal

import (
	"context"
	"fmt"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// summaryResult 是一份只提案 summary 的刮削结果：入队只落一个字段行，
// 「有没有覆盖已有值」因此可以直接读系列的简介判定。
func summaryResult(sourceID int, summary string) *metadata.SeriesMetadata {
	return &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Summary:    summary,
		SourceID:   sourceID,
		SourceURL:  fmt.Sprintf("https://bgm.tv/subject/%d", sourceID),
		Confidence: 0.8,
	}
}

// TestApplyFillEmptyJudgesEmptinessBySeriesNotSnapshot 覆盖两种让快照过时的路径：
// 用户在详情页手工填了该字段，以及同一系列上的前一条提案刚把它填上。
func TestApplyFillEmptyJudgesEmptinessBySeriesNotSnapshot(t *testing.T) {
	cases := []struct {
		name string
		// arrange 摆出「入队时该字段为空、轮到应用时系列上已经有值」的局面，
		// 返回按顺序应用的提案 id 与此刻该保住的简介。
		arrange func(t *testing.T, svc *Service, store database.Store, series database.Series) ([]int64, string)
	}{
		{
			name: "入队后用户手工写了简介",
			arrange: func(t *testing.T, svc *Service, store database.Store, series database.Series) ([]int64, string) {
				queued := seedProposal(t, svc, series, summaryResult(11, "刮削来的简介"))
				series.Summary = nullString("用户自己写的简介")
				updateSeries(t, store, series)
				return []int64{queued.ID}, "用户自己写的简介"
			},
		},
		{
			name: "同一系列两条提案，一次批量全选",
			arrange: func(t *testing.T, svc *Service, store database.Store, series database.Series) ([]int64, string) {
				first := seedProposal(t, svc, series, summaryResult(11, "Bangumi 的简介"))
				second := seedProposal(t, svc, series, summaryResult(22, "AniList 的简介"))
				return []int64{first.ID, second.ID}, "Bangumi 的简介"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newTestService(t)
			ctx := context.Background()
			series := seedSeries(t, store, "Lib", "Series Alpha")

			ids, want := tc.arrange(t, svc, store, series)
			// 与批量入口同一个形态：按选中顺序逐条应用，每条各自一次事务。
			var last ApplyResult
			for _, id := range ids {
				res, err := svc.Apply(ctx, id, ApplyModeFillEmpty)
				if err != nil {
					t.Fatalf("应用提案 %d: %v", id, err)
				}
				last = res
			}

			updated, err := store.GetSeries(ctx, series.ID)
			if err != nil {
				t.Fatalf("GetSeries: %v", err)
			}
			if updated.Summary.String != want {
				t.Errorf("简介 = %q，期望 %q —— 「只填空字段」覆盖了系列上已有的值",
					updated.Summary.String, want)
			}

			lastID := ids[len(ids)-1]
			if last.Status != ApplyNoChanges {
				t.Errorf("最后一条的结局 = %q，期望 %q —— 系列上已经有值，这条提案没有可写的字段",
					last.Status, ApplyNoChanges)
			}
			if status, _, _ := proposalStatus(t, store, lastID); status != "pending" {
				t.Errorf("一个字段都没写却把提案消费成 %q —— 它再也回不到收件箱", status)
			}
		})
	}
}

// TestApplyFillEmptyTreatsCollectionMembersAsFilled 钉住集合字段的「空」：
// 系列上还有成员就跳过，一个成员都没有才写。
func TestApplyFillEmptyTreatsCollectionMembersAsFilled(t *testing.T) {
	existingAuthor := metadata.SeriesAuthor{Name: "Kept Author", Role: "story"}
	cases := []struct {
		name string
		// seed 在入队之后给系列挂上成员，让行上的快照过时。
		seed        func(t *testing.T, store database.Store, seriesID int64)
		wantTags    string
		wantAuthors string
	}{
		{
			name: "入队后用户给系列加了标签与署名",
			seed: func(t *testing.T, store database.Store, seriesID int64) {
				seedTags(t, store, seriesID, "keep-a", "keep-b", "keep-c")
				seedAuthors(t, store, seriesID, existingAuthor)
			},
			wantTags:    "keep-a / keep-b / keep-c",
			wantAuthors: "Kept Author (story)",
		},
		{
			name: "系列上一个成员都没有",
			seed: func(t *testing.T, store database.Store, seriesID int64) {},
			// fullResult 的标签与作者。
			wantTags:    "action / drama",
			wantAuthors: "Someone (story)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newTestService(t)
			ctx := context.Background()
			series := seedSeries(t, store, "Lib", "Series Alpha")
			queued := seedProposal(t, svc, series, fullResult())
			tc.seed(t, store, series.ID)

			if _, err := svc.Apply(ctx, queued.ID, ApplyModeFillEmpty); err != nil {
				t.Fatalf("Apply: %v", err)
			}

			if got := seriesTagText(t, store, series.ID); got != tc.wantTags {
				t.Errorf("标签 = %q，期望 %q —— 整体替换把系列上已有的标签删掉了", got, tc.wantTags)
			}
			if got := seriesAuthorText(t, store, series.ID); got != tc.wantAuthors {
				t.Errorf("作者 = %q，期望 %q —— 整体替换把系列上已有的署名删掉了", got, tc.wantAuthors)
			}
		})
	}
}
