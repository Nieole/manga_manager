package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/proposal"
)

var errNoMetadataChanges = errors.New("no metadata changes")

// errAllFieldsLocked 表示确实有差异，但差异字段全部被用户锁定了。
// 与 errNoMetadataChanges 分开是因为用户该采取的动作完全不同：一个是「解锁后再试」，
// 一个是「数据已经是最新的」。
var errAllFieldsLocked = errors.New("all changed fields are locked")

// errMetadataReviewRejectedBefore 表示这份提案与一条**已被用户拒绝**的记录逐字相同。
// 去重必须同时覆盖 pending 与已拒绝的记录，否则用户拒绝之后，下一次刮削
// （尤其是定时的全库刮削）会把同一份提案原样再塞回队列，逼用户反复拒绝同一条东西。
var errMetadataReviewRejectedBefore = errors.New("an identical proposal was rejected before")

// rejectedDedupWindow 是回溯多少条最近的拒绝记录参与去重。
//
// 刻意不做成「全部历史拒绝记录」：那会随时间无界增长，而用户真正会反复看到的
// 就是最近那些。取 20 已经远超实际使用中单个系列的拒绝次数。
const rejectedDedupWindow = 20

// queueReviewOptions 控制入队的去重行为。
type queueReviewOptions struct {
	// IgnoreRejected 让本次入队跳过「与已拒绝记录去重」这一步。
	// 交互式的刮削入口会在用户显式要求时置位——否则一旦拒绝过，用户就再也没法
	// 把同一份数据重新加回队列了，这是个死胡同。后台批量刮削永远不置位。
	IgnoreRejected bool
}

type metadataReviewFieldDraft struct {
	Name       string
	Label      string
	Current    string
	Proposed   string
	Confidence float64
	Locked     bool
}

type metadataReviewFieldView struct {
	Name       string  `json:"name"`
	Label      string  `json:"label"`
	Current    string  `json:"current"`
	Proposed   string  `json:"proposed"`
	Confidence float64 `json:"confidence"`
	Locked     bool    `json:"locked"`
	Source     string  `json:"source"`
	SourceURL  string  `json:"source_url"`
}

type metadataReviewView struct {
	ID          int64                     `json:"id"`
	SeriesID    int64                     `json:"series_id"`
	Provider    string                    `json:"provider"`
	SourceURL   string                    `json:"source_url"`
	SourceID    int64                     `json:"source_id"`
	SourceQuery string                    `json:"source_query"`
	Summary     string                    `json:"summary"`
	Confidence  float64                   `json:"confidence"`
	Status      string                    `json:"status"`
	RawPayload  string                    `json:"raw_payload"`
	CreatedAt   time.Time                 `json:"created_at"`
	UpdatedAt   time.Time                 `json:"updated_at"`
	AppliedAt   *time.Time                `json:"applied_at,omitempty"`
	RejectedAt  *time.Time                `json:"rejected_at,omitempty"`
	Fields      []metadataReviewFieldView `json:"fields"`
}

type metadataProvenanceView struct {
	FieldName  string    `json:"field_name"`
	Label      string    `json:"label"`
	Value      string    `json:"value"`
	Source     string    `json:"source"`
	SourceURL  string    `json:"source_url"`
	Confidence float64   `json:"confidence"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type metadataReviewResponse struct {
	Reviews    []metadataReviewView     `json:"reviews"`
	Provenance []metadataProvenanceView `json:"provenance"`
}

type metadataReviewInboxItemView struct {
	metadataReviewView
	LibraryID        int64  `json:"library_id"`
	LibraryName      string `json:"library_name"`
	SeriesName       string `json:"series_name"`
	SeriesTitle      string `json:"series_title"`
	CoverBookID      int64  `json:"cover_book_id"`
	FieldCount       int64  `json:"field_count"`
	LockedFieldCount int64  `json:"locked_field_count"`
}

type metadataReviewInboxResponse struct {
	Items  []metadataReviewInboxItemView `json:"items"`
	Total  int64                         `json:"total"`
	Limit  int64                         `json:"limit"`
	Offset int64                         `json:"offset"`
}

type metadataReviewBulkRequest struct {
	ReviewIDs []int64 `json:"review_ids"`
	Mode      string  `json:"mode"`
}

type metadataReviewBulkResponse struct {
	Success bool    `json:"success"`
	Applied []int64 `json:"applied,omitempty"`
	// Partial 是「写入了一部分提案，但整条 review 仍待处理」的那些。
	// 与 Applied 分开是因为它们还留在收件箱里——混进 Applied 会让用户以为已经处理完了。
	Partial  []int64 `json:"partial,omitempty"`
	Rejected []int64 `json:"rejected,omitempty"`
	Skipped  []int64 `json:"skipped,omitempty"`
	// Conflict 收 proposal.ApplyConflict / proposal.RejectConflict。与 Failed 分开是因为
	// 它们并没有坏：混进去会让整条汇总提示变红，而用户什么也不用做。
	Conflict []int64 `json:"conflict,omitempty"`
	// Failed 只收真故障：查询报错、写入失败、提案查不到。
	Failed []int64 `json:"failed,omitempty"`
	Total  int     `json:"total"`
	Mode   string  `json:"mode"`
}

func metadataFieldLabel(name string) string {
	switch name {
	case "title":
		return "Title"
	case "summary":
		return "Summary"
	case "publisher":
		return "Publisher"
	case "status":
		return "Status"
	case "rating":
		return "Rating"
	case "tags":
		return "Tags"
	case "authors":
		return "Authors"
	case "source_link":
		return "Source link"
	default:
		return titleFieldLabel(strings.ReplaceAll(name, "_", " "))
	}
}

// titleFieldLabel upper-cases the first letter of each space-separated word (ASCII field keys such
// as "release date"), replacing the deprecated strings.Title for the metadata-field-label use case.
func titleFieldLabel(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func metadataLockedFieldSet(series database.Series) map[string]bool {
	if !series.LockedFields.Valid {
		return map[string]bool{}
	}
	return parseLockedFieldSet(series.LockedFields.String)
}

// parseLockedFieldSet 解析 locked_fields 的逗号分隔表示。
// 单独抽出来是因为收件箱查询直接取的是 s.locked_fields 字符串（没有整行 Series）。
func parseLockedFieldSet(raw string) map[string]bool {
	locked := make(map[string]bool)
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field != "" {
			locked[field] = true
		}
	}
	return locked
}

func metadataSeriesDisplayTitle(series database.Series) string {
	if series.Title.Valid && strings.TrimSpace(series.Title.String) != "" {
		return series.Title.String
	}
	return series.Name
}

func metadataJoinTags(tags []database.Tag) string {
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

func metadataCleanTags(tags []string) []string {
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
	return cleaned
}

func metadataJoinProposedTags(tags []string) string {
	return strings.Join(metadataCleanTags(tags), " / ")
}

func metadataAuthorEntryString(name, role string) string {
	name = strings.TrimSpace(name)
	role = strings.TrimSpace(role)
	if role == "" {
		return name
	}
	return name + " (" + role + ")"
}

func metadataJoinAuthors(authors []database.Author) string {
	if len(authors) == 0 {
		return ""
	}
	parts := make([]string, 0, len(authors))
	for _, a := range authors {
		if strings.TrimSpace(a.Name) == "" {
			continue
		}
		parts = append(parts, metadataAuthorEntryString(a.Name, a.Role))
	}
	sort.Strings(parts)
	return strings.Join(parts, " / ")
}

func metadataJoinProposedAuthors(authors []metadata.SeriesAuthor) string {
	if len(authors) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(authors))
	parts := make([]string, 0, len(authors))
	for _, a := range authors {
		entry := metadataAuthorEntryString(a.Name, a.Role)
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

func metadataDefaultConfidence(providerName string) float64 {
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
	// providerName 也可能是 provider.Name() 的显示名（如 "Ollama LLM"、
	// "OpenAI/Compatible LLM"、"OpenAI Compatible (v1/chat/completions)"），
	// 这里对显示名做包含匹配兜底，确保各 LLM provider 一视同仁。
	switch {
	case strings.Contains(name, "bangumi"):
		return 0.9
	case strings.Contains(name, "llm"), strings.Contains(name, "openai"), strings.Contains(name, "ollama"):
		return 0.6
	default:
		return 0.5
	}
}

func metadataSourceURL(providerName string, result *metadata.SeriesMetadata) string {
	if result == nil {
		return ""
	}
	if strings.TrimSpace(result.SourceURL) != "" {
		return strings.TrimSpace(result.SourceURL)
	}
	if result.SourceID > 0 && strings.EqualFold(providerName, "bangumi") {
		return fmt.Sprintf("https://bgm.tv/subject/%d", result.SourceID)
	}
	return ""
}

// metadataBuildFieldDrafts 产出可入队的字段提案，并返回「因锁定而被跳过」的字段数。
// 后者用来把「全被锁住」与「本来就没差异」两种结果区分开。
func metadataBuildFieldDrafts(series database.Series, tags []database.Tag, authors []database.Author, result *metadata.SeriesMetadata, providerName string) ([]metadataReviewFieldDraft, int) {
	locked := metadataLockedFieldSet(series)
	currentTags := metadataJoinTags(tags)
	proposedTags := metadataJoinProposedTags(result.Tags)
	currentAuthors := metadataJoinAuthors(authors)
	proposedAuthors := metadataJoinProposedAuthors(result.Authors)
	confidence := result.Confidence
	if confidence <= 0 {
		confidence = metadataDefaultConfidence(providerName)
	}

	drafts := []metadataReviewFieldDraft{
		{
			Name:       "title",
			Label:      metadataFieldLabel("title"),
			Current:    metadataSeriesDisplayTitle(series),
			Proposed:   strings.TrimSpace(result.Title),
			Confidence: confidence,
			Locked:     locked["title"],
		},
		{
			Name:       "summary",
			Label:      metadataFieldLabel("summary"),
			Current:    seriesText(series.Summary),
			Proposed:   strings.TrimSpace(result.Summary),
			Confidence: confidence,
			Locked:     locked["summary"],
		},
		{
			Name:       "publisher",
			Label:      metadataFieldLabel("publisher"),
			Current:    seriesText(series.Publisher),
			Proposed:   strings.TrimSpace(result.Publisher),
			Confidence: confidence,
			Locked:     locked["publisher"],
		},
		{
			Name:    "status",
			Label:   metadataFieldLabel("status"),
			Current: seriesText(series.Status),
			// 用 Optional 版：数据源没给状态时提案留空，由下游的「空提案不入队」逻辑丢弃。
			// 折成 "unknown" 会让不提供状态的数据源每次刮削都提议把已有的正确状态改成 unknown。
			Proposed:   metadataOptionalStatus(result.Status),
			Confidence: confidence,
			Locked:     locked["status"],
		},
		{
			Name:       "rating",
			Label:      metadataFieldLabel("rating"),
			Current:    seriesNumber(series.Rating),
			Proposed:   metadataNumber(result.Rating),
			Confidence: confidence,
			Locked:     locked["rating"],
		},
		{
			Name:       "tags",
			Label:      metadataFieldLabel("tags"),
			Current:    currentTags,
			Proposed:   proposedTags,
			Confidence: confidence,
			Locked:     locked["tags"],
		},
		{
			Name:       "authors",
			Label:      metadataFieldLabel("authors"),
			Current:    currentAuthors,
			Proposed:   proposedAuthors,
			Confidence: confidence,
			Locked:     locked["authors"],
		},
	}

	changes := make([]metadataReviewFieldDraft, 0, len(drafts))
	lockedSkipped := 0
	for _, draft := range drafts {
		if draft.Proposed == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(draft.Current), strings.TrimSpace(draft.Proposed)) {
			continue
		}
		// 锁定字段不得入队：入队的话 apply 会静默跳过它、却仍把整条 review 标成已应用，
		// 用户点「应用」实际什么也没发生，界面上却显示成功。
		//
		// 这个判断必须排在上面两个 continue **之后**：放最前面的话，
		// 「提案为空」或「与当前值完全相同」的锁定字段也会被算进 lockedSkipped，
		// 于是一次「数据毫无差异、但恰好有个字段被锁」的刮削会报「差异字段均已被锁定」，
		// 正好把要区分的两种语义搞反。lockedSkipped 只统计「不锁就会入队」的字段。
		if draft.Locked {
			lockedSkipped++
			continue
		}
		changes = append(changes, draft)
	}
	return changes, lockedSkipped
}

func seriesText(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return strings.TrimSpace(value.String)
}

func seriesNumber(value sql.NullFloat64) string {
	if !value.Valid || value.Float64 <= 0 {
		return ""
	}
	return strconv.FormatFloat(value.Float64, 'f', 1, 64)
}

// metadataOptionalStatus 返回可入队的状态提案；数据源未提供或无法识别时返回空串，
// 由「空提案不入队」的既有逻辑自然丢弃。
func metadataOptionalStatus(value string) string {
	code, ok := metadata.NormalizeStatusCodeOptional(value)
	if !ok {
		return ""
	}
	return code
}

func metadataNumber(value float64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', 1, 64)
}

// metadataReviewFieldToView 把待审字段转成前端视图。
//
// lockedNow 是**当前**的锁定集。不能直接用行上的 field.Locked——那是入队瞬间的快照，
// 「先入队、后加锁」的行 locked=false，于是 UI 上没有任何锁徽章，用户点了应用，
// 该字段却被静默丢弃。锁定状态是系列的当前属性，展示时就该按当前值算。
func metadataReviewFieldToView(field database.MetadataReviewField, lockedNow map[string]bool) metadataReviewFieldView {
	return metadataReviewFieldView{
		Name:       field.FieldName,
		Label:      metadataFieldLabel(field.FieldName),
		Current:    field.CurrentValue,
		Proposed:   field.ProposedValue,
		Confidence: field.Confidence,
		Locked:     field.Locked || lockedNow[field.FieldName],
		Source:     field.Source,
		SourceURL:  field.SourceUrl,
	}
}

func metadataReviewToView(review database.MetadataReview, fields []database.MetadataReviewField, lockedNow map[string]bool) metadataReviewView {
	view := metadataReviewView{
		ID:          review.ID,
		SeriesID:    review.SeriesID,
		Provider:    review.Provider,
		SourceURL:   review.SourceUrl,
		SourceID:    review.SourceID,
		SourceQuery: review.SourceQuery,
		Summary:     review.Summary,
		Confidence:  review.Confidence,
		Status:      review.Status,
		RawPayload:  review.RawPayload,
		CreatedAt:   review.CreatedAt,
		UpdatedAt:   review.UpdatedAt,
		Fields:      make([]metadataReviewFieldView, 0, len(fields)),
	}
	if review.AppliedAt.Valid {
		value := review.AppliedAt.Time
		view.AppliedAt = &value
	}
	if review.RejectedAt.Valid {
		value := review.RejectedAt.Time
		view.RejectedAt = &value
	}
	for _, field := range fields {
		view.Fields = append(view.Fields, metadataReviewFieldToView(field, lockedNow))
	}
	return view
}

func metadataReviewInboxRowToView(row database.ListPendingMetadataReviewInboxRow, fields []database.MetadataReviewField, lockedNow map[string]bool) metadataReviewInboxItemView {
	review := database.MetadataReview{
		ID:          row.ID,
		SeriesID:    row.SeriesID,
		Provider:    row.Provider,
		SourceUrl:   row.SourceUrl,
		SourceID:    row.SourceID,
		SourceQuery: row.SourceQuery,
		Summary:     row.Summary,
		Confidence:  row.Confidence,
		Status:      row.Status,
		RawPayload:  row.RawPayload,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		AppliedAt:   row.AppliedAt,
		RejectedAt:  row.RejectedAt,
	}
	return metadataReviewInboxItemView{
		metadataReviewView: metadataReviewToView(review, fields, lockedNow),
		LibraryID:          row.LibraryID,
		LibraryName:        row.LibraryName,
		SeriesName:         row.SeriesName,
		SeriesTitle:        row.SeriesTitle,
		CoverBookID:        row.CoverBookID,
		FieldCount:         row.FieldCount,
		LockedFieldCount:   row.LockedFieldCount,
	}
}

func provenanceToView(row database.SeriesMetadataProvenance) metadataProvenanceView {
	return metadataProvenanceView{
		FieldName:  row.FieldName,
		Label:      metadataFieldLabel(row.FieldName),
		Value:      row.Value,
		Source:     row.Source,
		SourceURL:  row.SourceUrl,
		Confidence: row.Confidence,
		UpdatedAt:  row.UpdatedAt,
	}
}

func metadataReviewIDsFromRequest(w http.ResponseWriter, r *http.Request) ([]int64, string, bool) {
	var req metadataReviewBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid metadata review payload")
		return nil, "", false
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "all"
	}
	if mode != "all" && mode != "fill_empty" {
		jsonError(w, http.StatusBadRequest, "Invalid metadata review mode")
		return nil, "", false
	}
	seen := make(map[int64]struct{}, len(req.ReviewIDs))
	ids := make([]int64, 0, len(req.ReviewIDs))
	for _, id := range req.ReviewIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		jsonError(w, http.StatusBadRequest, "No metadata review IDs provided")
		return nil, "", false
	}
	if len(ids) > 100 {
		jsonError(w, http.StatusBadRequest, "Too many metadata reviews in one request")
		return nil, "", false
	}
	return ids, mode, true
}

func normalizeMetadataReviewValue(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func metadataReviewDraftSignature(changes []metadataReviewFieldDraft, sourceID int64) map[string]string {
	signature := make(map[string]string, len(changes)+1)
	for _, change := range changes {
		signature[change.Name] = normalizeMetadataReviewValue(change.Current) + "\x00" + normalizeMetadataReviewValue(change.Proposed)
	}
	// 把来源条目 ID 纳入签名：不同来源（如 Bangumi 不同 subject）即使字段 diff 相同
	// 也应视为不同候选，分别入队，避免复用旧 review 时丢弃用户选中条目的 source_url。
	signature["\x00source_id"] = strconv.FormatInt(sourceID, 10)
	return signature
}

// metadataReviewFieldsSignature 给已入库的待审字段算签名。
//
// locked 参数是**当前**的锁定集，不是行上那个入队瞬间的 Locked 快照：新一轮刮削产出的
// changes 已经把当前锁定的字段筛掉了，这边若不同口径地筛，「入队后用户又加了锁」就会让
// 两边签名永远对不上，于是每次刮削都为同一个系列再堆一条几乎相同的待审记录。
func metadataReviewFieldsSignature(fields []database.MetadataReviewField, sourceID int64, locked map[string]bool) map[string]string {
	signature := make(map[string]string, len(fields)+1)
	for _, field := range fields {
		if locked[field.FieldName] {
			continue
		}
		signature[field.FieldName] = normalizeMetadataReviewValue(field.CurrentValue) + "\x00" + normalizeMetadataReviewValue(field.ProposedValue)
	}
	signature["\x00source_id"] = strconv.FormatInt(sourceID, 10)
	return signature
}

func metadataReviewSignaturesEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if right[key] != leftValue {
			return false
		}
	}
	return true
}

func (c *Controller) queueMetadataReview(ctx context.Context, series database.Series, result *metadata.SeriesMetadata, providerName, sourceQuery string) (database.MetadataReview, []database.MetadataReviewField, bool, error) {
	return c.queueMetadataReviewWithOptions(ctx, series, result, providerName, sourceQuery, queueReviewOptions{})
}

func (c *Controller) queueMetadataReviewWithOptions(ctx context.Context, series database.Series, result *metadata.SeriesMetadata, providerName, sourceQuery string, opts queueReviewOptions) (database.MetadataReview, []database.MetadataReviewField, bool, error) {
	var createdReview database.MetadataReview
	var createdFields []database.MetadataReviewField
	sourceURL := metadataSourceURL(providerName, result)
	confidence := result.Confidence
	if confidence <= 0 {
		confidence = metadataDefaultConfidence(providerName)
	}

	var isNew bool
	err := c.store.ExecTx(ctx, func(q *database.Queries) error {
		tags, err := q.GetTagsForSeries(ctx, series.ID)
		if err != nil {
			return err
		}
		authors, err := q.GetAuthorsForSeries(ctx, series.ID)
		if err != nil {
			return err
		}

		changes, lockedSkipped := metadataBuildFieldDrafts(series, tags, authors, result, providerName)
		if len(changes) == 0 {
			if lockedSkipped > 0 {
				// 与「本来就没差异」区分开：这里是有差异但全被用户锁住了，
				// 调用方该告诉用户「解锁后再试」，而不是「数据已是最新」。
				return errAllFieldsLocked
			}
			return errNoMetadataChanges
		}
		lockedNow := metadataLockedFieldSet(series)
		nextSignature := metadataReviewDraftSignature(changes, int64(result.SourceID))
		pendingReviews, err := q.ListPendingMetadataReviewsBySeries(ctx, series.ID)
		if err != nil {
			return err
		}
		// ListPendingMetadataReviewsBySeries 带 ORDER BY confidence DESC, created_at DESC，
		// 所以「多条历史记录过滤后签名相同」时复用哪一条是确定的：置信度最高、最新的那条。
		for _, pendingReview := range pendingReviews {
			fields, err := q.ListMetadataReviewFields(ctx, pendingReview.ID)
			if err != nil {
				return err
			}
			if metadataReviewSignaturesEqual(nextSignature, metadataReviewFieldsSignature(fields, pendingReview.SourceID, lockedNow)) {
				createdReview = pendingReview
				createdFields = fields
				isNew = false
				return nil
			}
		}

		// 再跟最近被拒绝的记录比一遍。只比 pending 的话，用户拒绝之后下一次刮削
		// （尤其是定时的全库刮削）会把同一份提案原样塞回来，变成反复拒绝同一条东西。
		if !opts.IgnoreRejected {
			rejected, err := q.ListRecentRejectedMetadataReviewsBySeries(ctx, database.ListRecentRejectedMetadataReviewsBySeriesParams{
				SeriesID: series.ID,
				Limit:    rejectedDedupWindow,
			})
			if err != nil {
				return err
			}
			for _, rejectedReview := range rejected {
				fields, err := q.ListMetadataReviewFields(ctx, rejectedReview.ID)
				if err != nil {
					return err
				}
				if metadataReviewSignaturesEqual(nextSignature, metadataReviewFieldsSignature(fields, rejectedReview.SourceID, lockedNow)) {
					return errMetadataReviewRejectedBefore
				}
			}
		}

		payload, _ := json.Marshal(result)
		review, err := q.CreateMetadataReview(ctx, database.CreateMetadataReviewParams{
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
				ReviewID:      review.ID,
				FieldName:     change.Name,
				CurrentValue:  change.Current,
				ProposedValue: change.Proposed,
				Confidence:    change.Confidence,
				Source:        strings.TrimSpace(providerName),
				SourceUrl:     sourceURL,
				Locked:        change.Locked,
			})
			if err != nil {
				return err
			}
			createdFields = append(createdFields, field)
		}

		createdReview = review
		isNew = true
		return nil
	})

	if err != nil {
		return database.MetadataReview{}, nil, false, err
	}

	return createdReview, createdFields, isNew, nil
}

func (c *Controller) listSeriesMetadataReview(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	payload, err := c.loadSeriesMetadataReview(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list metadata reviews")
		return
	}

	jsonResponse(w, http.StatusOK, payload)
}

// metadataReviewFieldsByReview 一次取回这批提案的字段行，并按提案分组。
//
// 逐条取是 N+1：一个系列有几条待裁决提案，就是几次额外查询。
// 分组保序，每条提案内部仍是 id 升序，与逐条版给出的顺序一致。
func (c *Controller) metadataReviewFieldsByReview(ctx context.Context, reviewIDs []int64) (map[int64][]database.MetadataReviewField, error) {
	grouped := make(map[int64][]database.MetadataReviewField, len(reviewIDs))
	if len(reviewIDs) == 0 {
		// 空列表被 sqlc 拼成 IN (NULL)，能查、只是白跑一次往返。
		return grouped, nil
	}
	fields, err := c.store.ListMetadataReviewFieldsByReviews(ctx, reviewIDs)
	if err != nil {
		return nil, err
	}
	for _, field := range fields {
		grouped[field.ReviewID] = append(grouped[field.ReviewID], field)
	}
	return grouped, nil
}

func (c *Controller) loadSeriesMetadataReview(ctx context.Context, seriesID int64) (metadataReviewResponse, error) {
	// 取一次系列，用于按**当前**锁定集渲染字段的 locked 徽章（行上那个是入队瞬间的快照）。
	series, err := c.store.GetSeries(ctx, seriesID)
	if err != nil {
		return metadataReviewResponse{}, err
	}
	lockedNow := metadataLockedFieldSet(series)

	reviews, err := c.store.ListPendingMetadataReviewsBySeries(ctx, seriesID)
	if err != nil {
		return metadataReviewResponse{}, err
	}
	if reviews == nil {
		reviews = []database.MetadataReview{}
	}

	provenanceRows, err := c.store.GetSeriesMetadataProvenance(ctx, seriesID)
	if err != nil {
		return metadataReviewResponse{}, err
	}
	if provenanceRows == nil {
		provenanceRows = []database.SeriesMetadataProvenance{}
	}

	reviewIDs := make([]int64, 0, len(reviews))
	for _, review := range reviews {
		reviewIDs = append(reviewIDs, review.ID)
	}
	fieldsByReview, err := c.metadataReviewFieldsByReview(ctx, reviewIDs)
	if err != nil {
		return metadataReviewResponse{}, err
	}

	payload := metadataReviewResponse{
		Reviews:    make([]metadataReviewView, 0, len(reviews)),
		Provenance: make([]metadataProvenanceView, 0, len(provenanceRows)),
	}
	for _, review := range reviews {
		payload.Reviews = append(payload.Reviews, metadataReviewToView(review, fieldsByReview[review.ID], lockedNow))
	}
	for _, row := range provenanceRows {
		payload.Provenance = append(payload.Provenance, provenanceToView(row))
	}

	return payload, nil
}

func emptyMetadataReviewResponse() metadataReviewResponse {
	return metadataReviewResponse{
		Reviews:    []metadataReviewView{},
		Provenance: []metadataProvenanceView{},
	}
}

func (c *Controller) listMetadataReviewInbox(w http.ResponseWriter, r *http.Request) {
	libraryID, _ := strconv.ParseInt(r.URL.Query().Get("library_id"), 10, 64)
	if libraryID < 0 {
		libraryID = 0
	}
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if offset < 0 {
		offset = 0
	}
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	total, err := c.store.CountPendingMetadataReviewInbox(r.Context(), database.CountPendingMetadataReviewInboxParams{
		LibraryID: libraryID,
		Provider:  provider,
		Query:     query,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to count metadata reviews")
		return
	}

	rows, err := c.store.ListPendingMetadataReviewInbox(r.Context(), database.ListPendingMetadataReviewInboxParams{
		LibraryID: libraryID,
		Provider:  provider,
		Query:     query,
		Offset:    offset,
		Limit:     limit,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list metadata reviews")
		return
	}
	if rows == nil {
		rows = []database.ListPendingMetadataReviewInboxRow{}
	}

	payload := metadataReviewInboxResponse{
		Items:  make([]metadataReviewInboxItemView, 0, len(rows)),
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
	reviewIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		reviewIDs = append(reviewIDs, row.ID)
	}
	fieldsByReview, err := c.metadataReviewFieldsByReview(r.Context(), reviewIDs)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load metadata review fields")
		return
	}
	for _, row := range rows {
		payload.Items = append(payload.Items, metadataReviewInboxRowToView(row, fieldsByReview[row.ID], parseLockedFieldSet(row.SeriesLockedFields)))
	}

	jsonResponse(w, http.StatusOK, payload)
}

// applyOutcomeCode 把模块的结局分类折成前端分支用的结果码。
//
// 映射写在这里而不是直接把 ApplyStatus 的值当 JSON 发出去：结果码是前端契约，改它要看
// web/ 里谁在读；模块的分类是领域词汇，改它只看领域。两者今天字面相同，不该因此被焊在一起。
func applyOutcomeCode(status proposal.ApplyStatus) string {
	switch status {
	case proposal.ApplyApplied:
		return "applied"
	case proposal.ApplyPartial:
		return "partial"
	case proposal.ApplyLockedSkipped:
		return "locked_skipped"
	case proposal.ApplyNoChanges:
		return "no_changes"
	default:
		return ""
	}
}

func (c *Controller) applyMetadataReview(w http.ResponseWriter, r *http.Request) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid review ID")
		return
	}

	outcome, err := c.proposals.Apply(r.Context(), reviewID, proposal.ApplyModeAll)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to apply metadata review")
		return
	}

	switch outcome.Status {
	case proposal.ApplyNotFound:
		jsonError(w, http.StatusNotFound, "Metadata review not found")
		return
	case proposal.ApplyConflict:
		// 被并发的 apply/reject（或用户重复点击、多标签页）抢先，元数据写入已整体撤销。
		// 不回吐 series 快照：抢先者的结果才是准的，返回半新半旧的数据只会误导前端。
		jsonError(w, http.StatusConflict, "Metadata review is not pending")
		return
	case proposal.ApplyLockedSkipped, proposal.ApplyNoChanges:
		// 这两种都是**良性结果**而非服务端故障，必须回 200 + applied:false + 具体 outcome，
		// 不能落 500——落 500 时前端只能提示「服务器错误」，给不出可行动的建议
		//（「先解锁字段」还是「数据已是最新」）。提案保持待裁决，不消费掉。
		jsonResponse(w, http.StatusOK, map[string]any{
			"success": true,
			"applied": false,
			"outcome": applyOutcomeCode(outcome.Status),
			"review":  reviewID,
		})
		return
	}

	updated, _ := c.store.GetSeries(r.Context(), outcome.SeriesID)
	jsonResponse(w, http.StatusOK, map[string]any{
		"success":          true,
		"applied":          true,
		"outcome":          applyOutcomeCode(outcome.Status),
		"applied_fields":   outcome.Applied,
		"remaining_fields": outcome.Remaining,
		"series":           updated,
		"review":           reviewID,
	})
}

func (c *Controller) bulkApplyMetadataReviews(w http.ResponseWriter, r *http.Request) {
	ids, mode, ok := metadataReviewIDsFromRequest(w, r)
	if !ok {
		return
	}

	result := metadataReviewBulkResponse{
		Success:  true,
		Applied:  make([]int64, 0, len(ids)),
		Partial:  make([]int64, 0),
		Skipped:  make([]int64, 0),
		Conflict: make([]int64, 0),
		Failed:   make([]int64, 0),
		Total:    len(ids),
		Mode:     mode,
	}
	for _, id := range ids {
		outcome, err := c.proposals.Apply(r.Context(), id, proposal.ApplyMode(mode))
		if err != nil {
			result.Failed = append(result.Failed, id)
			continue
		}
		switch outcome.Status {
		case proposal.ApplyApplied:
			result.Applied = append(result.Applied, id)
		case proposal.ApplyPartial:
			// 只应用了一部分（fill_empty 筛掉的、或已被锁的）。单独成桶而不是塞进 Applied：
			// 这条提案还留在收件箱里等着处理，报成「已应用」会让用户以为它已经消失了。
			result.Partial = append(result.Partial, id)
		case proposal.ApplyLockedSkipped, proposal.ApplyNoChanges:
			// 良性结果落 Skipped 而不是 Failed——前端把 Failed 当成出错来提示，
			// 会让用户以为系统坏了。
			result.Skipped = append(result.Skipped, id)
		case proposal.ApplyConflict:
			result.Conflict = append(result.Conflict, id)
		default:
			// ApplyNotFound 落这里：入参指向的行不存在，是真故障。模块若新增一种结局而
			// 这里没跟上，也落 failed——保守：报成成功会让用户以为已经处理完了。
			result.Failed = append(result.Failed, id)
		}
	}
	if len(result.Failed) > 0 {
		result.Success = false
	}
	jsonResponse(w, http.StatusOK, result)
}

func (c *Controller) rejectMetadataReview(w http.ResponseWriter, r *http.Request) {
	reviewID, err := parseID(r, "reviewId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid review ID")
		return
	}

	outcome, err := c.proposals.Reject(r.Context(), reviewID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to reject metadata review")
		return
	}
	switch outcome.Status {
	case proposal.RejectNotFound:
		jsonError(w, http.StatusNotFound, "Metadata review not found")
		return
	case proposal.RejectConflict:
		jsonError(w, http.StatusConflict, "Metadata review is not pending")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"success": true,
		"review":  reviewID,
	})
}

func (c *Controller) bulkRejectMetadataReviews(w http.ResponseWriter, r *http.Request) {
	ids, mode, ok := metadataReviewIDsFromRequest(w, r)
	if !ok {
		return
	}

	result := metadataReviewBulkResponse{
		Success:  true,
		Rejected: make([]int64, 0, len(ids)),
		Conflict: make([]int64, 0),
		Failed:   make([]int64, 0),
		Total:    len(ids),
		Mode:     mode,
	}
	for _, id := range ids {
		outcome, err := c.proposals.Reject(r.Context(), id)
		if err != nil {
			result.Failed = append(result.Failed, id)
			continue
		}
		// 归桶与批量应用一侧一致：被抢先落 Conflict，其余（RejectNotFound 与将来新增的
		// 结局）落 Failed。
		switch outcome.Status {
		case proposal.RejectRejected:
			result.Rejected = append(result.Rejected, id)
		case proposal.RejectConflict:
			result.Conflict = append(result.Conflict, id)
		default:
			result.Failed = append(result.Failed, id)
		}
	}
	if len(result.Failed) > 0 {
		result.Success = false
	}
	jsonResponse(w, http.StatusOK, result)
}
