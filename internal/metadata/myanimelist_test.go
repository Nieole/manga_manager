// 业务说明：本文件是 MyAnimeList Provider 的解析回归测试，验证 Client-ID 鉴权前置校验、limit 钳制、
// 作者姓名拼接与角色映射、状态映射、近似总数（含下一页 +1）计算，以及封面优先级。
package metadata

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestMyAnimeListRequiresClientID(t *testing.T) {
	p := NewMyAnimeListProvider("") // 未配置 client id
	_, _, err := p.SearchMetadata(context.Background(), "naruto", 10, 0)
	if err == nil || !strings.Contains(err.Error(), "client id not configured") {
		t.Fatalf("expected client-id error, got %v", err)
	}
}

func TestMyAnimeListSearchMetadataParsesFixture(t *testing.T) {
	body := `{
	  "data":[
	    {"node":{
	      "id":11,
	      "title":"Naruto",
	      "main_picture":{"large":"https://mal/large.jpg","medium":"https://mal/med.jpg"},
	      "synopsis":"Ninja growth story.",
	      "mean":7.98,
	      "genres":[{"id":1,"name":"Action"},{"id":2,"name":"Adventure"}],
	      "authors":[{"node":{"id":1,"first_name":"Masashi","last_name":"Kishimoto"},"role":"Story & Art"}],
	      "num_volumes":72,
	      "status":"finished",
	      "start_date":"1999-09-21"
	    }}
	  ],
	  "paging":{"next":"https://api.myanimelist.net/v2/manga?offset=1"}
	}`
	var gotClientID, gotLimit string
	p := NewMyAnimeListProvider("client-123")
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotClientID = req.Header.Get("X-MAL-CLIENT-ID")
		gotLimit = req.URL.Query().Get("limit")
		return metaJSONResponse(req, body), nil
	})}

	results, total, err := p.SearchMetadata(context.Background(), "naruto", 30, 0)
	if err != nil {
		t.Fatalf("SearchMetadata failed: %v", err)
	}
	if gotClientID != "client-123" {
		t.Errorf("X-MAL-CLIENT-ID header = %q, want injected id", gotClientID)
	}
	if gotLimit != "30" {
		t.Errorf("limit query = %q, want 30", gotLimit)
	}
	// 无精确总数：offset(0)+len(1) = 1，有下一页 → +1 = 2。
	if total != 2 {
		t.Errorf("total = %d, want 2 (approx with next page)", total)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	m := results[0]
	if m.Title != "Naruto" || m.Summary != "Ninja growth story." {
		t.Errorf("title/summary = %q/%q", m.Title, m.Summary)
	}
	if m.CoverURL != "https://mal/large.jpg" {
		t.Errorf("CoverURL = %q, want large", m.CoverURL)
	}
	assertMetaFloat(t, "Rating", m.Rating, 7.98)
	if len(m.Tags) != 2 {
		t.Errorf("Tags = %v", m.Tags)
	}
	if len(m.Authors) != 1 || m.Authors[0].Name != "Masashi Kishimoto" || m.Authors[0].Role != "Writer" {
		t.Errorf("Authors = %+v (want single Writer 'Masashi Kishimoto')", m.Authors)
	}
	if m.Status != "completed" {
		t.Errorf("Status = %q, want completed", m.Status)
	}
	if m.ReleaseDate != "1999-09-21" || m.VolumeCount != 72 || m.SourceID != 11 {
		t.Errorf("date/vols/id = %q/%d/%d", m.ReleaseDate, m.VolumeCount, m.SourceID)
	}
	if m.SourceURL != "https://myanimelist.net/manga/11" || m.Provider != "MyAnimeList" {
		t.Errorf("SourceURL/Provider = %q/%q", m.SourceURL, m.Provider)
	}
}

func TestMyAnimeListLimitClamping(t *testing.T) {
	cases := []struct {
		req  int
		want string
	}{
		{0, "20"},    // <=0 → 默认 20
		{500, "100"}, // >100 → 钳制到 100
		{50, "50"},
	}
	for _, c := range cases {
		var gotLimit string
		p := NewMyAnimeListProvider("cid")
		p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotLimit = req.URL.Query().Get("limit")
			return metaJSONResponse(req, `{"data":[],"paging":{}}`), nil
		})}
		if _, _, err := p.SearchMetadata(context.Background(), "x", c.req, 0); err != nil {
			t.Fatalf("SearchMetadata(%d) failed: %v", c.req, err)
		}
		if gotLimit != c.want {
			t.Errorf("limit for req=%d = %q, want %q", c.req, gotLimit, c.want)
		}
	}
}

func TestMyAnimeListEmptyDataReturnsNil(t *testing.T) {
	p := NewMyAnimeListProvider("cid")
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return metaJSONResponse(req, `{"data":[],"paging":{}}`), nil
	})}
	results, total, err := p.SearchMetadata(context.Background(), "x", 10, 0)
	if err != nil || results != nil || total != 0 {
		t.Fatalf("expected empty, got results=%v total=%d err=%v", results, total, err)
	}
}

// ---- 纯逻辑：角色与状态映射 ----

func TestMapMALAuthorRole(t *testing.T) {
	cases := map[string]string{
		"Story":        "Writer",
		"Story & Art":  "Writer", // 命中 story 先返回 Writer
		"Author":       "Writer",
		"Original":     "Writer",
		"Art":          "Penciller",
		"Illustration": "Writer", // 未含 story/author/original/art → 默认 Writer
		"":             "Writer",
	}
	for in, want := range cases {
		if got := mapMALAuthorRole(in); got != want {
			t.Errorf("mapMALAuthorRole(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapMALStatus(t *testing.T) {
	cases := map[string]string{
		"currently_publishing": "ongoing",
		"finished":             "completed",
		"on_hiatus":            "hiatus",
		"discontinued":         "cancelled",
		"  FINISHED  ":         "completed",
		"weird":                "",
	}
	for in, want := range cases {
		if got := mapMALStatus(in); got != want {
			t.Errorf("mapMALStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMALCoverMediumFallback(t *testing.T) {
	p := NewMyAnimeListProvider("cid")
	node := malMangaNode{MainPicture: &malPicture{Medium: "https://mal/med.jpg"}}
	if got := p.convertToSeriesMetadata(node, 0); got.CoverURL != "https://mal/med.jpg" {
		t.Errorf("CoverURL = %q, want medium fallback", got.CoverURL)
	}
}
