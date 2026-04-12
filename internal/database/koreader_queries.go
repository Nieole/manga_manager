package database

import (
	"context"
	"database/sql"
)

func (q *Queries) GetKOReaderSettings(ctx context.Context) (KOReaderSettings, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, updated_at
		FROM koreader_settings
		WHERE id = 1
	`)

	var item KOReaderSettings
	err := row.Scan(&item.ID, &item.Username, &item.PasswordHash, &item.UpdatedAt)
	if err == sql.ErrNoRows {
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
	`, arg.Username, arg.PasswordHash)

	var item KOReaderSettings
	err := row.Scan(&item.ID, &item.Username, &item.PasswordHash, &item.UpdatedAt)
	return item, err
}

func (q *Queries) GetKOReaderStats(ctx context.Context) (KOReaderStats, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT
			EXISTS(SELECT 1 FROM koreader_settings WHERE id = 1 AND username != '') as configured,
			EXISTS(SELECT 1 FROM koreader_settings WHERE id = 1 AND password_hash != '') as has_password,
			COALESCE((SELECT username FROM koreader_settings WHERE id = 1), '') as username,
			(SELECT COUNT(*) FROM books) as total_books,
			(SELECT COUNT(*) FROM books WHERE COALESCE(file_hash, '') != '') as hashed_books,
			(SELECT COUNT(*) FROM koreader_progress WHERE book_id IS NULL) as unmatched_progress_count,
			(SELECT COUNT(*) FROM koreader_progress WHERE book_id IS NOT NULL) as matched_progress_count,
			(SELECT MAX(updated_at) FROM koreader_progress) as latest_sync_at
	`)

	var item KOReaderStats
	err := row.Scan(
		&item.Configured,
		&item.HasPassword,
		&item.Username,
		&item.TotalBooks,
		&item.HashedBooks,
		&item.UnmatchedProgressCount,
		&item.MatchedProgressCount,
		&item.LatestSyncAt,
	)
	return item, err
}

func (q *Queries) FindBookByDocumentFingerprint(ctx context.Context, document string) (KOReaderBookMatch, error) {
	if document == "" {
		return KOReaderBookMatch{}, sql.ErrNoRows
	}

	row := q.db.QueryRowContext(ctx, `
		SELECT
			b.id,
			b.path,
			b.page_count,
			COALESCE(b.file_hash, ''),
			COALESCE(b.path_fingerprint, ''),
			COALESCE(b.filename_fingerprint, ''),
			CASE
				WHEN b.file_hash = ? THEN 'binary'
				WHEN b.path_fingerprint = ? THEN 'path'
				WHEN b.filename_fingerprint = ? THEN 'filename'
				ELSE ''
			END as matched_by,
			b.last_read_page
		FROM books b
		JOIN libraries l ON l.id = b.library_id
		WHERE l.koreader_sync_enabled = TRUE
		  AND (
			b.file_hash = ?
			OR b.path_fingerprint = ?
			OR b.filename_fingerprint = ?
		  )
		ORDER BY
			CASE
				WHEN b.file_hash = ? THEN 0
				WHEN b.path_fingerprint = ? THEN 1
				WHEN b.filename_fingerprint = ? THEN 2
				ELSE 3
			END
		LIMIT 1
	`, document, document, document, document, document, document, document, document, document)

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
		&item.FilenameFingerprint,
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

func (q *Queries) ListBooksMissingIdentity(ctx context.Context, limit int) ([]BookIdentityCandidate, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT b.id, b.library_id, l.path, b.path
		FROM books b
		JOIN libraries l ON l.id = b.library_id
		WHERE COALESCE(file_hash, '') = ''
		   OR COALESCE(path_fingerprint, '') = ''
		   OR COALESCE(filename_fingerprint, '') = ''
		ORDER BY b.updated_at ASC, b.id ASC
		LIMIT ?
	`, limit)
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
		SET file_hash = ?, path_fingerprint = ?, filename_fingerprint = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, arg.FileHash, arg.PathFingerprint, arg.FilenameFingerprint, arg.ID)
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

func (q *Queries) LinkKOReaderProgressToBook(ctx context.Context, progressID, bookID int64, matchedBy string) error {
	_, err := q.db.ExecContext(ctx, `
		UPDATE koreader_progress
		SET book_id = ?, matched_by = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, bookID, matchedBy, progressID)
	return err
}

func (q *Queries) CreateKOReaderSyncEvent(ctx context.Context, arg CreateKOReaderSyncEventParams) error {
	_, err := q.db.ExecContext(ctx, `
		INSERT INTO koreader_sync_events (direction, username, document, book_id, status, message)
		VALUES (?, ?, ?, ?, ?, ?)
	`, arg.Direction, arg.Username, arg.Document, arg.BookID, arg.Status, arg.Message)
	return err
}
