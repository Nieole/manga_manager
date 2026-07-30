// 业务说明：本文件守卫三个提示词构造器的语言分支与结构占位。
//
// 提示词是 LLM 那一侧唯一的输入。它错了不会报错——模型照样返回东西，只是质量下降或
// 格式不对，而失败最终表现为「刮削结果全是空的」「AI 分组一条都出不来」，
// 排查时几乎不会有人想到是提示词的语言分支写反了。
//
// 三个构造器的 en-US 分支此前全部零覆盖（中文分支被别处的用例间接带到了）。
//
// 断言刻意只钉两类东西：语言判别的锚句，以及结构占位（候选 ID、limit 数字、
// 无标题的占位文案）。不断言整段文案——那样每次润色措辞都要改测试，
// 用例会先被改成「注释掉」再被删掉。

package metadata

import (
	"context"
	"strings"
	"testing"
)

var localeCandidates = []CandidateSeries{
	{ID: 7, Title: "海贼王", Summary: "冒险"},
	{ID: 9, Title: "", Summary: ""},
}

func TestBuildFetchMetadataPromptSwitchesLocale(t *testing.T) {
	title := "One Piece"

	zh := BuildFetchMetadataPrompt(context.Background(), title)
	if !strings.Contains(zh, title) {
		t.Fatalf("中文提示词里没带上系列标题：\n%s", zh)
	}
	if strings.Contains(zh, "You are an expert") {
		t.Fatal("默认语言下走了英文分支")
	}

	en := BuildFetchMetadataPrompt(WithLocale(context.Background(), "en-US"), title)
	if !strings.Contains(en, "You are an expert") {
		t.Fatalf("en-US 下没走英文分支：\n%s", en)
	}
	if !strings.Contains(en, title) {
		t.Fatal("英文提示词里没带上系列标题")
	}
	// 两种语言都必须约束状态码取值：漏掉它，模型会自由发挥出 "airing"/"完结" 这类
	// 值域外的状态，落库后前端的状态徽章直接显示不出来。
	for name, prompt := range map[string]string{"zh": zh, "en": en} {
		for _, code := range []string{"ongoing", "completed", "hiatus", "cancelled", "unknown"} {
			if !strings.Contains(prompt, code) {
				t.Errorf("%s 提示词里缺少状态码 %q", name, code)
			}
		}
	}
}

func TestBuildRecommendationsPromptSwitchesLocale(t *testing.T) {
	const limit = 5

	zh := BuildRecommendationsPrompt(context.Background(), []string{"热血", "冒险"}, localeCandidates, limit)
	if !strings.Contains(zh, "热血") || !strings.Contains(zh, "冒险") {
		t.Fatalf("中文提示词里没带上用户标签：\n%s", zh)
	}

	en := BuildRecommendationsPrompt(WithLocale(context.Background(), "en-US"), []string{"action"}, localeCandidates, limit)
	if !strings.Contains(en, "action") {
		t.Fatalf("英文提示词里没带上用户标签：\n%s", en)
	}

	// 无偏好时两种语言各有自己的占位文案：拼错会让模型把它当成一个真实的标签去匹配。
	zhEmpty := BuildRecommendationsPrompt(context.Background(), nil, localeCandidates, limit)
	if !strings.Contains(zhEmpty, "无特定偏好") {
		t.Fatalf("中文的「无偏好」占位文案不对：\n%s", zhEmpty)
	}
	enEmpty := BuildRecommendationsPrompt(WithLocale(context.Background(), "en-US"), nil, localeCandidates, limit)
	if !strings.Contains(enEmpty, "no specific preference") {
		t.Fatalf("英文的「无偏好」占位文案不对：\n%s", enEmpty)
	}

	// limit 与候选 ID 必须出现在两种语言里：推荐结果是靠 ID 回填的，
	// 丢了 ID 整个功能就废了；丢了 limit 模型会返回任意条数。
	for name, prompt := range map[string]string{"zh": zh, "en": en} {
		if !strings.Contains(prompt, "5") {
			t.Errorf("%s 提示词里缺少 limit", name)
		}
		if !strings.Contains(prompt, "7") || !strings.Contains(prompt, "9") {
			t.Errorf("%s 提示词里缺少候选 ID", name)
		}
	}
}

func TestBuildGroupingPromptSwitchesLocale(t *testing.T) {
	zh := BuildGroupingPrompt(context.Background(), localeCandidates)
	if !strings.Contains(zh, "标题: 海贼王") {
		t.Fatalf("中文提示词的候选清单不对：\n%s", zh)
	}
	if strings.Contains(zh, "professional librarian") {
		t.Fatal("默认语言下走了英文分支")
	}

	en := BuildGroupingPrompt(WithLocale(context.Background(), "en-US"), localeCandidates)
	if !strings.Contains(en, "professional librarian") {
		t.Fatalf("en-US 下没走英文分支：\n%s", en)
	}
	// 候选清单也要跟着切语言：英文提示词里塞中文表头会让模型的输出语言飘忽不定。
	if !strings.Contains(en, "Title: 海贼王") {
		t.Fatalf("英文提示词的候选清单没切语言：\n%s", en)
	}
	if !strings.Contains(en, "Unknown title (ID: 9)") {
		t.Fatalf("英文提示词里无标题候选的占位文案不对：\n%s", en)
	}
	for name, prompt := range map[string]string{"zh": zh, "en": en} {
		if !strings.Contains(prompt, "ID: 7") {
			t.Errorf("%s 提示词里缺少候选 ID", name)
		}
	}
}
