// 本文件是提案的读通路：按系列列出、收件箱分页、待裁决计数。
//
// 三条路径都不开事务——读到的是某一瞬间的快照，真正的守卫在 Apply 与 Reject 那边。
//
// 字段行一律经批量查询一次取回再按提案分组：逐条版不在接口里，「在一批提案上逐条取
// 字段行」这个 N+1 因此在本包内写不出来。

package proposal

import (
	"context"
	"database/sql"

	"manga-manager/internal/database"
)

// Field 是一条提案里的一个字段。
//
// Current 与 Locked 都是**终值**：按系列此刻的状态算出来，不是字段行上那两个入队瞬间的
// 快照。这是裁决规则而不是渲染细节，展示与裁决必须同一个口径——照快照渲染，用户会看到
// 一个「当前值：(空)」却在应用时被跳过的字段，或是一个挂着锁徽章却被照写的字段。
type Field struct {
	Name       string
	Current    string
	Proposed   string
	Confidence float64
	Locked     bool
	Source     string
	SourceURL  string
}

// Proposal 是一条提案连同它的字段。Row 是提案自身那一行，读路径原样交出，不做改写。
type Proposal struct {
	Row    database.MetadataReview
	Fields []Field
}

// SeriesProposals 是一个系列上的待裁决提案与它当前的来源沿革。
type SeriesProposals struct {
	Proposals  []Proposal
	Provenance []database.SeriesMetadataProvenance
}

// InboxQuery 是收件箱一页的过滤与分页条件。LibraryID、Provider、Keyword 取零值即不过滤。
//
// Keyword 而不是 Query：本包里 Queries 已经是「事务内的查询集合」，同一个词再指一次
// 用户敲进搜索框的字，读代码时得先分辨是哪一个。
type InboxQuery struct {
	LibraryID int64
	Provider  string
	Keyword   string
	Offset    int64
	Limit     int64
}

// InboxItem 是收件箱里的一条待裁决提案。Row 一并带着列表要展示的系列与书库信息。
type InboxItem struct {
	Row    database.ListPendingMetadataReviewInboxRow
	Fields []Field
}

// InboxPage 是收件箱的一页。Total 是**过滤后、分页前**的总数。
type InboxPage struct {
	Items []InboxItem
	Total int64
}

// ListBySeries 返回一个系列上的待裁决提案与来源沿革。
func (s *Service) ListBySeries(ctx context.Context, seriesID int64) (SeriesProposals, error) {
	// 取一次系列与它的两个集合：字段的当前值与锁定标志都按它们算，不按行上的快照。
	series, err := s.db.GetSeries(ctx, seriesID)
	if err != nil {
		return SeriesProposals{}, err
	}
	tags, err := s.db.GetTagsForSeries(ctx, seriesID)
	if err != nil {
		return SeriesProposals{}, err
	}
	authors, err := s.db.GetAuthorsForSeries(ctx, seriesID)
	if err != nil {
		return SeriesProposals{}, err
	}
	locked := lockedFieldSet(series)
	current := currentFieldValues(series, tags, authors)

	rows, err := s.db.ListPendingMetadataReviewsBySeries(ctx, seriesID)
	if err != nil {
		return SeriesProposals{}, err
	}
	provenance, err := s.db.GetSeriesMetadataProvenance(ctx, seriesID)
	if err != nil {
		return SeriesProposals{}, err
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	fieldRows, err := s.fieldRowsByProposal(ctx, ids)
	if err != nil {
		return SeriesProposals{}, err
	}

	out := SeriesProposals{
		Proposals:  make([]Proposal, 0, len(rows)),
		Provenance: provenance,
	}
	for _, row := range rows {
		out.Proposals = append(out.Proposals, Proposal{
			Row:    row,
			Fields: toFields(fieldRows[row.ID], locked, current),
		})
	}
	return out, nil
}

// Inbox 返回一页待裁决提案与过滤后的总数。
//
// 锁定集与当前值都逐行各算各的：一页里的提案跨多个系列，共用一份会把别的系列的值串过来。
func (s *Service) Inbox(ctx context.Context, query InboxQuery) (InboxPage, error) {
	total, err := s.db.CountPendingMetadataReviewInbox(ctx, database.CountPendingMetadataReviewInboxParams{
		LibraryID: query.LibraryID,
		Provider:  query.Provider,
		Query:     query.Keyword,
	})
	if err != nil {
		return InboxPage{}, err
	}

	rows, err := s.db.ListPendingMetadataReviewInbox(ctx, database.ListPendingMetadataReviewInboxParams{
		LibraryID: query.LibraryID,
		Provider:  query.Provider,
		Query:     query.Keyword,
		Offset:    query.Offset,
		Limit:     query.Limit,
	})
	if err != nil {
		return InboxPage{}, err
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	fieldRows, err := s.fieldRowsByProposal(ctx, ids)
	if err != nil {
		return InboxPage{}, err
	}

	tags, authors, err := s.collectionsBySeries(ctx, seriesIDsOf(rows))
	if err != nil {
		return InboxPage{}, err
	}

	page := InboxPage{
		Items: make([]InboxItem, 0, len(rows)),
		Total: total,
	}
	for _, row := range rows {
		current := currentFieldValues(seriesFromInboxRow(row), tags[row.SeriesID], authors[row.SeriesID])
		page.Items = append(page.Items, InboxItem{
			Row:    row,
			Fields: toFields(fieldRows[row.ID], parseLockedFields(row.SeriesLockedFields), current),
		})
	}
	return page, nil
}

// seriesIDsOf 给出这一页涉及的系列 id，去重。
func seriesIDsOf(rows []database.ListPendingMetadataReviewInboxRow) []int64 {
	seen := make(map[int64]struct{}, len(rows))
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.SeriesID]; ok {
			continue
		}
		seen[row.SeriesID] = struct{}{}
		ids = append(ids, row.SeriesID)
	}
	return ids
}

// collectionsBySeries 一次取回这批系列的标签与作者并按系列分组。
// 逐个系列去取就是一页有几个系列、就多几倍查询的 N+1，而收件箱一页可达上百条。
func (s *Service) collectionsBySeries(ctx context.Context, seriesIDs []int64) (map[int64][]database.Tag, map[int64][]database.Author, error) {
	tags := map[int64][]database.Tag{}
	authors := map[int64][]database.Author{}
	if len(seriesIDs) == 0 {
		// 空列表被 sqlc 拼成 IN (NULL)，能查、只是白跑两次往返。
		return tags, authors, nil
	}
	tagRows, err := s.db.ListTagsBySeriesIDs(ctx, seriesIDs)
	if err != nil {
		return nil, nil, err
	}
	for _, row := range tagRows {
		tags[row.SeriesID] = append(tags[row.SeriesID], database.Tag{Name: row.Name})
	}
	authorRows, err := s.db.ListAuthorsBySeriesIDs(ctx, seriesIDs)
	if err != nil {
		return nil, nil, err
	}
	for _, row := range authorRows {
		authors[row.SeriesID] = append(authors[row.SeriesID], database.Author{Name: row.Name, Role: row.Role})
	}
	return tags, authors, nil
}

// seriesFromInboxRow 把收件箱那一行上的系列列折回 database.Series。
// 只有算当前值用得到的那几列有值——收件箱查询也只取这几列，别处不要拿它当一整行系列用。
func seriesFromInboxRow(row database.ListPendingMetadataReviewInboxRow) database.Series {
	return database.Series{
		ID:        row.SeriesID,
		Name:      row.SeriesName,
		Title:     nullIfEmpty(row.SeriesTitle),
		Summary:   nullIfEmpty(row.SeriesSummary),
		Publisher: nullIfEmpty(row.SeriesPublisher),
		Status:    nullIfEmpty(row.SeriesStatus),
		Rating:    sql.NullFloat64{Float64: row.SeriesRating, Valid: row.SeriesRating > 0},
	}
}

func nullIfEmpty(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

// PendingCount 回答「全库有多少条待裁决提案」，供审核中心的角标取数。
//
// 单独成一个方法而不是拿 Inbox 传 limit=0：后者要么白跑一次列表查询，要么在这里为
// limit==0 开一条特例分支——那是一条必须先知道才能用对的约定。
func (s *Service) PendingCount(ctx context.Context) (int64, error) {
	return s.db.CountPendingMetadataReviewInbox(ctx, database.CountPendingMetadataReviewInboxParams{})
}

// fieldRowsByProposal 一次取回这批提案的字段行并按提案分组。
func (s *Service) fieldRowsByProposal(ctx context.Context, proposalIDs []int64) (map[int64][]database.MetadataReviewField, error) {
	if len(proposalIDs) == 0 {
		// 空列表被 sqlc 拼成 IN (NULL)，能查、只是白跑一次往返。
		return map[int64][]database.MetadataReviewField{}, nil
	}
	rows, err := s.db.ListMetadataReviewFieldsByReviews(ctx, proposalIDs)
	if err != nil {
		return nil, err
	}
	return groupFieldRows(rows, len(proposalIDs)), nil
}

// groupFieldRows 把批量查询的结果按提案分组，proposalCount 只作预分配的容量提示。
// 分组保序：批量查询按 review_id、id 升序返回，每条提案内部因此仍是字段行的入库顺序。
func groupFieldRows(rows []database.MetadataReviewField, proposalCount int) map[int64][]database.MetadataReviewField {
	grouped := make(map[int64][]database.MetadataReviewField, proposalCount)
	for _, row := range rows {
		grouped[row.ReviewID] = append(grouped[row.ReviewID], row)
	}
	return grouped
}

// toFields 把字段行折成对外的 Field，当前值与锁定标志按系列此刻的状态（current 与 locked）
// 算出终值。两者都不取行上的快照：那是入队瞬间的样子，用户后来的编辑与解锁都不在里面。
func toFields(rows []database.MetadataReviewField, locked map[string]bool, current map[string]string) []Field {
	fields := make([]Field, 0, len(rows))
	for _, row := range rows {
		fields = append(fields, Field{
			Name:       row.FieldName,
			Current:    current[row.FieldName],
			Proposed:   row.ProposedValue,
			Confidence: row.Confidence,
			// 只认系列的当前锁定集，不看行上那个入队瞬间的快照——裁决那边也只认它。
			// 与快照取或的话，老库里遗留的 locked=1 会让用户看到一个「锁着」的字段，
			// 点应用却照写不误；而用户把它解锁之后，徽章还会一直挂着。
			Locked:    locked[row.FieldName],
			Source:    row.Source,
			SourceURL: row.SourceUrl,
		})
	}
	return fields
}
