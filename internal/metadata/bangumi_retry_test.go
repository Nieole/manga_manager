// 业务说明：本文件覆盖元数据聚合链路的共享基础设施与 Bangumi 解析：指数退避 backoffDelay、Retry-After 解析、
// 可取消退避 sleepWithContext、infobox 出版商/作者抽取、Bangumi 标题优先与置信度，以及 locale/状态归一化。
package metadata

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestBackoffDelay(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 30 * time.Second},  // 32s 封顶到 30s
		{62, 30 * time.Second}, // 移位溢出 → 封顶
	}
	for _, c := range cases {
		if got := backoffDelay(c.attempt); got != c.want {
			t.Errorf("backoffDelay(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"5", 5 * time.Second},
		{"120", 120 * time.Second},
		{"-3", 0},  // 负秒 → 0
		{"abc", 0}, // 无法解析 → 0
		{"  7 ", 7 * time.Second},
	}
	for _, c := range cases {
		if got := parseRetryAfter(c.in); got != c.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	// HTTP-date（未来时刻）应解析为正的等待时长。
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 || got > 31*time.Second {
		t.Errorf("parseRetryAfter(future date) = %v, want (0,31s]", got)
	}
	// 过去时刻 → 0。
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("parseRetryAfter(past date) = %v, want 0", got)
	}
}

func TestSleepWithContext(t *testing.T) {
	// d<=0 → 直接返回 ctx.Err()（此处 nil）。
	if err := sleepWithContext(context.Background(), 0); err != nil {
		t.Errorf("sleepWithContext(d=0) = %v, want nil", err)
	}
	// 已取消的 ctx → 立即返回 Canceled。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleepWithContext(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepWithContext(cancelled) = %v, want Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("cancelled sleep took %v, expected near-immediate", elapsed)
	}
	// 正常小延时 → nil。
	if err := sleepWithContext(context.Background(), 5*time.Millisecond); err != nil {
		t.Errorf("sleepWithContext(5ms) = %v, want nil", err)
	}
}

func TestExtractPublisherFromInfobox(t *testing.T) {
	infobox := []interface{}{
		"not-a-map",
		map[string]interface{}{"key": "话数", "value": "1000"},
		map[string]interface{}{"key": "出版社", "value": "集英社"},
	}
	if got := extractPublisherFromInfobox(infobox); got != "集英社" {
		t.Errorf("publisher = %q, want 集英社", got)
	}
	// 英文 key。
	if got := extractPublisherFromInfobox([]interface{}{
		map[string]interface{}{"key": "publisher", "value": "Viz Media"},
	}); got != "Viz Media" {
		t.Errorf("publisher(en) = %q", got)
	}
	// 无匹配。
	if got := extractPublisherFromInfobox([]interface{}{
		map[string]interface{}{"key": "other", "value": "x"},
	}); got != "" {
		t.Errorf("publisher(none) = %q, want empty", got)
	}
}

func TestExtractAuthorsFromInfobox(t *testing.T) {
	infobox := []interface{}{
		// 字符串值按 、/, 分割。
		map[string]interface{}{"key": "作者", "value": "Oda、Toriyama, Kishimoto"},
		// 数组值：字符串 + {v:} + {name:}。
		map[string]interface{}{"key": "作画", "value": []interface{}{
			"Artist A",
			map[string]interface{}{"v": "Artist B"},
			map[string]interface{}{"name": "Artist C"},
		}},
		// 重复条目应去重。
		map[string]interface{}{"key": "原作", "value": "Oda"},
		// 未知 key 忽略。
		map[string]interface{}{"key": "话数", "value": "1000"},
	}
	authors := extractAuthorsFromInfobox(infobox)

	got := map[string]bool{}
	for _, a := range authors {
		got[a.Role+"|"+a.Name] = true
	}
	// 作者/原作 → Writer；作画 → Penciller。Oda 出现在 作者 与 原作 但同为 Writer → 去重。
	want := []string{
		"Writer|Oda", "Writer|Toriyama", "Writer|Kishimoto",
		"Penciller|Artist A", "Penciller|Artist B", "Penciller|Artist C",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing author %q in %+v", w, authors)
		}
	}
	if len(authors) != len(want) {
		t.Errorf("author count = %d, want %d (dedupe failed?) %+v", len(authors), len(want), authors)
	}
}

func TestBangumiConvertTitleAndConfidence(t *testing.T) {
	b := NewBangumiProvider()

	// 完整数据（rank0，含简介/出版社/标签）→ 0.92，无惩罚。
	full := bangumiSubjectResult{
		ID:      100,
		Name:    "One Piece",
		NameCN:  "海贼王",
		Summary: "草帽一伙的冒险",
		Date:    "1997-07-22",
		Volumes: 105,
		Tags:    []bangumiTag{{Name: "冒险"}},
		Infobox: []interface{}{map[string]interface{}{"key": "出版社", "value": "集英社"}},
	}
	m := b.convertToSeriesMetadata(full, 0)
	if m.Title != "海贼王" {
		t.Errorf("Title = %q, want NameCN preferred", m.Title)
	}
	if m.OriginalTitle != "One Piece" {
		t.Errorf("OriginalTitle = %q, want Name", m.OriginalTitle)
	}
	if m.Publisher != "集英社" {
		t.Errorf("Publisher = %q", m.Publisher)
	}
	assertMetaFloat(t, "Confidence(full)", m.Confidence, 0.92)

	// 缺简介/出版社/标签（rank0）→ 0.92 - 0.08 - 0.03 - 0.03 = 0.78。
	sparse := bangumiSubjectResult{ID: 2, Name: "Solo"}
	ms := b.convertToSeriesMetadata(sparse, 0)
	if ms.Title != "Solo" {
		t.Errorf("Title = %q, want Name when NameCN empty", ms.Title)
	}
	assertMetaFloat(t, "Confidence(sparse)", ms.Confidence, 0.78)
}

func TestNormalizeStatusCode(t *testing.T) {
	cases := map[string]string{
		"ongoing":     "ongoing",
		"publishing":  "ongoing",
		"Serializing": "ongoing", // 大小写无关
		"completed":   "completed",
		"finished":    "completed",
		"hiatus":      "hiatus",
		"paused":      "hiatus",
		"cancelled":   "cancelled",
		"canceled":    "cancelled",
		"dropped":     "cancelled",
		"连载中":         "ongoing",
		"已完结":         "completed",
		"休刊中":         "hiatus",
		"已放弃":         "cancelled",
		"":            "unknown",
		"gibberish":   "unknown",
	}
	for in, want := range cases {
		if got := NormalizeStatusCode(in); got != want {
			t.Errorf("NormalizeStatusCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeLocaleAndContext(t *testing.T) {
	localeCases := map[string]string{
		"en":    "en-US",
		"EN-US": "en-US",
		"en-gb": "en-US",
		"zh":    "zh-CN",
		"zh-CN": "zh-CN",
		"":      "zh-CN",
		"fr":    "zh-CN", // 非 en 前缀 → 默认 zh-CN
	}
	for in, want := range localeCases {
		if got := normalizeLocale(in); got != want {
			t.Errorf("normalizeLocale(%q) = %q, want %q", in, got, want)
		}
	}

	// LocaleFromContext：nil ctx → 默认 zh-CN。
	if got := LocaleFromContext(nil); got != "zh-CN" { //nolint:staticcheck // intentionally passing nil to verify the default-locale fallback
		t.Errorf("LocaleFromContext(nil) = %q, want zh-CN", got)
	}
	// 未注入 → zh-CN。
	if got := LocaleFromContext(context.Background()); got != "zh-CN" {
		t.Errorf("LocaleFromContext(empty) = %q, want zh-CN", got)
	}
	// 注入 en → en-US（经 normalize）。
	ctx := WithLocale(context.Background(), "en")
	if got := LocaleFromContext(ctx); got != "en-US" {
		t.Errorf("LocaleFromContext(en) = %q, want en-US", got)
	}
}
