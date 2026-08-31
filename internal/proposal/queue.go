package proposal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// rejectedDedupWindow 是回溯多少条最近的拒绝记录参与去重。
//
// 刻意不做成「全部历史拒绝记录」：那会随时间无界增长，而用户真正会反复看到的
// 就是最近那些。取 20 已经远超实际使用中单个系列的拒绝次数。
const rejectedDedupWindow = 20

// QueueStatus 是一次入队的结局分类。
type QueueStatus string

const (
	// QueueQueued 表示新建了一条待裁决提案。
	QueueQueued QueueStatus = "queued"
	// QueueReusedExisting 表示队列里已有一条逐字相同的待裁决提案，本次复用了它。
	QueueReusedExisting QueueStatus = "reused_existing"
	// QueueNoChanges 表示刮削结果与系列的当前值没有差异。
	QueueNoChanges QueueStatus = "no_changes"
	// QueueAllFieldsLocked 表示确实有差异，但差异字段全部被用户锁定了。
	// 与 QueueNoChanges 分开是因为用户该采取的动作完全不同：一个是「解锁后再试」，
	// 一个是「数据已经是最新的」。
	QueueAllFieldsLocked QueueStatus = "all_fields_locked"
	// QueueRejectedBefore 表示这份提案与一条**已被用户拒绝**的记录逐字相同。
	// 去重必须同时覆盖待裁决与已拒绝的记录，否则用户拒绝之后，下一次刮削
	//（尤其是定时的全库刮削）会把同一份提案原样再塞回队列，逼用户反复拒绝同一条东西。
	QueueRejectedBefore QueueStatus = "rejected_before"
)

// QueueOptions 控制入队的去重行为。
type QueueOptions struct {
	// IgnoreRejected 让本次入队跳过「与已拒绝记录去重」这一步。
	// 交互式的刮削入口会在用户显式要求时置位——否则一旦拒绝过，用户就再也没法
	// 把同一份数据重新加回队列了，这是个死胡同。后台批量刮削永远不置位。
	IgnoreRejected bool
}

// QueueResult 是一次入队的已分类结果。
// Proposal 与 Fields 只在 QueueQueued 与 QueueReusedExisting 下有值。
type QueueResult struct {
	Status   QueueStatus
	Proposal database.MetadataReview
	Fields   []database.MetadataReviewField
}

// Queue 把一次刮削结果入队为待裁决提案。result 必须非空。
//
// series 由调用方交进来而不是按 id 加载：三个刮削入口都已握着它，批量刮削那处还在循环里。
// error 只表示故障，五种结局全部落在 QueueResult.Status 上。
func (s *Service) Queue(ctx context.Context, series database.Series, result *metadata.SeriesMetadata, providerName, sourceQuery string, opts QueueOptions) (QueueResult, error) {
	sourceURL := resolveSourceURL(providerName, result)
	confidence := result.Confidence
	if confidence <= 0 {
		confidence = defaultConfidence(providerName)
	}

	var out QueueResult
	err := s.db.ExecTx(ctx, func(q Queries) error {
		tags, err := q.GetTagsForSeries(ctx, series.ID)
		if err != nil {
			return err
		}
		authors, err := q.GetAuthorsForSeries(ctx, series.ID)
		if err != nil {
			return err
		}

		changes, lockedSkipped := buildFieldDrafts(series, tags, authors, result, confidence)
		if len(changes) == 0 {
			if lockedSkipped > 0 {
				out.Status = QueueAllFieldsLocked
			} else {
				out.Status = QueueNoChanges
			}
			return nil
		}

		lockedNow := lockedFieldSet(series)
		nextSignature := draftSignature(changes, int64(result.SourceID))

		// ListPendingMetadataReviewsBySeries 带 ORDER BY confidence DESC, created_at DESC，
		// 所以「多条历史记录过滤后签名相同」时复用哪一条是确定的：置信度最高、最新的那条。
		pending, err := q.ListPendingMetadataReviewsBySeries(ctx, series.ID)
		if err != nil {
			return err
		}
		match, fields, found, err := firstMatching(ctx, q, pending, nextSignature, lockedNow)
		if err != nil {
			return err
		}
		if found {
			out.Status = QueueReusedExisting
			out.Proposal = match
			out.Fields = fields
			return nil
		}

		if !opts.IgnoreRejected {
			rejected, err := q.ListRecentRejectedMetadataReviewsBySeries(ctx, database.ListRecentRejectedMetadataReviewsBySeriesParams{
				SeriesID: series.ID,
				Limit:    rejectedDedupWindow,
			})
			if err != nil {
				return err
			}
			if _, _, matched, err := firstMatching(ctx, q, rejected, nextSignature, lockedNow); err != nil {
				return err
			} else if matched {
				out.Status = QueueRejectedBefore
				return nil
			}
		}

		payload, _ := json.Marshal(result)
		created, err := q.CreateMetadataReview(ctx, database.CreateMetadataReviewParams{
			SeriesID:    series.ID,
			Provider:    strings.TrimSpace(providerName),
			SourceUrl:   sourceURL,
			SourceID:    int64(result.SourceID),
			SourceQuery: strings.TrimSpace(sourceQuery),
			Summary:     fmt.Sprintf("Queued %d metadata fields for review", len(changes)),
			Confidence:  confidence,
			Status:      "pending",
			RawPayload:  string(payload),
		})
		if err != nil {
			return err
		}

		for _, change := range changes {
			field, err := q.CreateMetadataReviewField(ctx, database.CreateMetadataReviewFieldParams{
				ReviewID:      created.ID,
				FieldName:     change.Name,
				CurrentValue:  change.Current,
				ProposedValue: change.Proposed,
				Confidence:    change.Confidence,
				Source:        strings.TrimSpace(providerName),
				SourceUrl:     sourceURL,
				// 锁定字段根本走不到这里（buildFieldDrafts 已把它们筛掉），
				// 所以行上这个快照恒为 false；渲染与应用一律按系列的当前锁定集算。
				Locked: false,
			})
			if err != nil {
				return err
			}
			out.Fields = append(out.Fields, field)
		}

		out.Status = QueueQueued
		out.Proposal = created
		return nil
	})
	if err != nil {
		return QueueResult{}, err
	}
	return out, nil
}

// firstMatching 返回 candidates 里第一条与 signature 逐字相同的提案及其字段行。
// candidates 的顺序即优先级，由调用方那条查询的 ORDER BY 决定。
func firstMatching(ctx context.Context, q Queries, candidates []database.MetadataReview, signature map[string]string, locked map[string]bool) (database.MetadataReview, []database.MetadataReviewField, bool, error) {
	if len(candidates) == 0 {
		// 空列表被 sqlc 拼成 IN (NULL)，能查、只是白跑一次往返。
		return database.MetadataReview{}, nil, false, nil
	}
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	rows, err := q.ListMetadataReviewFieldsByReviews(ctx, ids)
	if err != nil {
		return database.MetadataReview{}, nil, false, err
	}
	grouped := groupFieldRows(rows, len(candidates))
	for _, candidate := range candidates {
		fields := grouped[candidate.ID]
		if signaturesEqual(signature, rowsSignature(fields, candidate.SourceID, locked)) {
			return candidate, fields, true, nil
		}
	}
	return database.MetadataReview{}, nil, false, nil
}
