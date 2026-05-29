package database

import (
	"context"
	"database/sql"
	"strings"
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

func (s *SqlStore) attachLastTaskKeys(ctx context.Context, issues []HealthIssue) error {
	if len(issues) == 0 {
		return nil
	}
	type scopeKey struct {
		scope string
		id    int64
	}
	wanted := make(map[scopeKey]struct{})
	for _, issue := range issues {
		if issue.SeriesID != nil {
			wanted[scopeKey{"series", *issue.SeriesID}] = struct{}{}
		}
		if issue.LibraryID != 0 {
			wanted[scopeKey{"library", issue.LibraryID}] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	latest := make(map[scopeKey]string, len(wanted))
	for key := range wanted {
		taskKey, err := s.Queries.GetLastTaskKeyForScope(ctx, GetLastTaskKeyForScopeParams{
			Scope:   key.scope,
			ScopeID: sql.NullInt64{Int64: key.id, Valid: true},
		})
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		latest[key] = taskKey
	}
	for i := range issues {
		issue := &issues[i]
		if issue.SeriesID != nil {
			if k, ok := latest[scopeKey{"series", *issue.SeriesID}]; ok {
				issue.LastTaskKey = k
				continue
			}
		}
		if issue.LibraryID != 0 {
			if k, ok := latest[scopeKey{"library", issue.LibraryID}]; ok {
				issue.LastTaskKey = k
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
