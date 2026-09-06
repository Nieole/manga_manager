package database

import (
	"context"
	"strings"

	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
)

type HealthIssueSummary struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Count    int64  `json:"count"`
}

type HealthIssue struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	LibraryID   int64  `json:"library_id,omitempty"`
	LibraryName string `json:"library_name,omitempty"`
	SeriesID    *int64 `json:"series_id,omitempty"`
	SeriesName  string `json:"series_name,omitempty"`
	BookID      *int64 `json:"book_id,omitempty"`
	BookName    string `json:"book_name,omitempty"`
	Path        string `json:"path,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Count       int64  `json:"count,omitempty"`
	LastTaskKey string `json:"last_task_key,omitempty"`
}

type HealthIssueFilters struct {
	LibraryID    int64
	Type         string
	Limit        int
	SkipKOReader bool
}

type HealthReport struct {
	Summary []HealthIssueSummary `json:"summary"`
	Issues  []HealthIssue        `json:"issues"`
	Limit   int                  `json:"limit"`
}

type healthDef struct {
	Type     string
	Severity string
}

var healthIssueDefinitions = []healthDef{
	{Type: "empty_pages", Severity: "error"},
	{Type: "missing_cover", Severity: "warn"},
	{Type: "missing_metadata", Severity: "warn"},
	{Type: "duplicate_file_hash", Severity: "warn"},
	{Type: "missing_quick_hash", Severity: "warn"},
	{Type: "duplicate_quick_hash", Severity: "warn"},
	{Type: "unmatched_koreader", Severity: "info"},
}

func (s *SqlStore) GetHealthReport(ctx context.Context, filters HealthIssueFilters) (HealthReport, error) {
	limit := filters.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	report := HealthReport{
		Summary: make([]HealthIssueSummary, 0, len(healthIssueDefinitions)),
		Issues:  []HealthIssue{},
		Limit:   limit,
	}
	for _, def := range healthIssueDefinitions {
		if filters.SkipKOReader && def.Type == "unmatched_koreader" {
			continue
		}
		if filters.Type != "" && filters.Type != def.Type {
			continue
		}
		count, err := s.countHealthIssue(ctx, def.Type, filters.LibraryID)
		if err != nil {
			return report, err
		}
		report.Summary = append(report.Summary, HealthIssueSummary{
			Type:     def.Type,
			Severity: def.Severity,
			Count:    count,
		})
		if count == 0 {
			continue
		}
		items, err := s.listHealthIssues(ctx, def.Type, def.Severity, filters.LibraryID, limit)
		if err != nil {
			return report, err
		}
		report.Issues = append(report.Issues, items...)
	}

	if err := s.attachLastTaskKeys(ctx, report.Issues); err != nil {
		return report, err
	}

	return report, nil
}

// scopeRefsForIssue 列出一条健康问题可能挂上任务键的作用域，**系列在前**：
// 一条问题若同时落在系列与资料库上，用户想看的是那个系列自己的那次运行。
func scopeRefsForIssue(issue HealthIssue) []taskstore.ScopeRef {
	refs := make([]taskstore.ScopeRef, 0, 2)
	if issue.SeriesID != nil {
		refs = append(refs, taskstore.ScopeRef{Scope: task.ScopeSeries, ScopeID: *issue.SeriesID})
	}
	if issue.LibraryID != 0 {
		refs = append(refs, taskstore.ScopeRef{Scope: task.ScopeLibrary, ScopeID: issue.LibraryID})
	}
	return refs
}

// attachLastTaskKeys 给每条健康问题挂上它所在作用域最近那次运行的**任务键**，界面据此跳到日志。
//
// 作用域先去重再整批问一次，而不是每条问题单独发一条查询：一份报告能带回上千条问题，
// 而它们绝大多数指向同一批资料库。
func (s *SqlStore) attachLastTaskKeys(ctx context.Context, issues []HealthIssue) error {
	wanted := make(map[taskstore.ScopeRef]struct{})
	for _, issue := range issues {
		for _, ref := range scopeRefsForIssue(issue) {
			wanted[ref] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	scopes := make([]taskstore.ScopeRef, 0, len(wanted))
	for scope := range wanted {
		scopes = append(scopes, scope)
	}
	latest, err := taskstore.New(s.db).LastRunKeysForScopes(ctx, scopes)
	if err != nil {
		return err
	}

	for i := range issues {
		issue := &issues[i]
		for _, ref := range scopeRefsForIssue(*issue) {
			if key, ok := latest[ref]; ok {
				issue.LastTaskKey = key
				break
			}
		}
	}
	return nil
}

func (s *SqlStore) countHealthIssue(ctx context.Context, issueType string, libraryID int64) (int64, error) {
	switch issueType {
	case "empty_pages":
		return s.Queries.CountHealthEmptyPages(ctx, libraryID)
	case "missing_cover":
		return s.Queries.CountHealthMissingCover(ctx, libraryID)
	case "missing_metadata":
		return s.Queries.CountHealthMissingMetadata(ctx, libraryID)
	case "duplicate_file_hash":
		v, err := s.Queries.CountHealthDuplicateFileHash(ctx, libraryID)
		if err != nil {
			return 0, err
		}
		return interfaceToInt64(v), nil
	case "missing_quick_hash":
		return s.Queries.CountHealthMissingQuickHash(ctx, libraryID)
	case "duplicate_quick_hash":
		v, err := s.Queries.CountHealthDuplicateQuickHash(ctx, libraryID)
		if err != nil {
			return 0, err
		}
		return interfaceToInt64(v), nil
	case "unmatched_koreader":
		return s.Queries.CountHealthUnmatchedKOReader(ctx)
	}
	return 0, nil
}

func interfaceToInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func interfaceToString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	}
	return ""
}

func nullToInt64Ptr(v interface{}) *int64 {
	if v == nil {
		return nil
	}
	if n := interfaceToInt64(v); n != 0 {
		return &n
	}
	return nil
}

func (s *SqlStore) listHealthIssues(ctx context.Context, issueType, severity string, libraryID int64, limit int) ([]HealthIssue, error) {
	items := make([]HealthIssue, 0)
	switch issueType {
	case "empty_pages":
		rows, err := s.Queries.ListHealthEmptyPages(ctx, ListHealthEmptyPagesParams{LibraryID: libraryID, LimitCount: int64(limit)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			items = append(items, makeHealthIssue(issueType, severity, r.LibraryID, r.LibraryName, r.SeriesID, r.SeriesName, r.BookID, r.BookName, r.Path, r.Detail, r.IssueCount))
		}
	case "missing_cover":
		rows, err := s.Queries.ListHealthMissingCover(ctx, ListHealthMissingCoverParams{LibraryID: libraryID, LimitCount: int64(limit)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			items = append(items, makeHealthIssue(issueType, severity, r.LibraryID, r.LibraryName, r.SeriesID, r.SeriesName, r.BookID, r.BookName, r.Path, r.Detail, r.IssueCount))
		}
	case "missing_metadata":
		rows, err := s.Queries.ListHealthMissingMetadata(ctx, ListHealthMissingMetadataParams{LibraryID: libraryID, LimitCount: int64(limit)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			seriesID := r.SeriesID
			items = append(items, HealthIssue{
				Type:        issueType,
				Severity:    severity,
				LibraryID:   r.LibraryID,
				LibraryName: r.LibraryName,
				SeriesID:    &seriesID,
				SeriesName:  r.SeriesName,
				Path:        r.Path,
				Detail:      strings.TrimSpace(r.Detail),
				Count:       r.IssueCount,
			})
		}
	case "duplicate_file_hash":
		rows, err := s.Queries.ListHealthDuplicateFileHash(ctx, ListHealthDuplicateFileHashParams{LibraryID: libraryID, LimitCount: int64(limit)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			items = append(items, makeHealthIssue(issueType, severity, r.LibraryID, r.LibraryName, r.SeriesID, r.SeriesName, r.BookID, r.BookName, r.Path, interfaceToString(r.Detail), r.IssueCount))
		}
	case "missing_quick_hash":
		rows, err := s.Queries.ListHealthMissingQuickHash(ctx, ListHealthMissingQuickHashParams{LibraryID: libraryID, LimitCount: int64(limit)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			items = append(items, makeHealthIssue(issueType, severity, r.LibraryID, r.LibraryName, r.SeriesID, r.SeriesName, r.BookID, r.BookName, r.Path, r.Detail, r.IssueCount))
		}
	case "duplicate_quick_hash":
		rows, err := s.Queries.ListHealthDuplicateQuickHash(ctx, ListHealthDuplicateQuickHashParams{LibraryID: libraryID, LimitCount: int64(limit)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			items = append(items, makeHealthIssue(issueType, severity, r.LibraryID, r.LibraryName, r.SeriesID, r.SeriesName, r.BookID, r.BookName, r.Path, interfaceToString(r.Detail), r.IssueCount))
		}
	case "unmatched_koreader":
		rows, err := s.Queries.ListHealthUnmatchedKOReader(ctx, int64(limit))
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			items = append(items, HealthIssue{
				Type:        issueType,
				Severity:    severity,
				LibraryID:   r.LibraryID,
				LibraryName: r.LibraryName,
				SeriesID:    nullToInt64Ptr(r.SeriesID),
				SeriesName:  r.SeriesName,
				BookID:      nullToInt64Ptr(r.BookID),
				BookName:    r.BookName,
				Path:        r.Path,
				Detail:      strings.TrimSpace(interfaceToString(r.Detail)),
				Count:       r.IssueCount,
			})
		}
	}
	return items, nil
}

func makeHealthIssue(issueType, severity string, libraryID int64, libraryName string, seriesID int64, seriesName string, bookID int64, bookName, path, detail string, count int64) HealthIssue {
	issue := HealthIssue{
		Type:        issueType,
		Severity:    severity,
		LibraryID:   libraryID,
		LibraryName: libraryName,
		SeriesName:  seriesName,
		BookName:    bookName,
		Path:        path,
		Detail:      strings.TrimSpace(detail),
		Count:       count,
	}
	if seriesID != 0 {
		issue.SeriesID = &seriesID
	}
	if bookID != 0 {
		issue.BookID = &bookID
	}
	return issue
}
