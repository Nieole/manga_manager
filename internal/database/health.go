package database

import (
	"context"
	"database/sql"
	"fmt"
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

var healthIssueDefinitions = []struct {
	Type     string
	Severity string
	CountSQL string
	ListSQL  string
}{
	{
		Type:     "empty_pages",
		Severity: "error",
		CountSQL: `
			SELECT COUNT(*)
			FROM books b
			WHERE (? = 0 OR b.library_id = ?) AND b.page_count <= 0
		`,
		ListSQL: `
			SELECT l.id, l.name, s.id, s.name, b.id, b.name, b.path, 'page_count <= 0', 1
			FROM books b
			JOIN series s ON s.id = b.series_id
			JOIN libraries l ON l.id = b.library_id
			WHERE (? = 0 OR b.library_id = ?) AND b.page_count <= 0
			ORDER BY b.updated_at DESC, b.id DESC
			LIMIT ?
		`,
	},
	{
		Type:     "missing_cover",
		Severity: "warn",
		CountSQL: `
			SELECT COUNT(*)
			FROM books b
			WHERE (? = 0 OR b.library_id = ?) AND (b.cover_path IS NULL OR b.cover_path = '')
		`,
		ListSQL: `
			SELECT l.id, l.name, s.id, s.name, b.id, b.name, b.path, 'cover_path is empty', 1
			FROM books b
			JOIN series s ON s.id = b.series_id
			JOIN libraries l ON l.id = b.library_id
			WHERE (? = 0 OR b.library_id = ?) AND (b.cover_path IS NULL OR b.cover_path = '')
			ORDER BY b.updated_at DESC, b.id DESC
			LIMIT ?
		`,
	},
	{
		Type:     "missing_metadata",
		Severity: "warn",
		CountSQL: `
			SELECT COUNT(*)
			FROM series s
			WHERE (? = 0 OR s.library_id = ?)
			  AND (
				s.title IS NULL OR s.title = ''
				OR s.summary IS NULL OR s.summary = ''
				OR (
					NOT EXISTS (SELECT 1 FROM series_tags st WHERE st.series_id = s.id)
					AND NOT EXISTS (SELECT 1 FROM series_authors sa WHERE sa.series_id = s.id)
				)
			  )
		`,
		ListSQL: `
			SELECT l.id, l.name, s.id, s.name, NULL, '', s.path,
				CASE
					WHEN s.title IS NULL OR s.title = '' THEN 'missing title'
					WHEN s.summary IS NULL OR s.summary = '' THEN 'missing summary'
					ELSE 'missing tags and authors'
				END,
				1
			FROM series s
			JOIN libraries l ON l.id = s.library_id
			WHERE (? = 0 OR s.library_id = ?)
			  AND (
				s.title IS NULL OR s.title = ''
				OR s.summary IS NULL OR s.summary = ''
				OR (
					NOT EXISTS (SELECT 1 FROM series_tags st WHERE st.series_id = s.id)
					AND NOT EXISTS (SELECT 1 FROM series_authors sa WHERE sa.series_id = s.id)
				)
			  )
			ORDER BY s.updated_at DESC, s.id DESC
			LIMIT ?
		`,
	},
	{
		Type:     "duplicate_file_hash",
		Severity: "warn",
		CountSQL: `
			SELECT COALESCE(SUM(cnt), 0)
			FROM (
				SELECT COUNT(*) AS cnt
				FROM books b
				WHERE (? = 0 OR b.library_id = ?) AND b.file_hash IS NOT NULL AND b.file_hash != ''
				GROUP BY b.file_hash
				HAVING COUNT(*) > 1
			)
		`,
		ListSQL: `
			WITH duplicates AS (
				SELECT b.file_hash, COUNT(*) AS cnt
				FROM books b
				WHERE (? = 0 OR b.library_id = ?) AND b.file_hash IS NOT NULL AND b.file_hash != ''
				GROUP BY b.file_hash
				HAVING COUNT(*) > 1
			)
			SELECT l.id, l.name, s.id, s.name, b.id, b.name, b.path, 'file_hash=' || b.file_hash, d.cnt
			FROM duplicates d
			JOIN books b ON b.file_hash = d.file_hash
			JOIN series s ON s.id = b.series_id
			JOIN libraries l ON l.id = b.library_id
			WHERE (? = 0 OR b.library_id = ?)
			ORDER BY d.cnt DESC, b.file_hash, b.id
			LIMIT ?
		`,
	},
	{
		Type:     "missing_quick_hash",
		Severity: "warn",
		CountSQL: `
			SELECT COUNT(*)
			FROM books b
			WHERE (? = 0 OR b.library_id = ?)
			  AND COALESCE(b.quick_hash, '') = ''
		`,
		ListSQL: `
			SELECT l.id, l.name, s.id, s.name, b.id, b.name, b.path, 'quick_hash is empty', 1
			FROM books b
			JOIN series s ON s.id = b.series_id
			JOIN libraries l ON l.id = b.library_id
			WHERE (? = 0 OR b.library_id = ?)
			  AND COALESCE(b.quick_hash, '') = ''
			ORDER BY b.updated_at DESC, b.id DESC
			LIMIT ?
		`,
	},
	{
		Type:     "duplicate_quick_hash",
		Severity: "warn",
		CountSQL: `
			SELECT COALESCE(SUM(cnt), 0)
			FROM (
				SELECT COUNT(*) AS cnt
				FROM books b
				WHERE (? = 0 OR b.library_id = ?) AND b.quick_hash IS NOT NULL AND b.quick_hash != ''
				GROUP BY b.quick_hash
				HAVING COUNT(*) > 1
			)
		`,
		ListSQL: `
			WITH duplicates AS (
				SELECT b.quick_hash, COUNT(*) AS cnt
				FROM books b
				WHERE (? = 0 OR b.library_id = ?) AND b.quick_hash IS NOT NULL AND b.quick_hash != ''
				GROUP BY b.quick_hash
				HAVING COUNT(*) > 1
			)
			SELECT l.id, l.name, s.id, s.name, b.id, b.name, b.path, 'quick_hash=' || b.quick_hash, d.cnt
			FROM duplicates d
			JOIN books b ON b.quick_hash = d.quick_hash
			JOIN series s ON s.id = b.series_id
			JOIN libraries l ON l.id = b.library_id
			WHERE (? = 0 OR b.library_id = ?)
			ORDER BY d.cnt DESC, b.quick_hash, b.id
			LIMIT ?
		`,
	},
	{
		Type:     "unmatched_koreader",
		Severity: "info",
		CountSQL: `
			SELECT COUNT(*)
			FROM koreader_progress kp
			WHERE kp.book_id IS NULL
		`,
		ListSQL: `
			SELECT 0, '', NULL, '', NULL, '', kp.document, kp.username || ' / ' || kp.device, 1
			FROM koreader_progress kp
			WHERE kp.book_id IS NULL
			ORDER BY kp.updated_at DESC, kp.id DESC
			LIMIT ?
		`,
	},
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
		count, err := s.countHealthIssue(ctx, def.Type, def.CountSQL, filters.LibraryID)
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
		items, err := s.listHealthIssues(ctx, def.Type, def.Severity, def.ListSQL, filters.LibraryID, limit)
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
		var taskKey string
		err := s.db.QueryRowContext(ctx, `
			SELECT key FROM tasks WHERE scope = ? AND scope_id = ?
			ORDER BY updated_at DESC LIMIT 1
		`, key.scope, key.id).Scan(&taskKey)
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

func (s *SqlStore) countHealthIssue(ctx context.Context, issueType, query string, libraryID int64) (int64, error) {
	var count int64
	var err error
	if issueType == "unmatched_koreader" {
		err = s.db.QueryRowContext(ctx, query).Scan(&count)
	} else {
		err = s.db.QueryRowContext(ctx, query, libraryID, libraryID).Scan(&count)
	}
	return count, err
}

func (s *SqlStore) listHealthIssues(ctx context.Context, issueType, severity, query string, libraryID int64, limit int) ([]HealthIssue, error) {
	var rows *sql.Rows
	var err error
	switch issueType {
	case "unmatched_koreader":
		rows, err = s.db.QueryContext(ctx, query, limit)
	case "duplicate_file_hash", "duplicate_quick_hash":
		rows, err = s.db.QueryContext(ctx, query, libraryID, libraryID, libraryID, libraryID, limit)
	default:
		rows, err = s.db.QueryContext(ctx, query, libraryID, libraryID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]HealthIssue, 0)
	for rows.Next() {
		var item HealthIssue
		item.Type = issueType
		item.Severity = severity
		var seriesID sql.NullInt64
		var bookID sql.NullInt64
		if err := rows.Scan(
			&item.LibraryID,
			&item.LibraryName,
			&seriesID,
			&item.SeriesName,
			&bookID,
			&item.BookName,
			&item.Path,
			&item.Detail,
			&item.Count,
		); err != nil {
			return nil, err
		}
		if seriesID.Valid {
			item.SeriesID = &seriesID.Int64
		}
		if bookID.Valid {
			item.BookID = &bookID.Int64
		}
		item.Detail = strings.TrimSpace(item.Detail)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan health issues: %w", err)
	}
	return items, nil
}
