// 业务说明：本文件是 Comic Vine Provider 的解析回归测试，验证 apiKey 前置校验、deck 优先/description 清洗回退、
// 封面 super/medium 优先级、出版商与发行年份提取、总数透传，以及 stripComicVineHTML 纯逻辑。
package metadata

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestComicVineRequiresAPIKey(t *testing.T) {
	p := NewComicVineProvider("")
	_, _, err := p.SearchMetadata(context.Background(), "saga", 10, 0)
	if err == nil || !strings.Contains(err.Error(), "api key not configured") {
		t.Fatalf("expected api-key error, got %v", err)
	}
}

func TestComicVineSearchMetadataParsesFixture(t *testing.T) {
	body := `{
	  "error":"OK",
	  "number_of_total_results": 3,
	  "results":[
	    {"id":100,"name":"Saga","deck":"Space opera drama.","description":"<p>Long &amp; winding tale.</p>",
	     "image":{"super_url":"https://cv/super.jpg","medium_url":"https://cv/med.jpg"},
	     "count_of_issues":54,"start_year":"2012","site_detail_url":"https://comicvine.com/saga",
	     "publisher":{"name":"Image Comics"}}
	  ]
	}`
	var gotUA, gotKey string
	p := NewComicVineProvider("key-xyz")
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotUA = req.Header.Get("User-Agent")
		gotKey = req.URL.Query().Get("api_key")
		return metaJSONResponse(req, body), nil
	})}

	results, total, err := p.SearchMetadata(context.Background(), "saga", 20, 0)
	if err != nil {
		t.Fatalf("SearchMetadata failed: %v", err)
	}
	if gotUA == "" {
		t.Error("expected non-empty User-Agent (Comic Vine rejects empty)")
	}
	if gotKey != "key-xyz" {
		t.Errorf("api_key query = %q, want injected key", gotKey)
	}
	if total != 3 || len(results) != 1 {
		t.Fatalf("expected total=3 len=1, got total=%d len=%d", total, len(results))
	}
	m := results[0]
	if m.Title != "Saga" {
		t.Errorf("Title = %q", m.Title)
	}
	if m.Summary != "Space opera drama." {
		t.Errorf("Summary = %q, want deck preferred", m.Summary)
	}
	if m.Publisher != "Image Comics" {
		t.Errorf("Publisher = %q", m.Publisher)
	}
	if m.CoverURL != "https://cv/super.jpg" {
		t.Errorf("CoverURL = %q, want super_url", m.CoverURL)
	}
	if m.ReleaseDate != "2012" || m.VolumeCount != 54 || m.SourceID != 100 {
		t.Errorf("date/vols/id = %q/%d/%d", m.ReleaseDate, m.VolumeCount, m.SourceID)
	}
	if m.SourceURL != "https://comicvine.com/saga" || m.Provider != "Comic Vine" {
		t.Errorf("SourceURL/Provider = %q/%q", m.SourceURL, m.Provider)
	}
	assertMetaFloat(t, "Rating", m.Rating, 0)
	assertMetaFloat(t, "Confidence", m.Confidence, 0.9)
}

func TestComicVineDeckFallbackAndMediumCover(t *testing.T) {
	p := NewComicVineProvider("k")
	// deck 为空 → 用清洗后的 description；super_url 缺失 → medium_url。
	item := comicvineVolume{
		ID:          7,
		Name:        "X",
		Deck:        "   ",
		Description: "<p>Long &amp; winding tale.</p>",
		Image:       &comicvineImage{MediumURL: "https://cv/med.jpg"},
	}
	got := p.convertToSeriesMetadata(item, 0)
	if got.Summary != "Long & winding tale." {
		t.Errorf("Summary = %q, want cleaned description", got.Summary)
	}
	if got.CoverURL != "https://cv/med.jpg" {
		t.Errorf("CoverURL = %q, want medium fallback", got.CoverURL)
	}
}

func TestComicVineConfidenceFloorAndNoSummaryPenalty(t *testing.T) {
	p := NewComicVineProvider("k")
	// 无 deck 无 description → summary 空 → 0.9-0.05 = 0.85（rank0）。
	got := p.convertToSeriesMetadata(comicvineVolume{ID: 1, Name: "Y"}, 0)
	assertMetaFloat(t, "Confidence(no summary)", got.Confidence, 0.85)
	// 高排名 → 下限 0.4。
	got2 := p.convertToSeriesMetadata(comicvineVolume{ID: 2, Name: "Z", Deck: "has summary"}, 20)
	assertMetaFloat(t, "Confidence floor", got2.Confidence, 0.4)
}

func TestStripComicVineHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"<b>Hello</b> &amp;  world", "Hello & world"},
		{"<p>one</p><p>two</p>", "one two"},
		{"   spaced   text   ", "spaced text"},
	}
	for _, c := range cases {
		if got := stripComicVineHTML(c.in); got != c.want {
			t.Errorf("stripComicVineHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
