package api

import (
	"context"
	"strings"
	"testing"
)

// TestAPITextLocalization 验证后端 HTTP 响应文案表 apiText 的 locale 选择与回退语义，
// 并强制 zh-CN 与 en-US 两张表 key 完全一致（防止新增文案时漏配一侧）。
func TestAPITextLocalization(t *testing.T) {
	if got := apiText("zh-CN", "auth.token_required"); got != "需要有效的访问令牌" {
		t.Fatalf("zh-CN auth.token_required = %q", got)
	}
	if got := apiText("en-US", "auth.token_required"); got != "A valid access token is required" {
		t.Fatalf("en-US auth.token_required = %q", got)
	}
	// 未知 locale 回退 zh-CN
	if got := apiText("fr-FR", "auth.token_required"); got != "需要有效的访问令牌" {
		t.Fatalf("unknown locale fallback = %q", got)
	}
	// 未知 key 回退 key 本身
	if got := apiText("en-US", "nope.no_such_key"); got != "nope.no_such_key" {
		t.Fatalf("unknown key = %q", got)
	}
	for k := range apiMessages["zh-CN"] {
		if _, ok := apiMessages["en-US"][k]; !ok {
			t.Errorf("en-US table missing key %q present in zh-CN", k)
		}
	}
	for k := range apiMessages["en-US"] {
		if _, ok := apiMessages["zh-CN"][k]; !ok {
			t.Errorf("zh-CN table missing key %q present in en-US", k)
		}
	}
}

// TestValidateLibraryRequestLocalized 验证资料库校验消息随 locale 本地化：
// 默认中文、en-US 输出英文且不残留中文标点。
func TestValidateLibraryRequestLocalized(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	req := CreateLibraryRequest{Name: "", Path: ""} // 触发多条校验

	zh := controller.validateLibraryRequest(context.Background(), "zh-CN", nil, req)
	if len(zh) == 0 {
		t.Fatal("expected validation issues for empty request")
	}
	if zh[0].Message != "名称不能为空。" {
		t.Fatalf("zh-CN first issue = %q, want 名称不能为空。", zh[0].Message)
	}

	en := controller.validateLibraryRequest(context.Background(), "en-US", nil, req)
	if len(en) != len(zh) {
		t.Fatalf("en-US issue count %d != zh-CN %d", len(en), len(zh))
	}
	if en[0].Message != "Name cannot be empty." {
		t.Fatalf("en-US first issue = %q, want Name cannot be empty.", en[0].Message)
	}
	for _, iss := range en {
		if strings.ContainsAny(iss.Message, "，。、：") {
			t.Errorf("en-US issue still contains Chinese punctuation: %q", iss.Message)
		}
	}
}
