// 业务说明：本文件是 AniList Provider 的解析与置信度回归测试，验证 GraphQL 响应到 SeriesMetadata 的字段映射、
// 标题多语言优先级、HTML 简介清洗、标签过滤、作者角色映射、状态/日期归一化与置信度计算。
package metadata

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func newAniListWithTransport(fn roundTripFunc) *AniListProvider {
	p := NewAniListProvider()
	p.httpClient = &http.Client{Transport: fn}
	return p
}

func TestAniListSearchMetadataParsesFixture(t *testing.T) {
	body := `{
	  "data": {
	    "Page": {
	      "pageInfo": {"total": 42},
	      "media": [
	        {
	          "id": 30013,
	          "title": {"romaji":"ONE PIECE","english":"One Piece","native":"ワンピース"},
	          "description":"A story about <i>pirates</i>.<br>Second line &amp; more.",
	          "coverImage":{"extraLarge":"https://img/xl.jpg","large":"https://img/l.jpg"},
	          "averageScore": 88,
	          "genres":["Action","Adventure"],
	          "tags":[{"name":"Pirates","rank":90},{"name":"Minor","rank":40}],
	          "staff":{"edges":[
	             {"role":"Story & Art","node":{"name":{"full":"Eiichiro Oda"}}},
	             {"role":"Original Creator","node":{"name":{"full":"Author X"}}}
	          ]},
	          "startDate":{"year":1997,"month":7,"day":22},
	          "volumes":105,
	          "status":"RELEASING",
	          "siteUrl":"https://anilist.co/manga/30013"
	        }
	      ]
	    }
	  }
	}`
	p := newAniListWithTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		return metaJSONResponse(req, body), nil
	})

	results, total, err := p.SearchMetadata(context.Background(), "one piece", 20, 0)
	if err != nil {
		t.Fatalf("SearchMetadata failed: %v", err)
	}
	if total != 42 || len(results) != 1 {
		t.Fatalf("expected total=42 len=1, got total=%d len=%d", total, len(results))
	}
	m := results[0]
	if m.Title != "One Piece" {
		t.Errorf("Title = %q, want English 'One Piece'", m.Title)
	}
	if m.OriginalTitle != "ワンピース" {
		t.Errorf("OriginalTitle = %q, want native", m.OriginalTitle)
	}
	if m.Summary != "A story about pirates.\nSecond line & more." {
		t.Errorf("Summary = %q (HTML not cleaned as expected)", m.Summary)
	}
	if m.CoverURL != "https://img/xl.jpg" {
		t.Errorf("CoverURL = %q, want extraLarge", m.CoverURL)
	}
	assertMetaFloat(t, "Rating", m.Rating, 8.8)
	// genres 全部 + tag rank>=60；rank40 的 Minor 应被过滤。
	wantTags := map[string]bool{"Action": true, "Adventure": true, "Pirates": true}
	if len(m.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %v", m.Tags)
	}
	for _, tg := range m.Tags {
		if !wantTags[tg] {
			t.Errorf("unexpected tag %q (Minor rank<60 should be excluded)", tg)
		}
	}
	// Story & Art → Writer + Penciller；Original Creator → Writer。
	if len(m.Authors) != 3 {
		t.Fatalf("expected 3 author rows, got %+v", m.Authors)
	}
	roles := map[string]string{}
	for _, a := range m.Authors {
		roles[a.Role+"|"+a.Name] = a.Role
	}
	if roles["Writer|Eiichiro Oda"] == "" || roles["Penciller|Eiichiro Oda"] == "" || roles["Writer|Author X"] == "" {
		t.Errorf("unexpected author role mapping: %+v", m.Authors)
	}
	if m.Status != "ongoing" {
		t.Errorf("Status = %q, want ongoing", m.Status)
	}
	if m.ReleaseDate != "1997-07-22" {
		t.Errorf("ReleaseDate = %q", m.ReleaseDate)
	}
	if m.VolumeCount != 105 {
		t.Errorf("VolumeCount = %d, want 105", m.VolumeCount)
	}
	if m.SourceID != 30013 || m.Provider != "AniList" {
		t.Errorf("SourceID/Provider = %d/%q", m.SourceID, m.Provider)
	}
	assertMetaFloat(t, "Confidence(rank0,summary)", m.Confidence, 0.9)
}

func TestAniListGraphQLErrorSurfaces(t *testing.T) {
	body := `{"data":{"Page":{"media":[]}},"errors":[{"message":"boom"}]}`
	p := newAniListWithTransport(func(req *http.Request) (*http.Response, error) {
		return metaJSONResponse(req, body), nil
	})
	_, _, err := p.SearchMetadata(context.Background(), "x", 5, 0)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected GraphQL error surfaced, got %v", err)
	}
}

func TestAniListEmptyMediaReturnsNil(t *testing.T) {
	body := `{"data":{"Page":{"pageInfo":{"total":0},"media":[]}}}`
	p := newAniListWithTransport(func(req *http.Request) (*http.Response, error) {
		return metaJSONResponse(req, body), nil
	})
	results, total, err := p.SearchMetadata(context.Background(), "x", 5, 0)
	if err != nil || results != nil || total != 0 {
		t.Fatalf("expected empty (nil,0,nil), got results=%v total=%d err=%v", results, total, err)
	}
}

func TestAniListErrorStatusReturnsError(t *testing.T) {
	p := NewAniListProvider()
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := metaJSONResponse(req, `{"message":"forbidden"}`)
		resp.StatusCode = http.StatusForbidden
		return resp, nil
	})}
	// 403 非可重试状态，应立即返回错误。
	_, _, err := p.SearchMetadata(context.Background(), "x", 5, 0)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

// ---- 纯逻辑：状态映射 ----

func TestAnilistMapStatus(t *testing.T) {
	cases := map[string]string{
		"RELEASING":  "ongoing",
		"releasing":  "ongoing",
		" FINISHED ": "completed",
		"HIATUS":     "hiatus",
		"CANCELLED":  "cancelled",
		"NOT_YET":    "",
		"":           "",
	}
	for in, want := range cases {
		if got := anilistMapStatus(in); got != want {
			t.Errorf("anilistMapStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnilistReleaseDate(t *testing.T) {
	cases := []struct {
		y, m, d int
		want    string
	}{
		{1997, 7, 22, "1997-07-22"},
		{2020, 12, 0, "2020-12"},
		{2020, 0, 0, "2020"},
		{2020, 0, 5, "2020"}, // 无月份则日被丢弃
		{0, 5, 5, ""},
	}
	for _, c := range cases {
		if got := anilistReleaseDate(c.y, c.m, c.d); got != c.want {
			t.Errorf("anilistReleaseDate(%d,%d,%d) = %q, want %q", c.y, c.m, c.d, got, c.want)
		}
	}
}

func TestAnilistStripHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"<b>bold</b> text", "bold text"},
		{"line1<br>line2", "line1\nline2"},
		{"line1<BR />line2", "line1\nline2"},
		{"tom &amp; jerry", "tom & jerry"},
		{"  padded  ", "padded"},
	}
	for _, c := range cases {
		if got := anilistStripHTML(c.in); got != c.want {
			t.Errorf("anilistStripHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAnilistExtractAuthorsDedupe(t *testing.T) {
	m := anilistMedia{}
	m.Staff.Edges = []struct {
		Role string `json:"role"`
		Node struct {
			Name struct {
				Full string `json:"full"`
			} `json:"name"`
		} `json:"node"`
	}{
		mkAnilistEdge("Story", "Same Name"),
		mkAnilistEdge("Story", "Same Name"), // 重复 Writer，应去重
		mkAnilistEdge("Art", "Artist Y"),
		mkAnilistEdge("Background Art", "  "), // 空名跳过
	}
	authors := anilistExtractAuthors(m)
	if len(authors) != 2 {
		t.Fatalf("expected 2 deduped authors, got %+v", authors)
	}
	got := map[string]bool{}
	for _, a := range authors {
		got[a.Role+"|"+a.Name] = true
	}
	if !got["Writer|Same Name"] || !got["Penciller|Artist Y"] {
		t.Errorf("unexpected authors: %+v", authors)
	}
}

func mkAnilistEdge(role, name string) struct {
	Role string `json:"role"`
	Node struct {
		Name struct {
			Full string `json:"full"`
		} `json:"name"`
	} `json:"node"`
} {
	var e struct {
		Role string `json:"role"`
		Node struct {
			Name struct {
				Full string `json:"full"`
			} `json:"name"`
		} `json:"node"`
	}
	e.Role = role
	e.Node.Name.Full = name
	return e
}

func TestAnilistTitleFallbackAndConfidenceFloor(t *testing.T) {
	p := NewAniListProvider()
	// english 缺失 → romaji；native 缺失 → 用 romaji 作原名。
	var m anilistMedia
	m.Title.Romaji = "Romaji Only"
	// 高排名 + 无简介 → 置信度被拉到下限 0.4。
	got := p.convertToSeriesMetadata(m, 20)
	if got.Title != "Romaji Only" {
		t.Errorf("Title fallback = %q, want romaji", got.Title)
	}
	if got.OriginalTitle != "Romaji Only" {
		t.Errorf("OriginalTitle fallback = %q, want romaji", got.OriginalTitle)
	}
	assertMetaFloat(t, "Confidence floor", got.Confidence, 0.4)
}

func TestAnilistNativeTitleFallback(t *testing.T) {
	p := NewAniListProvider()
	var m anilistMedia
	m.Title.Native = "ネイティブ" // 仅 native 存在
	got := p.convertToSeriesMetadata(m, 0)
	if got.Title != "ネイティブ" {
		t.Errorf("Title = %q, want native fallback", got.Title)
	}
}
