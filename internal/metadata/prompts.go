package metadata

import (
	"fmt"
	"strings"
)

// ============================================================================
// 共享的 LLM 响应 JSON 解析类型
// ============================================================================

// llmMetadataResult LLM 返回的 JSON 结构化元数据
type llmMetadataResult struct {
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Publisher string   `json:"publisher"`
	Status    string   `json:"status"`
	Tags      []string `json:"tags"`
	Rating    float64  `json:"rating"`
}

// aiRecommendationResult LLM返回的推荐列表格式
type aiRecommendationResult struct {
	Recommendations []AIRecommendation `json:"recommendations"`
}

// AIGroupingResult LLM返回的分组列表格式
type AIGroupingResult struct {
	Collections []AIGroupCollection `json:"collections"`
}

// ============================================================================
// 统一提示词管理
// 所有 LLM Provider (Ollama, OpenAI 等) 共享相同的提示词模板，
// 仅在 HTTP 传输层有所不同。
// ============================================================================

// BuildFetchMetadataPrompt 构建元数据抓取的提示词
func BuildFetchMetadataPrompt(title string) string {
	return fmt.Sprintf(`你是一个漫画和书籍数据专家。请根据以下漫画/书籍系列的名称，提供该系列的详细元数据信息。
如果你不确定或不了解这个作品，请在所有字段中返回空值。

系列名称: %s

请严格以 JSON 格式回复，不要包含任何其他文字。JSON 结构如下:
{
  "title": "作品正式中文名（如有），否则原名",
  "summary": "作品简介（100-300字）",
  "publisher": "出版社名称",
  "status": "连载状态（连载中/已完结/未知）",
  "tags": ["标签1", "标签2", "标签3"],
  "rating": 0.0
}`, title)
}

// BuildRecommendationsPrompt 构建推荐提示词
func BuildRecommendationsPrompt(userTags []string, candidates []CandidateSeries, limit int) string {
	tagsStr := strings.Join(userTags, ", ")
	if tagsStr == "" {
		tagsStr = "无特定偏好(随机挑好书)"
	}

	candidatesText := buildCandidatesText(candidates)

	return fmt.Sprintf(`你是一个专业的漫画和书籍推荐助手。
请根据读者最近喜欢的阅读标签：[%s]，从下列候选作品中挑选出最符合他们口味的 %d 部作品。
每一部选出的作品，请仔细阅读它的简介，给出一句充满吸引力、富有情感的推荐语（50字左右，像向朋友安利一样）。

候选作品列表：
%s

请严格以 JSON 格式回复，不要包含任何其他文字。JSON 结构如下:
{
  "recommendations": [
    {
      "series_id": 123,
      "reason": "这部漫画巧妙地将科幻设定融入日常，绝对会让你大呼过瘾！"
    }
  ]
}`, tagsStr, limit, candidatesText)
}

// BuildGroupingPrompt 构建分组提示词
func BuildGroupingPrompt(seriesList []CandidateSeries) string {
	candidatesText := buildCandidatesText(seriesList)

	return fmt.Sprintf(`你是一个专业的图书管理员和漫画策展人。
请仔细阅读以下给定的所有漫画作品清单，分析它们的类型、背景设定或关联性，将它们逻辑分组成 3 到 5 个不同的主题合集(Collections)。

漫画作品清单：
%s

请严格以 JSON 格式回复，不要包含任何其他文字。必须输出以下格式:
{
  "collections": [
    {
      "name": "赛博朋克与科幻",
      "description": "探讨未来科技与人性的硬核科幻作品集",
      "series_ids": [12, 45, 89]
    }
  ]
}`, candidatesText)
}

// buildCandidatesText 构建候选作品列表文本（内部辅助函数）
// 包含标题回退和摘要截断逻辑
func buildCandidatesText(candidates []CandidateSeries) string {
	var builder strings.Builder
	for _, c := range candidates {
		title := c.Title
		if title == "" {
			title = fmt.Sprintf("未知标题 (ID: %d)", c.ID)
		}
		summary := c.Summary
		if len(summary) > 100 {
			summary = summary[:100] + "..." // 截断防止 token 爆炸
		}
		builder.WriteString(fmt.Sprintf("- ID: %d, 标题: %s, 简介: %s\n", c.ID, title, summary))
	}
	return builder.String()
}
