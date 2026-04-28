package metadata

import (
	"context"
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
	Groups      []AIGroupCollection `json:"groups"`
}

func (r AIGroupingResult) NormalizedCollections() []AIGroupCollection {
	if len(r.Collections) > 0 {
		return r.Collections
	}
	return r.Groups
}

// ============================================================================
// 统一提示词管理
// 所有 LLM Provider (Ollama, OpenAI 等) 共享相同的提示词模板，
// 仅在 HTTP 传输层有所不同。
// ============================================================================

// BuildFetchMetadataPrompt 构建元数据抓取的提示词
func BuildFetchMetadataPrompt(ctx context.Context, title string) string {
	if LocaleFromContext(ctx) == "en-US" {
		return fmt.Sprintf(`You are an expert in manga and book metadata. Based on the series title below, provide detailed metadata for the work.
If you are unsure about the work, return empty values for every field.

Series title: %s

Return strict JSON only, with no extra text. Use these status codes only: ongoing, completed, hiatus, cancelled, unknown.
Write title, summary, publisher, and tags in English.

{
  "title": "Official English title if available, otherwise the original title",
  "summary": "Brief summary in English (80-220 words)",
  "publisher": "Publisher name",
  "status": "ongoing",
  "tags": ["tag 1", "tag 2", "tag 3"],
  "rating": 0.0
}`, title)
	}

	return fmt.Sprintf(`你是一个漫画和书籍数据专家。请根据以下漫画/书籍系列的名称，提供该系列的详细元数据信息。
如果你不确定或不了解这个作品，请在所有字段中返回空值。

系列名称: %s

请严格以 JSON 格式回复，不要包含任何其他文字。status 字段只能使用以下英文状态码之一：ongoing、completed、hiatus、cancelled、unknown。
title、summary、publisher、tags 请使用简体中文输出。

{
  "title": "作品正式中文名（如有），否则原名",
  "summary": "作品简介（100-300字）",
  "publisher": "出版社名称",
  "status": "ongoing",
  "tags": ["标签1", "标签2", "标签3"],
  "rating": 0.0
}`, title)
}

// BuildRecommendationsPrompt 构建推荐提示词
func BuildRecommendationsPrompt(ctx context.Context, userTags []string, candidates []CandidateSeries, limit int) string {
	tagsStr := strings.Join(userTags, ", ")
	if tagsStr == "" {
		if LocaleFromContext(ctx) == "en-US" {
			tagsStr = "no specific preference (pick strong titles)"
		} else {
			tagsStr = "无特定偏好(随机挑好书)"
		}
	}

	candidatesText := buildCandidatesText(ctx, candidates)

	if LocaleFromContext(ctx) == "en-US" {
		return fmt.Sprintf(`You are a professional manga and book recommendation assistant.
Based on the reader's recent preferred tags [%s], choose the best %d works from the candidate list below.
For each selected work, read its summary carefully and write one compelling recommendation line in English, around 25-45 words, as if recommending it to a friend.

Candidate works:
%s

Return strict JSON only, with no extra text:
{
  "recommendations": [
    {
      "series_id": 123,
      "reason": "A sharp near-future story with warm character chemistry that makes the sci-fi ideas feel personal and exciting."
    }
  ]
}`, tagsStr, limit, candidatesText)
	}

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
func BuildGroupingPrompt(ctx context.Context, seriesList []CandidateSeries) string {
	candidatesText := buildCandidatesText(ctx, seriesList)

	if LocaleFromContext(ctx) == "en-US" {
		return fmt.Sprintf(`You are a professional librarian and manga curator.
Read the full candidate list below and organize the works into 3 to 5 themed collections based on genre, setting, or narrative affinity.
Write collection names and descriptions in English.

Candidate works:
%s

Return strict JSON only in this format:
{
  "collections": [
    {
      "name": "Cyberpunk and Speculative Futures",
      "description": "A set of works exploring future technology, urban alienation, and identity.",
      "series_ids": [12, 45, 89]
    }
  ]
}`, candidatesText)
	}

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
func buildCandidatesText(ctx context.Context, candidates []CandidateSeries) string {
	var builder strings.Builder
	locale := LocaleFromContext(ctx)
	for _, c := range candidates {
		title := c.Title
		if title == "" {
			if locale == "en-US" {
				title = fmt.Sprintf("Unknown title (ID: %d)", c.ID)
			} else {
				title = fmt.Sprintf("未知标题 (ID: %d)", c.ID)
			}
		}
		summary := c.Summary
		if len(summary) > 100 {
			summary = summary[:100] + "..." // 截断防止 token 爆炸
		}
		if locale == "en-US" {
			builder.WriteString(fmt.Sprintf("- ID: %d, Title: %s, Summary: %s\n", c.ID, title, summary))
		} else {
			builder.WriteString(fmt.Sprintf("- ID: %d, 标题: %s, 简介: %s\n", c.ID, title, summary))
		}
	}
	return builder.String()
}
