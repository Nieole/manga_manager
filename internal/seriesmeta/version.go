// 系列元数据的版本：由内容算出的指纹，而不是库里存的一列。
// 它是手工保存做冲突检测的依据，读路径下发、写路径回验。

package seriesmeta

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

// Version fingerprints editable metadata, normalizing only collection order and whitespace.
// Identity, reading state and derived statistics do not participate. Any writing source
// changing editable content invalidates the version without maintaining a version column.
func Version(series database.Series, tags []database.Tag, authors []database.Author, links []database.SeriesLink) string {
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

// VersionQueries loads only the content participating in the metadata fingerprint.
type VersionQueries interface {
	GetSeries(context.Context, int64) (database.Series, error)
	GetTagsForSeries(context.Context, int64) ([]database.Tag, error)
	GetAuthorsForSeries(context.Context, int64) ([]database.Author, error)
	GetLinksForSeries(context.Context, int64) ([]database.SeriesLink, error)
}

// LoadVersion 在给定查询句柄上读齐四份内容再算指纹。传事务句柄即可让
// 「校验版本」与「写入」落在同一次事务里。
func LoadVersion(ctx context.Context, q VersionQueries, seriesID int64) (string, error) {
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
	return Version(series, tags, authors, links), nil
}
