// 本文件把一次刮削结果与系列的当前值折成可入队的字段提案，并给出各值的规范文本形式。

package proposal

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// fieldDraft 是一条尚未落库的字段提案。
type fieldDraft struct {
	Name       string
	Current    string
	Proposed   string
	Confidence float64
}

// buildFieldDrafts 产出可入队的字段提案，并返回「因锁定而被跳过」的字段数。
// 后者用来把「全被锁住」与「本来就没差异」两种结果区分开。
func buildFieldDrafts(series database.Series, tags []database.Tag, authors []database.Author, result *metadata.SeriesMetadata, confidence float64) ([]fieldDraft, int) {
	locked := lockedFieldSet(series)

	drafts := []fieldDraft{
		{
			Name:     "title",
			Current:  seriesDisplayTitle(series),
			Proposed: strings.TrimSpace(result.Title),
		},
		{
			Name:     "summary",
			Current:  nullText(series.Summary),
			Proposed: strings.TrimSpace(result.Summary),
		},
		{
			Name:     "publisher",
			Current:  nullText(series.Publisher),
			Proposed: strings.TrimSpace(result.Publisher),
		},
		{
			Name:    "status",
			Current: nullText(series.Status),
			// 用 Optional 版：数据源没给状态时提案留空，由下游的「空提案不入队」逻辑丢弃。
			// 折成 "unknown" 会让不提供状态的数据源每次刮削都提议把已有的正确状态改成 unknown。
			Proposed: optionalStatus(result.Status),
		},
		{
			Name:     "rating",
			Current:  nullNumber(series.Rating),
			Proposed: formatNumber(result.Rating),
		},
		{
			Name:     "tags",
			Current:  joinTags(tags),
			Proposed: joinProposedTags(result.Tags),
		},
		{
			Name:     "authors",
			Current:  joinAuthors(authors),
			Proposed: joinProposedAuthors(result.Authors),
		},
	}

	changes := make([]fieldDraft, 0, len(drafts))
	lockedSkipped := 0
	for _, draft := range drafts {
		if draft.Proposed == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(draft.Current), strings.TrimSpace(draft.Proposed)) {
			continue
		}
		// 锁定字段不得入队：入队的话应用时会静默跳过它、却仍把整条提案标成已应用，
		// 用户点「应用」实际什么也没发生，界面上却显示成功。
		//
		// 这个判断必须排在上面两个 continue **之后**：放最前面的话，
		// 「提案为空」或「与当前值完全相同」的锁定字段也会被算进 lockedSkipped，
		// 于是一次「数据毫无差异、但恰好有个字段被锁」的刮削会报「差异字段均已被锁定」，
		// 正好把要区分的两种语义搞反。lockedSkipped 只统计「不锁就会入队」的字段。
		if locked[draft.Name] {
			lockedSkipped++
			continue
		}
		draft.Confidence = confidence
		changes = append(changes, draft)
	}
	return changes, lockedSkipped
}

func lockedFieldSet(series database.Series) map[string]bool {
	if !series.LockedFields.Valid {
		return map[string]bool{}
	}
	return parseLockedFields(series.LockedFields.String)
}

// parseLockedFields 解析 locked_fields 的逗号分隔表示。
// 单独抽出来是因为收件箱那条查询直接取 s.locked_fields 字符串，手上没有整行系列。
func parseLockedFields(raw string) map[string]bool {
	locked := make(map[string]bool)
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field != "" {
			locked[field] = true
		}
	}
	return locked
}

func seriesDisplayTitle(series database.Series) string {
	if series.Title.Valid && strings.TrimSpace(series.Title.String) != "" {
		return series.Title.String
	}
	return series.Name
}

func nullText(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return strings.TrimSpace(value.String)
}

func nullNumber(value sql.NullFloat64) string {
	if !value.Valid || value.Float64 <= 0 {
		return ""
	}
	return strconv.FormatFloat(value.Float64, 'f', 1, 64)
}

func formatNumber(value float64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', 1, 64)
}

// optionalStatus 返回可入队的状态提案；数据源未提供或无法识别时返回空串，
// 由「空提案不入队」的既有逻辑自然丢弃。
func optionalStatus(value string) string {
	code, ok := metadata.NormalizeStatusCodeOptional(value)
	if !ok {
		return ""
	}
	return code
}

func joinTags(tags []database.Tag) string {
	if len(tags) == 0 {
		return ""
	}
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		if strings.TrimSpace(tag.Name) == "" {
			continue
		}
		names = append(names, tag.Name)
	}
	sort.Strings(names)
	return strings.Join(names, " / ")
}

func joinProposedTags(tags []string) string {
	seen := make(map[string]struct{}, len(tags))
	cleaned := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, tag)
	}
	sort.Strings(cleaned)
	return strings.Join(cleaned, " / ")
}

func authorEntryString(name, role string) string {
	name = strings.TrimSpace(name)
	role = strings.TrimSpace(role)
	if role == "" {
		return name
	}
	return name + " (" + role + ")"
}

func joinAuthors(authors []database.Author) string {
	if len(authors) == 0 {
		return ""
	}
	parts := make([]string, 0, len(authors))
	for _, a := range authors {
		if strings.TrimSpace(a.Name) == "" {
			continue
		}
		parts = append(parts, authorEntryString(a.Name, a.Role))
	}
	sort.Strings(parts)
	return strings.Join(parts, " / ")
}

func joinProposedAuthors(authors []metadata.SeriesAuthor) string {
	if len(authors) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(authors))
	parts := make([]string, 0, len(authors))
	for _, a := range authors {
		entry := authorEntryString(a.Name, a.Role)
		if entry == "" {
			continue
		}
		key := strings.ToLower(entry)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, entry)
	}
	sort.Strings(parts)
	return strings.Join(parts, " / ")
}

// defaultConfidence 是各数据源的置信度默认值，用于刮削结果自己没给置信度时兜底。
// providerName 可能是 key（"bangumi"）也可能是 provider.Name() 的显示名
// （如 "Ollama LLM"、"OpenAI Compatible (v1/chat/completions)"），因此在精确匹配之后
// 还对显示名做一次包含匹配，确保各 LLM provider 一视同仁。
func defaultConfidence(providerName string) float64 {
	name := strings.ToLower(strings.TrimSpace(providerName))
	switch name {
	case "bangumi":
		return 0.9
	case "anilist", "myanimelist", "mal":
		return 0.85
	case "mangadex":
		return 0.8
	case "comicvine", "comic vine", "comic-vine":
		return 0.75
	case "openai", "ollama", "llm", "openai-legacy":
		return 0.6
	}
	switch {
	case strings.Contains(name, "bangumi"):
		return 0.9
	case strings.Contains(name, "llm"), strings.Contains(name, "openai"), strings.Contains(name, "ollama"):
		return 0.6
	default:
		return 0.5
	}
}

// resolveSourceURL 给出这条提案的来源链接：数据源自己给了就用它，
// 否则只有 Bangumi 能从条目 id 拼出可用的外链。
func resolveSourceURL(providerName string, result *metadata.SeriesMetadata) string {
	if strings.TrimSpace(result.SourceURL) != "" {
		return strings.TrimSpace(result.SourceURL)
	}
	if result.SourceID > 0 && strings.EqualFold(providerName, "bangumi") {
		return fmt.Sprintf("https://bgm.tv/subject/%d", result.SourceID)
	}
	return ""
}
