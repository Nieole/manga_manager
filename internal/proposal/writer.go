// 本文件是元数据写入器：把一份已裁决的元数据写进系列，并留下来源沿革。
//
// 它没有自己的事务——调用方在哪一次事务里调它，写入就落在那一次事务里。「写元数据」与
// 「关单」因此天然是同一次事务的两段，不必再把事务句柄递给外人。

package proposal

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// applyOptions 是一次写入的来源信息，落进 series_metadata_provenance 的各列。
type applyOptions struct {
	ProviderName string
	SourceURL    string
	Confidence   float64
	// ProposalID 把这次写入挂到发起它的提案上，构成审计链。人工编辑那条路径没有提案，留空。
	ProposalID *int64
	// WrittenFields 是本次写入负责的字段名。tags 与 authors 是整体替换的集合，只有列在
	// 这里时才清空重建：把「本次没在写它」当成「提案说它是空的」，一次只补空的部分应用
	// 就会顺手清空系列的全部标签。
	WrittenFields map[string]bool
}

// applyMetadata 把 result 写进 series，跳过 series 上已锁定的字段。
//
// 标量字段按「非空即覆盖」写，tags 与 authors 则是整体替换：写完之后系列上的这两个集合
// 等于提案值，提案里没有的标签与署名错误的作者随之消失。只补不删的话，「去掉某个标签」
// 这类建议永远不生效，而系列的当前值也永远追不上提案值，同一条提案会被反复生成。
//
// 锁在这里再判一次是有意为之：调用方筛过的是**提案字段行**，而 result 可以来自任何地方，
// 写入器不该指望上游一定筛干净——被外部源悄悄抹掉用户手工改过的值是这一条防线的对象。
func applyMetadata(ctx context.Context, q Queries, series database.Series, result *metadata.SeriesMetadata, opts applyOptions) error {
	locked := lockedFieldSet(series)
	providerName := strings.TrimSpace(opts.ProviderName)
	confidence := opts.Confidence
	if confidence <= 0 {
		confidence = defaultConfidence(opts.ProviderName)
	}
	proposalID := sql.NullInt64{}
	if opts.ProposalID != nil {
		proposalID = sql.NullInt64{Int64: *opts.ProposalID, Valid: true}
	}

	updateParams := database.UpdateSeriesMetadataParams{ID: series.ID}
	appliedFields := make(map[string]string)

	if !locked["title"] && result.Title != "" {
		updateParams.Title = sql.NullString{String: result.Title, Valid: true}
		appliedFields["title"] = result.Title
	} else {
		updateParams.Title = series.Title
	}

	if !locked["summary"] && result.Summary != "" {
		updateParams.Summary = sql.NullString{String: result.Summary, Valid: true}
		appliedFields["summary"] = result.Summary
	} else {
		updateParams.Summary = series.Summary
	}

	if !locked["publisher"] && result.Publisher != "" {
		updateParams.Publisher = sql.NullString{String: result.Publisher, Valid: true}
		appliedFields["publisher"] = result.Publisher
	} else {
		updateParams.Publisher = series.Publisher
	}

	if !locked["rating"] && result.Rating > 0 {
		updateParams.Rating = sql.NullFloat64{Float64: result.Rating, Valid: true}
		appliedFields["rating"] = fmt.Sprintf("%.1f", result.Rating)
	} else {
		updateParams.Rating = series.Rating
	}

	if !locked["status"] && result.Status != "" {
		status := metadata.StatusCodeOrUnknown(result.Status)
		updateParams.Status = sql.NullString{String: status, Valid: true}
		appliedFields["status"] = status
	} else {
		updateParams.Status = series.Status
	}
	updateParams.Language = series.Language
	updateParams.LockedFields = series.LockedFields
	updateParams.NameInitial = database.SeriesInitialFromNullTitle(updateParams.Title, series.Name)

	if _, err := q.UpdateSeriesMetadata(ctx, updateParams); err != nil {
		return err
	}

	recordField := func(fieldName, value string) error {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		_, err := q.UpsertSeriesMetadataProvenance(ctx, database.UpsertSeriesMetadataProvenanceParams{
			SeriesID:   series.ID,
			FieldName:  fieldName,
			Value:      value,
			Source:     providerName,
			SourceUrl:  strings.TrimSpace(opts.SourceURL),
			Confidence: confidence,
			ReviewID:   proposalID,
		})
		return err
	}

	for _, fieldName := range []string{"title", "summary", "publisher", "status", "rating"} {
		if err := recordField(fieldName, appliedFields[fieldName]); err != nil {
			return err
		}
	}

	// 关联表的写入错误一律向上返回让 ExecTx 回滚：吞掉的话，标签没落库、沿革却记下了、
	// 整条提案照样被 CAS 成已应用，三处说法互相矛盾且事后无从分辨。
	var tagValues []string
	if opts.WrittenFields["tags"] && !locked["tags"] {
		if err := q.ClearSeriesTags(ctx, series.ID); err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(result.Tags))
		for _, tagName := range result.Tags {
			tagName = strings.TrimSpace(tagName)
			if tagName == "" {
				continue
			}
			// 按落库口径（tags.name 唯一）去重，沿革那串文本才与系列的真实内容逐字一致。
			if _, ok := seen[tagName]; ok {
				continue
			}
			seen[tagName] = struct{}{}
			inserted, err := q.UpsertTag(ctx, tagName)
			if err != nil {
				return err
			}
			if err := q.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: series.ID, TagID: inserted.ID}); err != nil {
				return err
			}
			tagValues = append(tagValues, tagName)
		}
	}
	if len(tagValues) > 0 {
		sort.Strings(tagValues)
		if err := recordField("tags", strings.Join(tagValues, " / ")); err != nil {
			return err
		}
	}

	var authorEntries []string
	if opts.WrittenFields["authors"] && !locked["authors"] {
		if err := q.ClearSeriesAuthors(ctx, series.ID); err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(result.Authors))
		for _, a := range result.Authors {
			name := strings.TrimSpace(a.Name)
			role := strings.TrimSpace(a.Role)
			if name == "" {
				continue
			}
			key := strings.ToLower(name + "|" + role)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			inserted, err := q.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: name, Role: role})
			if err != nil {
				return err
			}
			if err := q.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: series.ID, AuthorID: inserted.ID}); err != nil {
				return err
			}
			authorEntries = append(authorEntries, authorEntryString(name, role))
		}
	}
	if len(authorEntries) > 0 {
		sort.Strings(authorEntries)
		if err := recordField("authors", strings.Join(authorEntries, " / ")); err != nil {
			return err
		}
	}

	// 来源链接：仅 Bangumi 提供 bgm.tv 外链。providerName 可能是 key（"bangumi"）
	// 或显示名，统一用包含匹配，避免 LLM 显示名（如 "Ollama LLM"）被误判为可写外链。
	if result.SourceID > 0 && strings.Contains(strings.ToLower(providerName), "bangumi") {
		linkName := "Bangumi"
		linkURL := fmt.Sprintf("https://bgm.tv/subject/%d", result.SourceID)

		existingLinks, err := q.GetLinksForSeries(ctx, series.ID)
		if err != nil {
			return err
		}
		hasLink := false
		for _, l := range existingLinks {
			if l.Name == linkName {
				hasLink = true
				break
			}
		}
		if !hasLink {
			if _, err := q.LinkSeriesLink(ctx, database.LinkSeriesLinkParams{
				SeriesID: series.ID,
				Name:     linkName,
				Url:      linkURL,
			}); err != nil {
				return err
			}
			if err := recordField("source_link", linkURL); err != nil {
				return err
			}
		}
	}

	return q.RefreshSeriesStats(ctx, series.ID)
}
