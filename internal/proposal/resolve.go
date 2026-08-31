// 本文件是提案的裁决通路：应用与拒绝。调用方交出提案 id 与模式，拿回一个已分类的结局；
// 「读提案 / 判待裁决 / 读字段行 / 按模式筛 / 读系列 / 按当前锁定集过滤 / 写入 / 推终态」
// 这一串只在这里写一遍——单条入口与批量入口共用它，两处不可能再对同一种结局给出不同分类。

package proposal

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// ApplyMode 决定一条提案里哪些字段参与本次应用。
type ApplyMode string

const (
	// ApplyModeAll 应用提案里的全部字段。
	ApplyModeAll ApplyMode = "all"
	// ApplyModeFillEmpty 只补当前值为空的字段，不覆盖系列上已有的数据。
	ApplyModeFillEmpty ApplyMode = "fill_empty"
)

// ApplyStatus 是一次应用的结局分类。
type ApplyStatus string

const (
	// ApplyApplied 表示提案里的字段全部处理完毕，提案已关单。
	ApplyApplied ApplyStatus = "applied"
	// ApplyPartial 表示只写入了一部分，提案仍待裁决。剩下的字段还留在里面等下一次处理——
	// 与 ApplyApplied 分开是因为它还挂在收件箱里，报成「已应用」会让用户以为已经处理完了。
	ApplyPartial ApplyStatus = "partial"
	// ApplyLockedSkipped 表示可应用的字段被系列的当前锁定集筛光了，一个字段也没写。
	// 提案保持待裁决：标成已应用会让被跳过的提案在只查待裁决的收件箱里永久消失。
	ApplyLockedSkipped ApplyStatus = "locked_skipped"
	// ApplyNoChanges 表示按本次模式筛完之后没有字段可写（如 fill_empty 遇上当前值都非空）。
	ApplyNoChanges ApplyStatus = "no_changes"
	// ApplyConflict 表示这条提案已经不是待裁决状态——被并发请求、另一个标签页或上一次操作抢先了。
	// 它不是故障：抢先者的结果才是准的。
	ApplyConflict ApplyStatus = "conflict"
	// ApplyNotFound 表示这条提案不存在（含随系列级联删除的情形）。
	ApplyNotFound ApplyStatus = "not_found"
)

// ApplyResult 是一次应用的已分类结果。
// SeriesID 只在提案被找到时有值；Applied/Remaining 只在真正写入过（applied / partial）时有值。
type ApplyResult struct {
	Status   ApplyStatus
	SeriesID int64
	// Applied 是本次真正写进系列的字段名。
	Applied []string
	// Remaining 是留在这条提案里、下次还能再处理的字段名。
	Remaining []string
}

// RejectStatus 是一次拒绝的结局分类。
type RejectStatus string

const (
	// RejectRejected 表示提案已被推进到已拒绝。
	RejectRejected RejectStatus = "rejected"
	// RejectConflict 表示这条提案已经不是待裁决状态，被抢先了。
	RejectConflict RejectStatus = "conflict"
	// RejectNotFound 表示这条提案不存在。
	RejectNotFound RejectStatus = "not_found"
)

// RejectResult 是一次拒绝的已分类结果。
type RejectResult struct {
	Status RejectStatus
}

// errPreempted 只在事务内部用来触发回滚，不出本包，也不供调用方 errors.Is——
// 被抢先这件事对外只有 ApplyConflict 一种表达。
var errPreempted = errors.New("proposal is no longer pending")

// Apply 把一条提案按 mode 写进它的系列。error 只表示故障，六种结局全部落在 ApplyResult.Status 上。
func (s *Service) Apply(ctx context.Context, proposalID int64, mode ApplyMode) (ApplyResult, error) {
	proposal, err := s.db.GetMetadataReview(ctx, proposalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ApplyResult{Status: ApplyNotFound}, nil
		}
		return ApplyResult{}, err
	}
	// 事务外的这道前置检查省下一次写锁，也给出快速的冲突结论；真正的防线是下面事务内的 CAS。
	// 两条发现路径必须给出同一个分类：对用户来说「已经被别人处理过了」就是一件事。
	if !isPending(proposal.Status) {
		return ApplyResult{Status: ApplyConflict, SeriesID: proposal.SeriesID}, nil
	}

	// 走批量查询传单个 id：逐条版不在 Queries 里，本包因此没有第二种取字段行的写法。
	all, err := s.db.ListMetadataReviewFieldsByReviews(ctx, []int64{proposalID})
	if err != nil {
		return ApplyResult{}, err
	}
	selected := fieldsForMode(all, mode)
	if len(selected) == 0 {
		return ApplyResult{Status: ApplyNoChanges, SeriesID: proposal.SeriesID}, nil
	}

	series, err := s.db.GetSeries(ctx, proposal.SeriesID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 系列被删了，提案随之级联删除，只是这次加载抢在了删除中间。
			// 对调用方来说与「提案查不到」是同一件事。
			return ApplyResult{Status: ApplyNotFound}, nil
		}
		return ApplyResult{}, err
	}
	// 入队之后用户又给字段加了锁：写入器内部本来就会跳过它们，但那是静默的——整条提案
	// 照样被关单，用户看不出哪些提案没被写进去。在这里先筛一遍，全被筛光时明确报
	// ApplyLockedSkipped，让调用方给出「去解锁」这条可行动的提示。
	locked := lockedFieldSet(series)
	applicable := make([]database.MetadataReviewField, 0, len(selected))
	for _, field := range selected {
		if locked[field.FieldName] {
			continue
		}
		applicable = append(applicable, field)
	}
	if len(applicable) == 0 {
		return ApplyResult{Status: ApplyLockedSkipped, SeriesID: proposal.SeriesID}, nil
	}

	out := ApplyResult{SeriesID: proposal.SeriesID}
	appliedNames := make(map[string]bool, len(applicable))
	for _, field := range applicable {
		appliedNames[field.FieldName] = true
		out.Applied = append(out.Applied, field.FieldName)
	}
	for _, field := range all {
		if !appliedNames[field.FieldName] {
			out.Remaining = append(out.Remaining, field.FieldName)
		}
	}
	partial := len(out.Remaining) > 0

	err = s.db.ExecTx(ctx, func(q Queries) error {
		if err := applyMetadata(ctx, q, series, metadataFromFields(proposal, applicable), applyOptions{
			ProviderName: proposal.Provider,
			SourceURL:    proposal.SourceUrl,
			Confidence:   proposal.Confidence,
			ProposalID:   &proposal.ID,
			// 交出的是本次真正参与写入的字段名：写入器据此判断该不该整体替换 tags/authors，
			// 「本次没在写它」与「提案说它是空的」因此不会被混为一谈。
			WrittenFields: appliedNames,
		}); err != nil {
			return err
		}
		if partial {
			// 只应用了一部分：删掉已写入的字段行，让提案带着剩下的字段继续待裁决。
			// 删行而不是留着，是因为它们的 current_value 已经过时——留下会在收件箱里
			// 陈列一个「当前值 → 提案值」都相同的假 diff。剩余字段没被动过，快照仍然有效。
			// 提案保持待裁决也让下一轮刮削的去重签名能自然命中它。
			//
			// 这条分支没有终态 CAS：提案本来就要留在待裁决，没有状态可推进。代价是
			// 「加载之后被并发拒绝」在这里挡不住——元数据仍会写入，来源沿革会挂到一条
			// 已被拒绝的提案上。上面那道前置检查覆盖了绝大多数情形。
			for _, name := range out.Applied {
				if err := q.DeleteMetadataReviewField(ctx, database.DeleteMetadataReviewFieldParams{
					ReviewID:  proposal.ID,
					FieldName: name,
				}); err != nil {
					return err
				}
			}
			return nil
		}
		// 全部字段都处理完了才关单。同一事务内把提案从待裁决 CAS 到已应用：元数据写入与
		// 状态推进原子，避免元数据已写但状态仍待裁决而被重复应用。守卫必须落在事务内的
		// SQL 上——上面那次前置检查读到的是事务外的旧快照，随时可能过期。
		//
		// 影响行数为 0 即冲突：可能是并发抢先处理，也可能是提案已随系列级联删除。
		// CASE + ELSE 旧值的累加式写法让漏掉守卫的后果格外糟：一行同时带 applied_at 与
		// rejected_at，series_metadata_provenance 还指着它，审计链自相矛盾且无法复原。
		rows, err := q.ResolvePendingMetadataReview(ctx, database.ResolvePendingMetadataReviewParams{
			Status: "applied",
			ID:     proposal.ID,
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			return errPreempted
		}
		return nil
	})
	switch {
	case errors.Is(err, errPreempted):
		// 元数据写入已随事务整体撤销。
		return ApplyResult{Status: ApplyConflict, SeriesID: proposal.SeriesID}, nil
	case err != nil:
		return ApplyResult{}, err
	}

	if partial {
		out.Status = ApplyPartial
	} else {
		out.Status = ApplyApplied
	}
	return out, nil
}

// Reject 把一条提案推进到已拒绝。error 只表示故障。
func (s *Service) Reject(ctx context.Context, proposalID int64) (RejectResult, error) {
	proposal, err := s.db.GetMetadataReview(ctx, proposalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RejectResult{Status: RejectNotFound}, nil
		}
		return RejectResult{}, err
	}
	if !isPending(proposal.Status) {
		return RejectResult{Status: RejectConflict}, nil
	}

	// 与应用同一条 CAS：已是终态的行一律不被改写，原始的终态时间戳因此不会被顶掉。
	rows, err := s.db.ResolvePendingMetadataReview(ctx, database.ResolvePendingMetadataReviewParams{
		Status: "rejected",
		ID:     proposal.ID,
	})
	if err != nil {
		return RejectResult{}, err
	}
	if rows == 0 {
		return RejectResult{Status: RejectConflict}, nil
	}
	return RejectResult{Status: RejectRejected}, nil
}

func isPending(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "pending")
}

// fieldsForMode 按本次模式筛出参与写入的字段行。
func fieldsForMode(fields []database.MetadataReviewField, mode ApplyMode) []database.MetadataReviewField {
	if mode != ApplyModeFillEmpty {
		return fields
	}
	filtered := make([]database.MetadataReviewField, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field.CurrentValue) == "" {
			filtered = append(filtered, field)
		}
	}
	return filtered
}

// metadataFromFields 把要写入的字段行折回写入器认识的形状。
func metadataFromFields(proposal database.MetadataReview, fields []database.MetadataReviewField) *metadata.SeriesMetadata {
	result := &metadata.SeriesMetadata{
		Provider:   proposal.Provider,
		SourceID:   int(proposal.SourceID),
		SourceURL:  proposal.SourceUrl,
		Confidence: proposal.Confidence,
	}
	for _, field := range fields {
		switch field.FieldName {
		case "title":
			result.Title = field.ProposedValue
		case "summary":
			result.Summary = field.ProposedValue
		case "publisher":
			result.Publisher = field.ProposedValue
		case "status":
			result.Status = field.ProposedValue
		case "rating":
			if parsed, err := strconv.ParseFloat(field.ProposedValue, 64); err == nil {
				result.Rating = parsed
			}
		case "tags":
			if field.ProposedValue != "" {
				raw := strings.Split(field.ProposedValue, " / ")
				result.Tags = make([]string, 0, len(raw))
				for _, tag := range raw {
					tag = strings.TrimSpace(tag)
					if tag != "" {
						result.Tags = append(result.Tags, tag)
					}
				}
			}
		case "authors":
			result.Authors = parseAuthors(field.ProposedValue)
		}
	}
	return result
}

// parseAuthors 反解 joinProposedAuthors 写下的 "名字 (角色) / 名字" 文本。
func parseAuthors(value string) []metadata.SeriesAuthor {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	out := make([]metadata.SeriesAuthor, 0)
	for _, raw := range strings.Split(value, " / ") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		role := ""
		name := raw
		if idx := strings.LastIndex(raw, " ("); idx >= 0 && strings.HasSuffix(raw, ")") {
			name = strings.TrimSpace(raw[:idx])
			role = strings.TrimSpace(raw[idx+2 : len(raw)-1])
		}
		if name == "" {
			continue
		}
		out = append(out, metadata.SeriesAuthor{Name: name, Role: role})
	}
	return out
}
