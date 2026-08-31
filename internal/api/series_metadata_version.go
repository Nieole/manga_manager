// 系列元数据的版本：由内容算出的指纹，而不是库里存的一列。
// 它是手工保存做冲突检测的依据，读路径下发、写路径回验。

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"manga-manager/internal/database"
)

// 指纹串的分隔符：取 ASCII 的记录/单元/组分隔符，元数据文本里不会出现，因此拼接不可能歧义。
const (
	metadataFieldSep  = "\x1f"
	metadataRecordSep = "\x1e"
	metadataItemSep   = "\x1d"
)

// seriesMetadataVersion 是「元数据编辑器管辖的那些字段」此刻的指纹。
//
// 取内容指纹而不是自增版本列，是因为要防的是**内容**被盖掉，而改这些内容的写入方不止一处
// （手工保存、刮削应用提案、批量编辑、扫描增补），没有一个共同的写入口能保证「谁写了谁 +1」；
// 漏掉任何一处，那条路径下的覆盖就仍然是静默的。指纹由读路径算出，任何来源改了内容，
// 用户手里那份版本自然作废，写入方一行都不用改。
//
// 覆盖面刻意只到编辑器管辖的字段：is_favorite、阅读进度、卷数/页数/封面等派生统计一律不进——
// 它们进来了，用户读完一本书就存不下正在写的简介。
//
// 代价是它只说明「变了」，说不出变的是哪一项：指纹不可逆，服务端手里没有用户那一版的原值。
func seriesMetadataVersion(series database.Series, tags []database.Tag, authors []database.Author, links []database.SeriesLink) string {
	var b strings.Builder
	writeField := func(name, value string) {
		b.WriteString(name)
		b.WriteString(metadataFieldSep)
		b.WriteString(value)
		b.WriteString(metadataRecordSep)
	}

	writeField("title", series.Title.String)
	writeField("summary", series.Summary.String)
	writeField("publisher", series.Publisher.String)
	writeField("status", series.Status.String)
	writeField("language", series.Language.String)

	rating := ""
	if series.Rating.Valid {
		rating = strconv.FormatFloat(series.Rating.Float64, 'f', -1, 64)
	}
	writeField("rating", rating)

	// 锁、标签、作者、链接都按集合归一后再入指纹：它们的存储顺序由插入次序决定，
	// 只调换次序并没有任何人的内容被覆盖，不该把用户正在写的那一版判成过期。
	writeField("locked_fields", strings.Join(normalizedMetadataSet(strings.Split(series.LockedFields.String, ",")), metadataItemSep))

	tagNames := make([]string, 0, len(tags))
	for _, tag := range tags {
		tagNames = append(tagNames, tag.Name)
	}
	writeField("tags", strings.Join(normalizedMetadataSet(tagNames), metadataItemSep))

	authorEntries := make([]string, 0, len(authors))
	for _, author := range authors {
		authorEntries = append(authorEntries, author.Name+metadataFieldSep+author.Role)
	}
	writeField("authors", strings.Join(normalizedMetadataSet(authorEntries), metadataItemSep))

	linkEntries := make([]string, 0, len(links))
	for _, link := range links {
		linkEntries = append(linkEntries, link.Name+metadataFieldSep+link.Url)
	}
	writeField("links", strings.Join(normalizedMetadataSet(linkEntries), metadataItemSep))

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// normalizedMetadataSet 去空白、去空项、去重并排序，把一串值归一成与顺序无关的集合。
func normalizedMetadataSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// loadSeriesMetadataVersion 在给定查询句柄上读齐四份内容再算指纹。传事务句柄即可让
// 「校验版本」与「写入」落在同一次事务里。
func loadSeriesMetadataVersion(ctx context.Context, q database.Querier, seriesID int64) (string, error) {
	series, err := q.GetSeries(ctx, seriesID)
	if err != nil {
		return "", err
	}
	tags, err := q.GetTagsForSeries(ctx, seriesID)
	if err != nil {
		return "", err
	}
	authors, err := q.GetAuthorsForSeries(ctx, seriesID)
	if err != nil {
		return "", err
	}
	links, err := q.GetLinksForSeries(ctx, seriesID)
	if err != nil {
		return "", err
	}
	return seriesMetadataVersion(series, tags, authors, links), nil
}
