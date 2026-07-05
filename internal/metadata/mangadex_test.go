// 业务说明：本文件是 MangaDex Provider 的解析回归测试，验证多语言标题/别名取值、封面 URL 拼接、
// 标签与作者去重、册数解析、置信度计算，以及 firstLocalizedTitle / altTitleByLang / firstAltTitle 纯逻辑。
package metadata

import (
	"context"
	"net/http"
	"testing"
)

func newMangaDexWithTransport(fn roundTripFunc) *MangaDexProvider {
	p := NewMangaDexProvider()
	p.httpClient = &http.Client{Transport: fn}
	return p
}

func TestMangaDexSearchMetadataParsesFixture(t *testing.T) {
	body := `{
	  "data":[
	    {
	      "id":"abc-uuid",
	      "attributes":{
	        "title":{"en":"Berserk"},
	        "altTitles":[{"ja":"ベルセルク"},{"ja-ro":"Beruseruku"}],
	        "description":{"en":"Dark fantasy epic."},
	        "status":"ongoing",
	        "year":1989,
	        "lastVolume":"41",
	        "tags":[{"attributes":{"name":{"en":"Horror"}}},{"attributes":{"name":{"en":"Action"}}}]
	      },
	      "relationships":[
	        {"type":"cover_art","attributes":{"fileName":"cover.jpg"}},
	        {"type":"author","attributes":{"name":"Kentaro Miura"}},
	        {"type":"artist","attributes":{"name":"Kentaro Miura"}}
	      ]
	    }
	  ],
	  "total": 7
	}`
	p := newMangaDexWithTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", req.Method)
		}
		return metaJSONResponse(req, body), nil
	})

	results, total, err := p.SearchMetadata(context.Background(), "berserk", 20, 0)
	if err != nil {
		t.Fatalf("SearchMetadata failed: %v", err)
	}
	if total != 7 || len(results) != 1 {
		t.Fatalf("expected total=7 len=1, got total=%d len=%d", total, len(results))
	}
	m := results[0]
	if m.Title != "Berserk" {
		t.Errorf("Title = %q, want en title", m.Title)
	}
	if m.OriginalTitle != "ベルセルク" {
		t.Errorf("OriginalTitle = %q, want ja altTitle", m.OriginalTitle)
	}
	if m.Summary != "Dark fantasy epic." {
		t.Errorf("Summary = %q", m.Summary)
	}
	if m.CoverURL != "https://uploads.mangadex.org/covers/abc-uuid/cover.jpg" {
		t.Errorf("CoverURL = %q", m.CoverURL)
	}
	if len(m.Tags) != 2 || m.Tags[0] != "Horror" || m.Tags[1] != "Action" {
		t.Errorf("Tags = %v", m.Tags)
	}
	// author→Writer 与 artist→Penciller 同名不同角色，均保留。
	if len(m.Authors) != 2 {
		t.Fatalf("expected 2 authors, got %+v", m.Authors)
	}
	roles := map[string]bool{}
	for _, a := range m.Authors {
		roles[a.Role+"|"+a.Name] = true
	}
	if !roles["Writer|Kentaro Miura"] || !roles["Penciller|Kentaro Miura"] {
		t.Errorf("unexpected authors: %+v", m.Authors)
	}
	if m.Status != "ongoing" || m.ReleaseDate != "1989" || m.VolumeCount != 41 {
		t.Errorf("status/date/volumes = %q/%q/%d", m.Status, m.ReleaseDate, m.VolumeCount)
	}
	if m.SourceID != 0 || m.SourceURL != "https://mangadex.org/title/abc-uuid" || m.Provider != "MangaDex" {
		t.Errorf("SourceID/URL/Provider = %d/%q/%q", m.SourceID, m.SourceURL, m.Provider)
	}
	assertMetaFloat(t, "Rating", m.Rating, 0)
	assertMetaFloat(t, "Confidence", m.Confidence, 0.9)
}

func TestMangaDexTitleFallbackToAltAndVolumeParse(t *testing.T) {
	p := NewMangaDexProvider()
	// title map 无 en 也无任何值 → 回退到别名首个非空。lastVolume 非数字 → 0。
	item := mangadexManga{
		ID: "u1",
		Attributes: mangadexAttributes{
			Title:      map[string]string{},
			AltTitles:  []map[string]string{{"en-us": "Fallback Title"}},
			LastVolume: "none",
		},
	}
	got := p.convertToSeriesMetadata(item, 0)
	if got.Title != "Fallback Title" {
		t.Errorf("Title fallback = %q", got.Title)
	}
	if got.VolumeCount != 0 {
		t.Errorf("VolumeCount = %d, want 0 for non-numeric lastVolume", got.VolumeCount)
	}
	// 无简介 → 置信度 0.9-0.05 = 0.85。
	assertMetaFloat(t, "Confidence(no summary)", got.Confidence, 0.85)
}

func TestMangaDexConfidenceFloor(t *testing.T) {
	p := NewMangaDexProvider()
	item := mangadexManga{ID: "u2", Attributes: mangadexAttributes{Title: map[string]string{"en": "X"}}}
	got := p.convertToSeriesMetadata(item, 20) // 0.9-1.0 → floored 0.4
	assertMetaFloat(t, "Confidence floor", got.Confidence, 0.4)
}

// ---- 纯逻辑：本地化标题取值 ----

func TestFirstLocalizedTitle(t *testing.T) {
	m := map[string]string{"ja": "  日本語  ", "en": "English"}
	if got := firstLocalizedTitle(m, "en"); got != "English" {
		t.Errorf("preferred = %q, want English", got)
	}
	// 首选缺失 → 返回任意非空（trim 后）。
	only := map[string]string{"fr": "  Bonjour "}
	if got := firstLocalizedTitle(only, "en"); got != "Bonjour" {
		t.Errorf("fallback = %q, want trimmed 'Bonjour'", got)
	}
	if got := firstLocalizedTitle(map[string]string{}, "en"); got != "" {
		t.Errorf("empty map = %q, want empty", got)
	}
	// 首选存在但为空白 → 跳过空白继续找非空。
	blank := map[string]string{"en": "   ", "de": "Hallo"}
	if got := firstLocalizedTitle(blank, "en"); got != "Hallo" {
		t.Errorf("blank preferred = %q, want 'Hallo'", got)
	}
}

func TestAltTitleByLang(t *testing.T) {
	alts := []map[string]string{{"ja-ro": "Romaji"}, {"ja": "  日本  "}}
	// ja 优先于 ja-ro，即使 ja 出现在后面的条目里。
	if got := altTitleByLang(alts, "ja", "ja-ro"); got != "日本" {
		t.Errorf("altTitleByLang ja-first = %q, want '日本'", got)
	}
	// 只有 ja-ro 命中。
	if got := altTitleByLang([]map[string]string{{"ja-ro": "R"}}, "ja", "ja-ro"); got != "R" {
		t.Errorf("altTitleByLang ja-ro = %q", got)
	}
	if got := altTitleByLang(alts, "ko"); got != "" {
		t.Errorf("altTitleByLang miss = %q, want empty", got)
	}
}

func TestFirstAltTitle(t *testing.T) {
	alts := []map[string]string{{"x": "   "}, {"y": "  Real  "}}
	if got := firstAltTitle(alts); got != "Real" {
		t.Errorf("firstAltTitle = %q, want trimmed 'Real'", got)
	}
	if got := firstAltTitle(nil); got != "" {
		t.Errorf("firstAltTitle(nil) = %q, want empty", got)
	}
}
