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
}

// applyMetadata 把 result 写进 series，跳过 series 上已锁定的字段。
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
		status := metadata.NormalizeStatusCode(result.Status)
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

	var tagValues []string
	if !locked["tags"] {
		for _, tagName := range result.Tags {
			tagName = strings.TrimSpace(tagName)
			if tagName == "" {
				continue
			}
			if inserted, err := q.UpsertTag(ctx, tagName); err == nil {
				_ = q.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: series.ID, TagID: inserted.ID})
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
	if !locked["authors"] && len(result.Authors) > 0 {
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
			if inserted, err := q.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: name, Role: role}); err == nil {
				_ = q.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: series.ID, AuthorID: inserted.ID})
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

		existingLinks, _ := q.GetLinksForSeries(ctx, series.ID)
		hasLink := false
		for _, l := range existingLinks {
			if l.Name == linkName {
				hasLink = true
				break
			}
		}
		if !hasLink {
			_, _ = q.LinkSeriesLink(ctx, database.LinkSeriesLinkParams{
				SeriesID: series.ID,
				Name:     linkName,
				Url:      linkURL,
			})
			if err := recordField("source_link", linkURL); err != nil {
				return err
			}
		}
	}

	return q.RefreshSeriesStats(ctx, series.ID)
}
