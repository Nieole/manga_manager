// 业务说明：本文件是业务实现，属于 SQLite 数据访问层，负责把漫画库、系列、阅读进度、任务和元数据状态持久化为稳定数据模型。
// 它连接 sqlc 生成查询与上层领域服务，是资料库筛选、搜索同步和关系图谱的数据基础。
// 维护时应保持 schema、查询定义、事务边界和迁移兼容，避免破坏既有用户数据。

package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (q *Queries) GetKOReaderSettings(ctx context.Context) (KOReaderSettings, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, updated_at
		FROM koreader_settings
		WHERE id = 1
	`)

	var item KOReaderSettings
	err := row.Scan(&item.ID, &item.Username, &item.SyncKey, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return KOReaderSettings{}, nil
	}
	return item, err
}

func (q *Queries) UpsertKOReaderSettings(ctx context.Context, arg UpsertKOReaderSettingsParams) (KOReaderSettings, error) {
	row := q.db.QueryRowContext(ctx, `
		INSERT INTO koreader_settings (id, username, password_hash, updated_at)
		VALUES (1, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			password_hash = CASE
				WHEN excluded.password_hash = '' THEN koreader_settings.password_hash
				ELSE excluded.password_hash
			END,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, username, password_hash, updated_at
	`, arg.Username, arg.SyncKey)

	var item KOReaderSettings
	err := row.Scan(&item.ID, &item.Username, &item.SyncKey, &item.UpdatedAt)
	return item, err
}

func (q *Queries) ListKOReaderAccounts(ctx context.Context) ([]KOReaderAccount, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT
			a.id,
			a.username,
			a.sync_key,
			a.enabled,
			a.created_at,
			a.updated_at,
			(SELECT MAX(updated_at) FROM koreader_progress p WHERE p.username = a.username) as last_used_at,
			COALESCE((
				SELECT e.message
				FROM koreader_sync_events e
				WHERE e.username = a.username
				  AND e.direction != 'system'
				  AND e.status NOT IN ('ok', 'progress_regressed')
				ORDER BY e.created_at DESC, e.id DESC
				LIMIT 1
			), '') as latest_error
		FROM koreader_accounts a
		ORDER BY LOWER(a.username) ASC, a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]KOReaderAccount, 0)
	for rows.Next() {
		var item KOReaderAccount
		if err := rows.Scan(
			&item.ID,
			&item.Username,
			&item.SyncKey,
			&item.Enabled,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.LastUsedAt,
			&item.LatestError,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (q *Queries) CreateKOReaderAccount(ctx context.Context, arg CreateKOReaderAccountParams) (KOReaderAccount, error) {
	row := q.db.QueryRowContext(ctx, `
		INSERT INTO koreader_accounts (username, sync_key, enabled, created_at, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING id, username, sync_key, enabled, created_at, updated_at
	`, strings.TrimSpace(arg.Username), strings.TrimSpace(arg.SyncKey), arg.Enabled)

	var item KOReaderAccount
	err := row.Scan(
		&item.ID,
		&item.Username,
		&item.SyncKey,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (q *Queries) GetKOReaderAccountByUsername(ctx context.Context, username string) (KOReaderAccount, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT id, username, sync_key, enabled, created_at, updated_at
		FROM koreader_accounts
		WHERE username = ?
		LIMIT 1
	`, strings.TrimSpace(username))

	var item KOReaderAccount
	err := row.Scan(
		&item.ID,
		&item.Username,
		&item.SyncKey,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (q *Queries) GetKOReaderAccountByID(ctx context.Context, id int64) (KOReaderAccount, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT id, username, sync_key, enabled, created_at, updated_at
		FROM koreader_accounts
		WHERE id = ?
		LIMIT 1
	`, id)

	var item KOReaderAccount
	err := row.Scan(
		&item.ID,
		&item.Username,
		&item.SyncKey,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (q *Queries) RotateKOReaderAccountKey(ctx context.Context, id int64, syncKey string) (KOReaderAccount, error) {
	row := q.db.QueryRowContext(ctx, `
		UPDATE koreader_accounts
		SET sync_key = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
		RETURNING id, username, sync_key, enabled, created_at, updated_at
	`, strings.TrimSpace(syncKey), id)

	var item KOReaderAccount
	err := row.Scan(
		&item.ID,
		&item.Username,
		&item.SyncKey,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (q *Queries) SetKOReaderAccountEnabled(ctx context.Context, id int64, enabled bool) (KOReaderAccount, error) {
	row := q.db.QueryRowContext(ctx, `
		UPDATE koreader_accounts
		SET enabled = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
		RETURNING id, username, sync_key, enabled, created_at, updated_at
	`, enabled, id)

	var item KOReaderAccount
	err := row.Scan(
		&item.ID,
		&item.Username,
		&item.SyncKey,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (q *Queries) GetKOReaderStats(ctx context.Context) (KOReaderStats, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT
			EXISTS(SELECT 1 FROM koreader_accounts) as configured,
			EXISTS(SELECT 1 FROM koreader_accounts WHERE sync_key != '') as has_password,
			EXISTS(
				SELECT 1
				FROM koreader_accounts
				WHERE LOWER(sync_key) GLOB '[0-9a-f]*'
				  AND LENGTH(sync_key) = 32
			) as has_valid_sync_key,
			COALESCE((SELECT username FROM koreader_accounts ORDER BY id ASC LIMIT 1), '') as username,
			(SELECT COUNT(*) FROM koreader_accounts) as account_count,
			(SELECT COUNT(*) FROM koreader_accounts WHERE enabled = TRUE) as enabled_account_count,
			(SELECT COUNT(*) FROM books) as total_books,
			(SELECT COUNT(*) FROM books WHERE COALESCE(file_hash, '') != '' AND COALESCE(path_fingerprint, '') != '' AND COALESCE(path_fingerprint_no_ext, '') != '') as hashed_books,
			(SELECT COUNT(*) FROM koreader_progress WHERE book_id IS NULL) as unmatched_progress_count,
			(SELECT COUNT(*) FROM koreader_progress WHERE book_id IS NOT NULL) as matched_progress_count,
			(SELECT MAX(updated_at) FROM koreader_progress) as latest_sync_at
	`)

	var item KOReaderStats
	err := row.Scan(
		&item.Configured,
		&item.HasPassword,
		&item.HasValidSyncKey,
		&item.Username,
		&item.AccountCount,
		&item.EnabledAccountCount,
		&item.TotalBooks,
		&item.HashedBooks,
		&item.UnmatchedProgressCount,
		&item.MatchedProgressCount,
		&item.LatestSyncAt,
	)
	return item, err
}

func (q *Queries) GetLatestKOReaderFailure(ctx context.Context) (KOReaderSyncEvent, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT id, direction, username, document, book_id, status, message, created_at
		FROM koreader_sync_events
		WHERE direction != 'system'
		  AND status NOT IN ('ok', 'progress_regressed')
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`)

	var item KOReaderSyncEvent
	err := row.Scan(
		&item.ID,
		&item.Direction,
		&item.Username,
		&item.Document,
		&item.BookID,
		&item.Status,
		&item.Message,
		&item.CreatedAt,
	)
	return item, err
}

func (q *Queries) CountBooksMissingIdentity(ctx context.Context, matchMode string) (int64, error) {
	condition := `COALESCE(file_hash, '') = ''`
	if matchMode == "file_path" {
		condition = `COALESCE(path_fingerprint, '') = '' OR COALESCE(path_fingerprint_no_ext, '') = ''`
	}

	row := q.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM books
		WHERE `+condition)

	var count int64
	err := row.Scan(&count)
	return count, err
}

func (q *Queries) CountBooksMissingQuickHash(ctx context.Context) (int64, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM books
		WHERE COALESCE(quick_hash, '') = ''
	`)

	var count int64
	err := row.Scan(&count)
	return count, err
}

func (q *Queries) CountUnmatchedKOReaderProgress(ctx context.Context) (int64, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM koreader_progress
		WHERE book_id IS NULL
	`)

	var count int64
	err := row.Scan(&count)
	return count, err
}

func (q *Queries) FindBookByDocumentFingerprint(ctx context.Context, documentKey, matchMode string, pathIgnoreExtension bool) (KOReaderBookMatch, error) {
	if documentKey == "" {
		return KOReaderBookMatch{}, sql.ErrNoRows
	}

	var (
		query     string
		matchedBy string
	)
	switch matchMode {
	case "binary_hash":
		query = `
			SELECT
				b.id,
				b.path,
				b.page_count,
				COALESCE(b.file_hash, ''),
				COALESCE(b.path_fingerprint, ''),
				COALESCE(b.path_fingerprint_no_ext, ''),
				?,
				b.last_read_page
			FROM books b
			JOIN libraries l ON l.id = b.library_id
			WHERE l.koreader_sync_enabled = TRUE
			  AND b.file_hash = ?
			LIMIT 1
		`
		matchedBy = "binary_hash"
	case "file_path":
		column := "b.path_fingerprint"
		matchedBy = "file_path_exact"
		if pathIgnoreExtension {
			column = "b.path_fingerprint_no_ext"
			matchedBy = "file_path_ignore_extension"
		}
		query = `
			SELECT
				b.id,
				b.path,
				b.page_count,
				COALESCE(b.file_hash, ''),
				COALESCE(b.path_fingerprint, ''),
				COALESCE(b.path_fingerprint_no_ext, ''),
				?,
				b.last_read_page
			FROM books b
			JOIN libraries l ON l.id = b.library_id
			WHERE l.koreader_sync_enabled = TRUE
			  AND ` + column + ` = ?
			LIMIT 1
		`
	default:
		return KOReaderBookMatch{}, sql.ErrNoRows
	}

	row := q.db.QueryRowContext(ctx, query, matchedBy, documentKey)

	var (
		item         KOReaderBookMatch
		lastReadPage sql.NullInt64
	)
	err := row.Scan(
		&item.BookID,
		&item.Path,
		&item.PageCount,
		&item.FileHash,
		&item.PathFingerprint,
		&item.PathFingerprintNoExt,
		&item.MatchedBy,
		&lastReadPage,
	)
	if err != nil {
		return KOReaderBookMatch{}, err
	}
	if lastReadPage.Valid {
		item.LastReadPage = &lastReadPage.Int64
	}
	return item, nil
}

func (q *Queries) UpsertKOReaderProgress(ctx context.Context, arg UpsertKOReaderProgressParams) (KOReaderProgress, error) {
	row := q.db.QueryRowContext(ctx, `
		INSERT INTO koreader_progress (
			username, document, progress, percentage, device, device_id, book_id, matched_by, timestamp, raw_payload, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(username, document) DO UPDATE SET
			progress = excluded.progress,
			percentage = excluded.percentage,
			device = excluded.device,
			device_id = excluded.device_id,
			book_id = excluded.book_id,
			matched_by = excluded.matched_by,
			timestamp = excluded.timestamp,
			raw_payload = excluded.raw_payload,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, username, document, progress, percentage, device, device_id, book_id, matched_by, timestamp, created_at, updated_at, raw_payload
	`, arg.Username, arg.Document, arg.Progress, arg.Percentage, arg.Device, arg.DeviceID, arg.BookID, arg.MatchedBy, arg.Timestamp, arg.RawPayload)

	var item KOReaderProgress
	err := row.Scan(
		&item.ID,
		&item.Username,
		&item.Document,
		&item.Progress,
		&item.Percentage,
		&item.Device,
		&item.DeviceID,
		&item.BookID,
		&item.MatchedBy,
		&item.Timestamp,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.RawPayload,
	)
	return item, err
}

func (q *Queries) GetKOReaderProgress(ctx context.Context, username, document string) (KOReaderProgress, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT id, username, document, progress, percentage, device, device_id, book_id, matched_by, timestamp, created_at, updated_at, raw_payload
		FROM koreader_progress
		WHERE username = ? AND document = ?
	`, username, document)

	var item KOReaderProgress
	err := row.Scan(
		&item.ID,
		&item.Username,
		&item.Document,
		&item.Progress,
		&item.Percentage,
		&item.Device,
		&item.DeviceID,
		&item.BookID,
		&item.MatchedBy,
		&item.Timestamp,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.RawPayload,
	)
	return item, err
}

func (q *Queries) ListBooksMissingIdentityBatch(ctx context.Context, matchMode string, afterID int64, limit int) ([]BookIdentityCandidate, error) {
	if limit <= 0 {
		limit = 500
	}

	condition := `COALESCE(file_hash, '') = ''`
	if matchMode == "file_path" {
		condition = `COALESCE(path_fingerprint, '') = '' OR COALESCE(path_fingerprint_no_ext, '') = ''`
	}

	rows, err := q.db.QueryContext(ctx, `
		SELECT b.id, b.library_id, l.path, b.path
		FROM books b
		JOIN libraries l ON l.id = b.library_id
		WHERE b.id > ?
		  AND (`+condition+`)
		ORDER BY b.id ASC
		LIMIT ?
	`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BookIdentityCandidate, 0)
	for rows.Next() {
		var item BookIdentityCandidate
		if err := rows.Scan(&item.ID, &item.LibraryID, &item.LibraryPath, &item.Path); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (q *Queries) ListBooksMissingQuickHashBatch(ctx context.Context, afterID int64, limit int) ([]BookIdentityCandidate, error) {
	if limit <= 0 {
		limit = 500
	}

	rows, err := q.db.QueryContext(ctx, `
		SELECT b.id, b.library_id, l.path, b.path
		FROM books b
		JOIN libraries l ON l.id = b.library_id
		WHERE b.id > ?
		  AND COALESCE(b.quick_hash, '') = ''
		ORDER BY b.id ASC
		LIMIT ?
	`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BookIdentityCandidate, 0)
	for rows.Next() {
		var item BookIdentityCandidate
		if err := rows.Scan(&item.ID, &item.LibraryID, &item.LibraryPath, &item.Path); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (q *Queries) UpdateBookIdentity(ctx context.Context, arg UpdateBookIdentityParams) error {
	_, err := q.db.ExecContext(ctx, `
		UPDATE books
		SET file_hash = CASE WHEN ? = '' THEN file_hash ELSE ? END,
		    quick_hash = CASE WHEN ? = '' THEN quick_hash ELSE ? END,
		    path_fingerprint = CASE WHEN ? = '' THEN path_fingerprint ELSE ? END,
		    path_fingerprint_no_ext = CASE WHEN ? = '' THEN path_fingerprint_no_ext ELSE ? END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, arg.FileHash, arg.FileHash, arg.QuickHash, arg.QuickHash, arg.PathFingerprint, arg.PathFingerprint, arg.PathFingerprintNoExt, arg.PathFingerprintNoExt, arg.ID)
	return err
}

func (q *Queries) ListUnmatchedKOReaderProgress(ctx context.Context, limit int) ([]KOReaderProgress, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, username, document, progress, percentage, device, device_id, book_id, matched_by, timestamp, created_at, updated_at, raw_payload
		FROM koreader_progress
		WHERE book_id IS NULL
		ORDER BY updated_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]KOReaderProgress, 0)
	for rows.Next() {
		var item KOReaderProgress
		if err := rows.Scan(
			&item.ID,
			&item.Username,
			&item.Document,
			&item.Progress,
			&item.Percentage,
			&item.Device,
			&item.DeviceID,
			&item.BookID,
			&item.MatchedBy,
			&item.Timestamp,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.RawPayload,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (q *Queries) ListKOReaderDeviceDiagnostics(ctx context.Context) ([]KOReaderDeviceDiagnostic, error) {
	rows, err := q.db.QueryContext(ctx, `
		WITH devices AS (
			SELECT
				username,
				COALESCE(device, '') AS device,
				COALESCE(device_id, '') AS device_id,
				COUNT(*) AS total_records,
				SUM(CASE WHEN book_id IS NOT NULL THEN 1 ELSE 0 END) AS matched_records,
				SUM(CASE WHEN book_id IS NULL THEN 1 ELSE 0 END) AS unmatched_records,
				MAX(updated_at) AS latest_sync_at
			FROM koreader_progress
			GROUP BY username, COALESCE(device, ''), COALESCE(device_id, '')
		)
		SELECT
			d.username,
			d.device,
			d.device_id,
			d.total_records,
			d.matched_records,
			d.unmatched_records,
			d.latest_sync_at,
			COALESCE((
				SELECT p.document
				FROM koreader_progress p
				WHERE p.username = d.username
				  AND COALESCE(p.device, '') = d.device
				  AND COALESCE(p.device_id, '') = d.device_id
				ORDER BY p.updated_at DESC, p.id DESC
				LIMIT 1
			), '') AS latest_document,
			COALESCE((
				SELECT p.matched_by
				FROM koreader_progress p
				WHERE p.username = d.username
				  AND COALESCE(p.device, '') = d.device
				  AND COALESCE(p.device_id, '') = d.device_id
				ORDER BY p.updated_at DESC, p.id DESC
				LIMIT 1
			), '') AS latest_matched_by,
			COALESCE((
				SELECT e.message
				FROM koreader_sync_events e
				JOIN koreader_progress p ON p.username = e.username AND p.document = e.document
				WHERE e.status != 'ok'
				  AND e.direction != 'system'
				  AND p.username = d.username
				  AND COALESCE(p.device, '') = d.device
				  AND COALESCE(p.device_id, '') = d.device_id
				ORDER BY e.created_at DESC, e.id DESC
				LIMIT 1
			), '') AS latest_error
		FROM devices d
		ORDER BY d.latest_sync_at DESC, d.username ASC, d.device ASC, d.device_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]KOReaderDeviceDiagnostic, 0)
	for rows.Next() {
		var item KOReaderDeviceDiagnostic
		var latestSyncAt sql.NullString
		if err := rows.Scan(
			&item.Username,
			&item.Device,
			&item.DeviceID,
			&item.TotalRecords,
			&item.MatchedRecords,
			&item.UnmatchedRecords,
			&latestSyncAt,
			&item.LatestDocument,
			&item.LatestMatchedBy,
			&item.LatestError,
		); err != nil {
			return nil, err
		}
		item.LatestSyncAt = parseSQLiteNullTime(latestSyncAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (q *Queries) ListKOReaderDeviceMatchMethods(ctx context.Context) ([]KOReaderDeviceMatchMethod, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT
			username,
			COALESCE(device, '') AS device,
			COALESCE(device_id, '') AS device_id,
			COALESCE(NULLIF(matched_by, ''), 'unmatched') AS matched_by,
			COUNT(*) AS count
		FROM koreader_progress
		GROUP BY username, COALESCE(device, ''), COALESCE(device_id, ''), COALESCE(NULLIF(matched_by, ''), 'unmatched')
		ORDER BY username ASC, device ASC, device_id ASC, count DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]KOReaderDeviceMatchMethod, 0)
	for rows.Next() {
		var item KOReaderDeviceMatchMethod
		if err := rows.Scan(&item.Username, &item.Device, &item.DeviceID, &item.MatchedBy, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (q *Queries) ListKOReaderDeviceConflicts(ctx context.Context, limit int) ([]KOReaderDeviceConflict, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT
			p.id,
			'unmatched_progress' AS type,
			'warning' AS severity,
			p.username,
			COALESCE(p.device, '') AS device,
			COALESCE(p.device_id, '') AS device_id,
			p.document,
			p.book_id,
			COALESCE(p.matched_by, '') AS matched_by,
			'unmatched' AS status,
			'Progress record is not linked to a local book' AS message,
			p.percentage,
			p.updated_at
		FROM koreader_progress p
		WHERE p.book_id IS NULL
		UNION ALL
		SELECT
			e.id,
			'sync_error' AS type,
			CASE WHEN e.status LIKE 'auth_failed%' THEN 'error' ELSE 'warning' END AS severity,
			e.username,
			COALESCE(p.device, '') AS device,
			COALESCE(p.device_id, '') AS device_id,
			e.document,
			e.book_id,
			COALESCE(p.matched_by, '') AS matched_by,
			e.status,
			e.message,
			COALESCE(p.percentage, 0) AS percentage,
			e.created_at AS updated_at
		FROM koreader_sync_events e
		LEFT JOIN koreader_progress p ON p.username = e.username AND p.document = e.document
		WHERE e.direction != 'system'
		  AND e.status != 'ok'
		ORDER BY updated_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]KOReaderDeviceConflict, 0)
	for rows.Next() {
		var item KOReaderDeviceConflict
		var updatedAt sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.Type,
			&item.Severity,
			&item.Username,
			&item.Device,
			&item.DeviceID,
			&item.Document,
			&item.BookID,
			&item.MatchedBy,
			&item.Status,
			&item.Message,
			&item.Percentage,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		item.UpdatedAt = parseSQLiteNullTime(updatedAt).Time
		items = append(items, item)
	}
	return items, rows.Err()
}

func parseSQLiteNullTime(value sql.NullString) sql.NullTime {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return sql.NullTime{}
	}
	raw := strings.TrimSpace(value.String)
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return sql.NullTime{Time: parsed, Valid: true}
		}
	}
	return sql.NullTime{}
}

func (q *Queries) ListUnmatchedKOReaderProgressBatch(ctx context.Context, afterID int64, limit int) ([]KOReaderProgress, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, username, document, progress, percentage, device, device_id, book_id, matched_by, timestamp, created_at, updated_at, raw_payload
		FROM koreader_progress
		WHERE book_id IS NULL
		  AND id > ?
		ORDER BY id ASC
		LIMIT ?
	`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]KOReaderProgress, 0)
	for rows.Next() {
		var item KOReaderProgress
		if err := rows.Scan(
			&item.ID,
			&item.Username,
			&item.Document,
			&item.Progress,
			&item.Percentage,
			&item.Device,
			&item.DeviceID,
			&item.BookID,
			&item.MatchedBy,
			&item.Timestamp,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.RawPayload,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (q *Queries) DeleteKOReaderProgress(ctx context.Context, id int64) (KOReaderProgress, error) {
	row := q.db.QueryRowContext(ctx, `
		DELETE FROM koreader_progress
		WHERE id = ?
		RETURNING id, username, document, progress, percentage, device, device_id, book_id, matched_by, timestamp, created_at, updated_at, raw_payload
	`, id)

	var item KOReaderProgress
	err := row.Scan(
		&item.ID,
		&item.Username,
		&item.Document,
		&item.Progress,
		&item.Percentage,
		&item.Device,
		&item.DeviceID,
		&item.BookID,
		&item.MatchedBy,
		&item.Timestamp,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.RawPayload,
	)
	return item, err
}

func (q *Queries) LinkKOReaderProgressToBook(ctx context.Context, progressID, bookID int64, matchedBy string) error {
	_, err := q.db.ExecContext(ctx, `
		UPDATE koreader_progress
		SET book_id = ?, matched_by = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, bookID, matchedBy, progressID)
	return err
}

// koreaderSyncEventRetention 是 koreader_sync_events 表保留的最近事件条数上限。
const koreaderSyncEventRetention = 10000

func (q *Queries) CreateKOReaderSyncEvent(ctx context.Context, arg CreateKOReaderSyncEventParams) error {
	if _, err := q.db.ExecContext(ctx, `
		INSERT INTO koreader_sync_events (direction, username, document, book_id, status, message)
		VALUES (?, ?, ?, ?, ?, ?)
	`, arg.Direction, arg.Username, arg.Document, arg.BookID, arg.Status, arg.Message); err != nil {
		return err
	}
	// 保留最近 koreaderSyncEventRetention 条：推/拉/认证失败都会写事件（未认证请求也会触发），
	// 无上限会让该表随访问量无限增长。此处为最佳努力裁剪，失败不影响同步主流程。
	_, _ = q.db.ExecContext(ctx, `
		DELETE FROM koreader_sync_events
		WHERE id <= (SELECT MAX(id) FROM koreader_sync_events) - ?
	`, koreaderSyncEventRetention)
	return nil
}
