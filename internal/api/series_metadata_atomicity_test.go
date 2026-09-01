// 守「系列元数据的手工保存是一笔全有或全无的写」：先清空再重建的三段里任一条写入失败，
// 整笔事务都必须回滚且不返回 200——否则用户的标签/作者/链接被清空后再也没重建回来。

package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"manga-manager/internal/database"
)

// rejectLinkURL 让数据库拒收指定 url 的链接插入。事务体里「写入被 DB 拒了」只有一种形状，
// 触发器与约束冲突、磁盘写失败对调用方是同一件事：LinkSeriesLink 返回非 nil error。
func rejectLinkURL(t *testing.T, store database.Store, url string) {
	t.Helper()
	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatalf("expected a *database.SqlStore, got %T", store)
	}
	// 触发器体内不能用绑定变量，url 只能拼进 SQL 文本。
	stmt := fmt.Sprintf(`
		CREATE TRIGGER reject_link BEFORE INSERT ON series_links
		WHEN NEW.url = '%s'
		BEGIN SELECT RAISE(ABORT, 'link rejected'); END;`, strings.ReplaceAll(url, "'", "''"))
	if _, err := sqlStore.DB().Exec(stmt); err != nil {
		t.Fatalf("install rejecting trigger failed: %v", err)
	}
}

func TestUpdateSeriesInfoLinkRebuildRollsBack(t *testing.T) {
	t.Run("重建链接失败时整批回滚且不返回 200", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		// 用户已有的一份元数据。
		version := seriesMetadataVersionOf(t, controller, series.ID)
		code, _ := putSeriesInfo(t, controller, series.ID, `{
			"title":"原标题","locked_fields":"",
			"tags":["原标签"],
			"authors":[{"name":"原作者","role":"story"}],
			"links":[
				{"name":"Bangumi","url":"https://bgm.tv/subject/1"},
				{"name":"官网","url":"https://example.com"}
			],
			"expected_version":"`+version+`"
		}`)
		if code != http.StatusOK {
			t.Fatalf("expected the seeding save to succeed, got %d", code)
		}

		rejectLinkURL(t, store, "https://boom.example")

		// 用户再存一次，其中一条链接写不进去。
		version = seriesMetadataVersionOf(t, controller, series.ID)
		code, _ = putSeriesInfo(t, controller, series.ID, `{
			"title":"新标题","locked_fields":"",
			"tags":["新标签"],
			"authors":[{"name":"新作者","role":"story"}],
			"links":[
				{"name":"能写的","url":"https://ok.example"},
				{"name":"写不进去的","url":"https://boom.example"}
			],
			"expected_version":"`+version+`"
		}`)
		if code == http.StatusOK {
			t.Fatalf("expected the save to fail, got 200 with links silently dropped")
		}

		// 链接必须原样还在：清空成功而重建失败时提交，等于用户的链接被静默删了。
		links, err := store.GetLinksForSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetLinksForSeries failed: %v", err)
		}
		if len(links) != 2 || links[0].Url != "https://bgm.tv/subject/1" || links[1].Url != "https://example.com" {
			t.Fatalf("failed save did not roll back the links: %+v", links)
		}

		// 同一笔事务里的标量字段、标签、作者一样都不能落。
		saved, err := store.GetSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetSeries failed: %v", err)
		}
		if saved.Title.String != "原标题" {
			t.Fatalf("failed save leaked the title: %q", saved.Title.String)
		}
		tags, err := store.GetTagsForSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetTagsForSeries failed: %v", err)
		}
		if len(tags) != 1 || tags[0].Name != "原标签" {
			t.Fatalf("failed save leaked the tags: %+v", tags)
		}
	})

	t.Run("链接照常保存不受影响", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

		// 反向判据：把错误改成向上返回后，本来能过的保存必须照样过。series_links 上没有唯一约束，
		// 同名同址重复两条也是合法写入，不该开始报 500。空 name / 空 url 仍按老规矩静默跳过。
		version := seriesMetadataVersionOf(t, controller, series.ID)
		code, _ := putSeriesInfo(t, controller, series.ID, `{
			"title":"标题","locked_fields":"",
			"tags":["A","A","  ",""],
			"authors":[{"name":"作者","role":"story"},{"name":"作者","role":"story"},{"name":"","role":"x"}],
			"links":[
				{"name":"Bangumi","url":"https://bgm.tv/subject/1"},
				{"name":"Bangumi","url":"https://bgm.tv/subject/1"},
				{"name":"","url":"https://no-name.example"},
				{"name":"没有地址","url":""}
			],
			"expected_version":"`+version+`"
		}`)
		if code != http.StatusOK {
			t.Fatalf("expected a normal save with duplicate links to succeed, got %d", code)
		}

		links, err := store.GetLinksForSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetLinksForSeries failed: %v", err)
		}
		if len(links) != 2 {
			t.Fatalf("expected both duplicate links to be stored, got %+v", links)
		}
		tags, err := store.GetTagsForSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetTagsForSeries failed: %v", err)
		}
		if len(tags) != 1 || tags[0].Name != "A" {
			t.Fatalf("normal tag save regressed: %+v", tags)
		}
		authors, err := store.GetAuthorsForSeries(t.Context(), series.ID)
		if err != nil {
			t.Fatalf("GetAuthorsForSeries failed: %v", err)
		}
		if len(authors) != 1 || authors[0].Name != "作者" {
			t.Fatalf("normal author save regressed: %+v", authors)
		}
	})
}
