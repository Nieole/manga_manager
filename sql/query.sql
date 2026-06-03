-- name: CreateLibrary :one
INSERT INTO libraries (name, path, scan_mode, koreader_sync_enabled, scan_interval, scan_formats)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetLibrary :one
SELECT * FROM libraries WHERE id = ? LIMIT 1;

-- name: UpdateLibrary :one
UPDATE libraries
SET name = ?, path = ?, scan_mode = ?, koreader_sync_enabled = ?, scan_interval = ?, scan_formats = ?
WHERE id = ?
RETURNING *;

-- name: ListLibraries :many
SELECT * FROM libraries ORDER BY name;

-- name: DeleteLibrary :exec
DELETE FROM libraries WHERE id = ?;

-- name: CreateSeries :one
INSERT INTO series (
    library_id, name, path, title, summary, publisher, status, rating, language, locked_fields, name_initial
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetSeries :one
SELECT * FROM series WHERE id = ? LIMIT 1;

-- name: ListSeriesByLibrary :many
SELECT
       s.id,
       s.library_id,
       s.name,
       s.title,
       s.summary,
       s.publisher,
       s.status,
       s.rating,
       s.language,
       s.locked_fields,
       s.name_initial,
       s.path,
       s.created_at,
       s.updated_at,
       s.is_favorite,
       s.volume_count,
       s.book_count,
       s.total_pages,
       (SELECT b.cover_path 
        FROM books b 
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name 
        LIMIT 1) as cover_path 
FROM series s 
WHERE s.library_id = ? 
ORDER BY s.name;

-- name: CountOPDSSeriesSearch :one
SELECT COUNT(*)
FROM series s
WHERE instr(lower(s.name), lower(sqlc.arg(query))) > 0
   OR instr(lower(COALESCE(s.title, '')), lower(sqlc.arg(query))) > 0;

-- name: SearchOPDSSeries :many
SELECT
    s.id,
    s.name,
    COALESCE(s.title, '') as title,
    COALESCE(s.summary, '') as summary,
    s.updated_at,
    CAST(COALESCE(ss.cover_path, '') AS TEXT) as cover_path
FROM series s
LEFT JOIN series_stats ss ON ss.series_id = s.id
WHERE instr(lower(s.name), lower(sqlc.arg(query))) > 0
   OR instr(lower(COALESCE(s.title, '')), lower(sqlc.arg(query))) > 0
ORDER BY COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountRecentAddedSeries :one
SELECT COUNT(*)
FROM series s
WHERE CAST(sqlc.arg(library_id) AS INTEGER) = 0
   OR s.library_id = CAST(sqlc.arg(library_id) AS INTEGER);

-- name: ListRecentAddedSeries :many
SELECT
    s.id,
    s.library_id,
    s.name,
    COALESCE(s.title, '') as title,
    COALESCE(s.summary, '') as summary,
    COALESCE(s.status, '') as status,
    s.created_at,
    s.updated_at,
    s.book_count,
    s.total_pages,
    CAST(COALESCE(ss.cover_path, '') AS TEXT) as cover_path,
    CAST(COALESCE(ss.cover_book_id, 0) AS INTEGER) as cover_book_id
FROM series s
LEFT JOIN series_stats ss ON ss.series_id = s.id
WHERE CAST(sqlc.arg(library_id) AS INTEGER) = 0
   OR s.library_id = CAST(sqlc.arg(library_id) AS INTEGER)
ORDER BY s.created_at DESC, s.updated_at DESC, COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountMihonSeries :one
SELECT COUNT(*)
FROM series s
WHERE (CAST(sqlc.arg(library_id) AS INTEGER) = 0 OR s.library_id = CAST(sqlc.arg(library_id) AS INTEGER))
  AND (
    CAST(sqlc.arg(query) AS TEXT) = ''
    OR instr(lower(s.name), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
    OR instr(lower(COALESCE(s.title, '')), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
  );

-- name: ListMihonSeries :many
SELECT
    s.id,
    s.library_id,
    s.name,
    COALESCE(s.title, '') as title,
    COALESCE(s.summary, '') as summary,
    COALESCE(s.status, '') as status,
    s.updated_at,
    s.book_count,
    s.total_pages,
    CAST(COALESCE((
        SELECT b.id
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), 0) AS INTEGER) as cover_book_id
FROM series s
WHERE (CAST(sqlc.arg(library_id) AS INTEGER) = 0 OR s.library_id = CAST(sqlc.arg(library_id) AS INTEGER))
  AND (
    CAST(sqlc.arg(query) AS TEXT) = ''
    OR instr(lower(s.name), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
    OR instr(lower(COALESCE(s.title, '')), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
  )
ORDER BY COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: ListMihonSeriesByUpdated :many
SELECT
    s.id,
    s.library_id,
    s.name,
    COALESCE(s.title, '') as title,
    COALESCE(s.summary, '') as summary,
    COALESCE(s.status, '') as status,
    s.updated_at,
    s.book_count,
    s.total_pages,
    CAST(COALESCE((
        SELECT b.id
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), 0) AS INTEGER) as cover_book_id
FROM series s
WHERE (CAST(sqlc.arg(library_id) AS INTEGER) = 0 OR s.library_id = CAST(sqlc.arg(library_id) AS INTEGER))
  AND (
    CAST(sqlc.arg(query) AS TEXT) = ''
    OR instr(lower(s.name), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
    OR instr(lower(COALESCE(s.title, '')), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
  )
ORDER BY s.updated_at DESC, COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: ListMihonSeriesByBooks :many
SELECT
    s.id,
    s.library_id,
    s.name,
    COALESCE(s.title, '') as title,
    COALESCE(s.summary, '') as summary,
    COALESCE(s.status, '') as status,
    s.updated_at,
    s.book_count,
    s.total_pages,
    CAST(COALESCE((
        SELECT b.id
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), 0) AS INTEGER) as cover_book_id
FROM series s
WHERE (CAST(sqlc.arg(library_id) AS INTEGER) = 0 OR s.library_id = CAST(sqlc.arg(library_id) AS INTEGER))
  AND (
    CAST(sqlc.arg(query) AS TEXT) = ''
    OR instr(lower(s.name), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
    OR instr(lower(COALESCE(s.title, '')), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
  )
ORDER BY s.book_count DESC, COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: GetMihonSeries :one
SELECT
    s.id,
    s.library_id,
    s.name,
    COALESCE(s.title, '') as title,
    COALESCE(s.summary, '') as summary,
    COALESCE(s.status, '') as status,
    s.updated_at,
    s.book_count,
    s.total_pages,
    CAST(COALESCE((
        SELECT b.id
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), 0) AS INTEGER) as cover_book_id
FROM series s
WHERE s.id = ?
LIMIT 1;

-- name: CreateBook :one
INSERT INTO books (
    series_id, library_id, name, path, size, file_modified_at, 
    volume, title, summary, number, sort_number, page_count, cover_path
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetBook :one
SELECT * FROM books WHERE id = ? LIMIT 1;

-- name: GetBookByPath :one
SELECT * FROM books WHERE path = ? LIMIT 1;

-- name: ListBooksBySeries :many
SELECT * FROM books WHERE series_id = ? ORDER BY volume, sort_number, name;



-- name: ListBooksByLibrary :many
SELECT id, path, file_modified_at, size, cover_path FROM books WHERE library_id = ?;

-- name: DeleteBookByPath :exec
DELETE FROM books WHERE path = ?;



-- name: UpsertBookByPath :one
INSERT INTO books (
    series_id, library_id, name, path, size, file_modified_at, 
    volume, title, summary, number, sort_number, page_count, cover_path
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(path) DO UPDATE SET
    series_id = excluded.series_id,
    library_id = excluded.library_id,
    name = excluded.name,
    size = excluded.size,
    file_modified_at = excluded.file_modified_at,
    volume = excluded.volume,
    title = excluded.title,
    summary = excluded.summary,
    number = excluded.number,
    sort_number = excluded.sort_number,
    page_count = excluded.page_count,
    cover_path = excluded.cover_path,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: UpdateBookProgress :exec
UPDATE books 
SET last_read_page = ?, last_read_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpsertSeriesByPath :one
INSERT INTO series (
    library_id, name, path, title, summary, publisher, status, rating, language, locked_fields, is_favorite,
    volume_count, book_count, total_pages, name_initial
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(path) DO UPDATE SET
    library_id = excluded.library_id,
    name = excluded.name,
    name_initial = excluded.name_initial,
    title = excluded.title,
    summary = excluded.summary,
    publisher = excluded.publisher,
    status = excluded.status,
    rating = excluded.rating,
    language = excluded.language,
    locked_fields = excluded.locked_fields,
    volume_count = excluded.volume_count,
    book_count = excluded.book_count,
    total_pages = excluded.total_pages,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: UpdateSeriesMetadata :one
UPDATE series
SET 
    title = ?,
    summary = ?,
    publisher = ?,
    status = ?,
    rating = ?,
    language = ?,
    locked_fields = ?,
    name_initial = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: CreateMetadataReview :one
INSERT INTO metadata_reviews (
    series_id, provider, source_url, source_id, source_query, summary, confidence, status, raw_payload
) VALUES (
    sqlc.arg(series_id), sqlc.arg(provider), sqlc.arg(source_url), sqlc.arg(source_id), sqlc.arg(source_query), sqlc.arg(summary), sqlc.arg(confidence), sqlc.arg(status), sqlc.arg(raw_payload)
)
RETURNING *;

-- name: GetMetadataReview :one
SELECT * FROM metadata_reviews WHERE id = ? LIMIT 1;

-- name: ListMetadataReviewsBySeries :many
SELECT * FROM metadata_reviews WHERE series_id = ? ORDER BY created_at DESC;

-- name: ListPendingMetadataReviewsBySeries :many
SELECT * FROM metadata_reviews WHERE series_id = ? AND status = 'pending' ORDER BY confidence DESC, created_at DESC;

-- name: CountPendingMetadataReviewInbox :one
SELECT COUNT(*)
FROM metadata_reviews mr
JOIN series s ON s.id = mr.series_id
JOIN libraries l ON l.id = s.library_id
WHERE mr.status = 'pending'
  AND (CAST(sqlc.arg(library_id) AS INTEGER) = 0 OR s.library_id = CAST(sqlc.arg(library_id) AS INTEGER))
  AND (CAST(sqlc.arg(provider) AS TEXT) = '' OR lower(mr.provider) = lower(CAST(sqlc.arg(provider) AS TEXT)))
  AND (
    CAST(sqlc.arg(query) AS TEXT) = ''
    OR instr(lower(s.name), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
    OR instr(lower(COALESCE(s.title, '')), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
    OR instr(lower(mr.source_query), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
  );

-- name: ListPendingMetadataReviewInbox :many
SELECT
    mr.id,
    mr.series_id,
    mr.provider,
    mr.source_url,
    mr.source_id,
    mr.source_query,
    mr.summary,
    mr.confidence,
    mr.status,
    mr.raw_payload,
    mr.created_at,
    mr.updated_at,
    mr.applied_at,
    mr.rejected_at,
    s.library_id,
    l.name as library_name,
    s.name as series_name,
    COALESCE(s.title, '') as series_title,
    CAST(COALESCE((
        SELECT b.id
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), 0) AS INTEGER) as cover_book_id,
    CAST((SELECT COUNT(*) FROM metadata_review_fields f WHERE f.review_id = mr.id) AS INTEGER) as field_count,
    CAST((SELECT COUNT(*) FROM metadata_review_fields f WHERE f.review_id = mr.id AND f.locked = TRUE) AS INTEGER) as locked_field_count
FROM metadata_reviews mr
JOIN series s ON s.id = mr.series_id
JOIN libraries l ON l.id = s.library_id
WHERE mr.status = 'pending'
  AND (CAST(sqlc.arg(library_id) AS INTEGER) = 0 OR s.library_id = CAST(sqlc.arg(library_id) AS INTEGER))
  AND (CAST(sqlc.arg(provider) AS TEXT) = '' OR lower(mr.provider) = lower(CAST(sqlc.arg(provider) AS TEXT)))
  AND (
    CAST(sqlc.arg(query) AS TEXT) = ''
    OR instr(lower(s.name), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
    OR instr(lower(COALESCE(s.title, '')), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
    OR instr(lower(mr.source_query), lower(CAST(sqlc.arg(query) AS TEXT))) > 0
  )
ORDER BY mr.confidence ASC, mr.created_at ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: UpdateMetadataReviewStatus :one
UPDATE metadata_reviews
SET status = sqlc.arg(status),
    updated_at = CURRENT_TIMESTAMP,
    applied_at = CASE WHEN sqlc.arg(status) = 'applied' THEN CURRENT_TIMESTAMP ELSE applied_at END,
    rejected_at = CASE WHEN sqlc.arg(status) = 'rejected' THEN CURRENT_TIMESTAMP ELSE rejected_at END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateMetadataReviewField :one
INSERT INTO metadata_review_fields (
    review_id, field_name, current_value, proposed_value, confidence, source, source_url, locked, status
) VALUES (
    sqlc.arg(review_id), sqlc.arg(field_name), sqlc.arg(current_value), sqlc.arg(proposed_value), sqlc.arg(confidence), sqlc.arg(source), sqlc.arg(source_url), sqlc.arg(locked), sqlc.arg(status)
)
RETURNING *;

-- name: ListMetadataReviewFields :many
SELECT * FROM metadata_review_fields WHERE review_id = ? ORDER BY id ASC;

-- name: UpsertSeriesMetadataProvenance :one
INSERT INTO series_metadata_provenance (
    series_id, field_name, value, source, source_url, confidence, review_id
) VALUES (
    sqlc.arg(series_id), sqlc.arg(field_name), sqlc.arg(value), sqlc.arg(source), sqlc.arg(source_url), sqlc.arg(confidence), sqlc.arg(review_id)
)
ON CONFLICT(series_id, field_name) DO UPDATE SET
    value = excluded.value,
    source = excluded.source,
    source_url = excluded.source_url,
    confidence = excluded.confidence,
    review_id = excluded.review_id,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: GetSeriesMetadataProvenance :many
SELECT * FROM series_metadata_provenance WHERE series_id = ? ORDER BY field_name;

-- name: ListSeriesInitialBackfillCandidates :many
SELECT id, name, title, name_initial FROM series;

-- name: UpdateSeriesInitial :exec
UPDATE series SET name_initial = ? WHERE id = ?;

-- name: UpsertTag :one
INSERT INTO tags (name) VALUES (?)
ON CONFLICT(name) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: LinkSeriesTag :exec
INSERT OR IGNORE INTO series_tags (series_id, tag_id) VALUES (?, ?);

-- name: GetSeriesByLibrary :many
SELECT
    s.id,
    s.library_id,
    s.name,
    s.title,
    s.summary,
    s.publisher,
    s.status,
    s.rating,
    s.language,
    s.locked_fields,
    s.name_initial,
    s.path,
    s.created_at,
    s.updated_at,
    s.is_favorite,
    s.volume_count,
    s.book_count,
    s.total_pages,
    GROUP_CONCAT(DISTINCT t.name) as tags_string,
    (SELECT b.cover_path FROM books b WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != '' ORDER BY b.sort_number, b.name LIMIT 1) as cover_path,
    (SELECT COUNT(DISTINCT CASE WHEN b.last_read_page > 0 THEN b.id END) FROM books b WHERE b.series_id = s.id) as read_count
FROM series s
LEFT JOIN series_tags st ON s.id = st.series_id
LEFT JOIN tags t ON st.tag_id = t.id
WHERE s.library_id = ? 
GROUP BY s.id
ORDER BY s.name;

-- name: UpdateSeriesStatistics :exec
UPDATE series
SET 
    volume_count = (SELECT COUNT(DISTINCT NULLIF(b.volume, '')) FROM books b WHERE b.series_id = ?),
    book_count = (SELECT COUNT(*) FROM books b WHERE b.series_id = ?),
    total_pages = (SELECT COALESCE(SUM(page_count), 0) FROM books b WHERE b.series_id = ?),
    updated_at = CURRENT_TIMESTAMP
WHERE series.id = ?;

-- name: RefreshSeriesStats :exec
INSERT INTO series_stats (
    series_id,
    cover_path,
    cover_book_id,
    read_pages,
    read_book_count,
    completed_book_count,
    last_read_at,
    last_read_book_id,
    tag_names_cache,
    author_names_cache,
    updated_at
)
SELECT
    s.id,
    COALESCE((
        SELECT b.cover_path
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), '') AS cover_path,
    COALESCE((
        SELECT b.id
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), 0) AS cover_book_id,
    COALESCE((
        SELECT SUM(
            CASE
                WHEN b.last_read_page IS NULL OR b.last_read_page <= 0 THEN 0
                WHEN b.page_count > 0 AND b.last_read_page > b.page_count THEN b.page_count
                ELSE b.last_read_page
            END
        )
        FROM books b
        WHERE b.series_id = s.id
    ), 0) AS read_pages,
    COALESCE((
        SELECT COUNT(*)
        FROM books b
        WHERE b.series_id = s.id AND b.last_read_page IS NOT NULL AND b.last_read_page > 0
    ), 0) AS read_book_count,
    COALESCE((
        SELECT COUNT(*)
        FROM books b
        WHERE b.series_id = s.id AND b.page_count > 0 AND b.last_read_page >= b.page_count
    ), 0) AS completed_book_count,
    (
        SELECT b.last_read_at
        FROM books b
        WHERE b.series_id = s.id AND b.last_read_at IS NOT NULL
        ORDER BY b.last_read_at DESC, b.id DESC
        LIMIT 1
    ) AS last_read_at,
    COALESCE((
        SELECT b.id
        FROM books b
        WHERE b.series_id = s.id AND b.last_read_at IS NOT NULL
        ORDER BY b.last_read_at DESC, b.id DESC
        LIMIT 1
    ), 0) AS last_read_book_id,
    COALESCE((
        SELECT GROUP_CONCAT(name)
        FROM (
            SELECT DISTINCT t.name AS name
            FROM tags t
            JOIN series_tags st ON st.tag_id = t.id
            WHERE st.series_id = s.id
            ORDER BY t.name
        )
    ), '') AS tag_names_cache,
    COALESCE((
        SELECT GROUP_CONCAT(name)
        FROM (
            SELECT DISTINCT a.name AS name
            FROM authors a
            JOIN series_authors sa ON sa.author_id = a.id
            WHERE sa.series_id = s.id
            ORDER BY a.name
        )
    ), '') AS author_names_cache,
    CURRENT_TIMESTAMP
FROM series s
WHERE s.id = ?
ON CONFLICT(series_id) DO UPDATE SET
    cover_path = excluded.cover_path,
    cover_book_id = excluded.cover_book_id,
    read_pages = excluded.read_pages,
    read_book_count = excluded.read_book_count,
    completed_book_count = excluded.completed_book_count,
    last_read_at = excluded.last_read_at,
    last_read_book_id = excluded.last_read_book_id,
    tag_names_cache = excluded.tag_names_cache,
    author_names_cache = excluded.author_names_cache,
    updated_at = CURRENT_TIMESTAMP;

-- name: GetTagsForSeries :many
SELECT t.* FROM tags t
JOIN series_tags st ON t.id = st.tag_id
WHERE st.series_id = ? ORDER BY t.name;

-- name: UpsertAuthor :one
INSERT INTO authors (name, role) VALUES (?, ?)
ON CONFLICT(name, role) DO UPDATE SET name = excluded.name, role=excluded.role
RETURNING *;

-- name: LinkSeriesAuthor :exec
INSERT OR IGNORE INTO series_authors (series_id, author_id) VALUES (?, ?);

-- name: GetAuthorsForSeries :many
SELECT a.* FROM authors a
JOIN series_authors sa ON a.id = sa.author_id
WHERE sa.series_id = ? ORDER BY a.role, a.name;

-- name: ClearSeriesTags :exec
DELETE FROM series_tags WHERE series_id = ?;

-- name: ClearSeriesAuthors :exec
DELETE FROM series_authors WHERE series_id = ?;

-- name: GetNextBookInSeries :one
SELECT nb.* FROM books nb
INNER JOIN books cb ON cb.id = ? AND nb.series_id = cb.series_id
WHERE 
   (nb.volume > cb.volume)
   OR (nb.volume = cb.volume AND nb.sort_number > cb.sort_number)
   OR (nb.volume = cb.volume AND nb.sort_number = cb.sort_number AND nb.name > cb.name)
ORDER BY nb.volume ASC, nb.sort_number ASC, nb.name ASC
LIMIT 1;

-- name: GetAllTags :many
SELECT * FROM tags ORDER BY name;

-- name: GetAllAuthors :many
SELECT * FROM authors ORDER BY name;

-- name: LinkSeriesLink :one
INSERT INTO series_links (series_id, name, url) VALUES (?, ?, ?)
RETURNING *;

-- name: ClearSeriesLinks :exec
DELETE FROM series_links WHERE series_id = ?;

-- name: GetLinksForSeries :many
SELECT * FROM series_links WHERE series_id = ? ORDER BY id ASC;

-- name: GetRecentReadSeries :many
WITH RankedBooks AS (
    SELECT 
        b.series_id,
        b.id AS book_id,
        b.last_read_at,
        b.last_read_page,
        ROW_NUMBER() OVER(PARTITION BY b.series_id ORDER BY b.last_read_at DESC) as rn
    FROM books b
    WHERE b.last_read_at IS NOT NULL AND b.library_id = ?
)
SELECT 
    s.*,
    rb.book_id AS recent_book_id,
    rb.last_read_at,
    rb.last_read_page,
    (SELECT b.cover_path FROM books b WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != '' ORDER BY b.sort_number, b.name LIMIT 1) as cover_path
FROM series s
JOIN RankedBooks rb ON s.id = rb.series_id AND rb.rn = 1
WHERE s.library_id = ?
ORDER BY rb.last_read_at DESC
LIMIT ?;

-- name: UpdateSeriesFavorite :exec
UPDATE series SET is_favorite = ? WHERE id = ?;

-- name: DeleteSeries :exec
DELETE FROM series WHERE id = ?;

-- name: DeleteBook :exec
DELETE FROM books WHERE id = ?;

-- name: GetTopReadingTags :many
SELECT t.name, COUNT(*) as tag_count
FROM tags t
JOIN series_tags st ON t.id = st.tag_id
JOIN books b ON st.series_id = b.series_id
JOIN reading_activity ra ON b.id = ra.book_id
GROUP BY t.id
ORDER BY tag_count DESC
LIMIT ?;

-- name: GetCandidateSeriesForAI :many
SELECT s.id, s.title, s.name, s.summary, 
       (SELECT b.cover_path FROM books b WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != '' ORDER BY b.sort_number, b.name LIMIT 1) as cover_path
FROM series s
WHERE s.summary IS NOT NULL AND s.summary != '' 
  AND (s.total_pages = 0 OR (CAST(s.book_count AS REAL) > 0 AND (SELECT COUNT(*) FROM books b WHERE b.series_id = s.id AND b.last_read_page > 0) < s.book_count * 0.5))
ORDER BY RANDOM()
LIMIT ?;

-- name: GetSeriesWithoutCollection :many
SELECT s.id, s.title, s.name, s.summary
FROM series s
LEFT JOIN collection_series cs ON s.id = cs.series_id
WHERE s.library_id = ? AND cs.collection_id IS NULL;

-- name: CreateAIGroupingReview :one
INSERT INTO ai_grouping_reviews (
    library_id, provider, status, summary, raw_payload, candidate_count, collection_count
) VALUES (
    sqlc.arg(library_id), sqlc.arg(provider), sqlc.arg(status), sqlc.arg(summary), sqlc.arg(raw_payload), sqlc.arg(candidate_count), sqlc.arg(collection_count)
)
RETURNING *;

-- name: CreateAIGroupingReviewCollection :one
INSERT INTO ai_grouping_review_collections (
    review_id, name, description, series_ids, series_count, status
) VALUES (
    sqlc.arg(review_id), sqlc.arg(name), sqlc.arg(description), sqlc.arg(series_ids), sqlc.arg(series_count), sqlc.arg(status)
)
RETURNING *;

-- name: ListAIGroupingReviews :many
SELECT
    agr.id,
    agr.library_id,
    l.name as library_name,
    agr.provider,
    agr.status,
    agr.summary,
    agr.raw_payload,
    agr.candidate_count,
    agr.collection_count,
    agr.created_at,
    agr.updated_at,
    agr.applied_at,
    agr.rejected_at
FROM ai_grouping_reviews agr
JOIN libraries l ON l.id = agr.library_id
WHERE (CAST(sqlc.arg(library_id) AS INTEGER) = 0 OR agr.library_id = CAST(sqlc.arg(library_id) AS INTEGER))
  AND (CAST(sqlc.arg(status) AS TEXT) = '' OR agr.status = CAST(sqlc.arg(status) AS TEXT))
ORDER BY agr.created_at DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountAIGroupingReviews :one
SELECT COUNT(*)
FROM ai_grouping_reviews agr
WHERE (CAST(sqlc.arg(library_id) AS INTEGER) = 0 OR agr.library_id = CAST(sqlc.arg(library_id) AS INTEGER))
  AND (CAST(sqlc.arg(status) AS TEXT) = '' OR agr.status = CAST(sqlc.arg(status) AS TEXT));

-- name: GetAIGroupingReview :one
SELECT * FROM ai_grouping_reviews WHERE id = ? LIMIT 1;

-- name: ListAIGroupingReviewCollections :many
SELECT * FROM ai_grouping_review_collections WHERE review_id = ? ORDER BY id ASC;

-- name: GetAIGroupingReviewCollection :one
SELECT * FROM ai_grouping_review_collections WHERE id = ? LIMIT 1;

-- name: UpdateAIGroupingReviewCollection :one
UPDATE ai_grouping_review_collections
SET name = sqlc.arg(name),
    description = sqlc.arg(description),
    series_ids = sqlc.arg(series_ids),
    series_count = sqlc.arg(series_count),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'pending'
RETURNING *;

-- name: UpdateAIGroupingReviewStatus :one
UPDATE ai_grouping_reviews
SET status = sqlc.arg(status),
    updated_at = CURRENT_TIMESTAMP,
    applied_at = CASE WHEN sqlc.arg(status) = 'applied' THEN CURRENT_TIMESTAMP ELSE applied_at END,
    rejected_at = CASE WHEN sqlc.arg(status) = 'rejected' THEN CURRENT_TIMESTAMP ELSE rejected_at END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkAIGroupingReviewCollectionApplied :exec
UPDATE ai_grouping_review_collections
SET status = 'applied',
    created_collection_id = sqlc.arg(created_collection_id),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id);

-- name: MarkAIGroupingReviewCollectionRejected :exec
UPDATE ai_grouping_review_collections
SET status = 'rejected',
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'pending';

-- name: MarkAIGroupingReviewCollectionsRejected :exec
UPDATE ai_grouping_review_collections
SET status = 'rejected',
    updated_at = CURRENT_TIMESTAMP
WHERE review_id = sqlc.arg(review_id)
  AND status = 'pending';

-- name: CountPendingAIGroupingReviewCollections :one
SELECT COUNT(*)
FROM ai_grouping_review_collections
WHERE review_id = ?
  AND status = 'pending';

-- name: CountAppliedAIGroupingReviewCollections :one
SELECT COUNT(*)
FROM ai_grouping_review_collections
WHERE review_id = ?
  AND status = 'applied';

-- name: GetSeriesNamesByIDs :many
SELECT id, name, COALESCE(title, '') as title
FROM series
WHERE id IN (sqlc.slice(ids))
ORDER BY COALESCE(NULLIF(title, ''), name) COLLATE NOCASE;

-- name: CreateCollection :one
INSERT INTO collections (name, description, source_type, source_review_id)
VALUES (
    sqlc.arg(name),
    sqlc.arg(description),
    COALESCE(NULLIF(CAST(sqlc.arg(source_type) AS TEXT), ''), 'manual'),
    sqlc.arg(source_review_id)
)
RETURNING *;

-- name: AddSeriesToCollection :exec
INSERT OR IGNORE INTO collection_series (collection_id, series_id)
VALUES (?, ?);

-- name: TouchCollection :exec
UPDATE collections
SET updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: ListReadingLists :many
SELECT
    rl.id,
    rl.name,
    rl.description,
    rl.sort_order,
    rl.created_at,
    rl.updated_at,
    COUNT(rli.id) as item_count
FROM reading_lists rl
LEFT JOIN reading_list_items rli ON rli.reading_list_id = rl.id
GROUP BY rl.id
ORDER BY rl.sort_order, rl.name;

-- name: CreateReadingList :one
INSERT INTO reading_lists (name, description)
VALUES (?, ?)
RETURNING *;

-- name: GetReadingList :one
SELECT * FROM reading_lists WHERE id = ? LIMIT 1;

-- name: UpdateReadingList :one
UPDATE reading_lists
SET name = ?, description = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: DeleteReadingList :exec
DELETE FROM reading_lists WHERE id = ?;

-- name: ListReadingListItems :many
SELECT
    rli.id,
    rli.reading_list_id,
    rli.series_id,
    rli.sort_order,
    rli.note,
    rli.created_at,
    rli.updated_at,
    s.name as series_name,
    COALESCE(s.title, '') as series_title,
    s.book_count,
    CAST(COALESCE((
        SELECT b.cover_path
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), '') AS TEXT) as cover_path,
    CAST(COALESCE((
        SELECT b.id
        FROM books b
        WHERE b.series_id = s.id
        ORDER BY
            CASE
                WHEN b.page_count = 0 THEN 0
                WHEN b.last_read_page IS NULL THEN 0
                WHEN b.last_read_page < b.page_count THEN 0
                ELSE 1
            END,
            b.sort_number,
            b.name
        LIMIT 1
    ), 0) AS INTEGER) as next_book_id
FROM reading_list_items rli
JOIN series s ON s.id = rli.series_id
WHERE rli.reading_list_id = ?
ORDER BY rli.sort_order, s.name;

-- name: CountReadingListSeries :one
SELECT COUNT(*)
FROM reading_list_items rli
WHERE rli.reading_list_id = ?;

-- name: ListReadingListSeriesPage :many
SELECT
    rli.id as item_id,
    rli.reading_list_id,
    rli.series_id,
    rli.sort_order,
    rli.note,
    rli.updated_at as item_updated_at,
    s.library_id,
    s.name,
    COALESCE(s.title, '') as title,
    COALESCE(s.summary, '') as summary,
    COALESCE(s.status, '') as status,
    s.created_at,
    s.updated_at,
    s.book_count,
    s.total_pages,
    CAST(COALESCE((
        SELECT b.cover_path
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), '') AS TEXT) as cover_path,
    CAST(COALESCE((
        SELECT b.id
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), 0) AS INTEGER) as cover_book_id,
    CAST(COALESCE((
        SELECT b.id
        FROM books b
        WHERE b.series_id = s.id
        ORDER BY
            CASE
                WHEN b.page_count = 0 THEN 0
                WHEN b.last_read_page IS NULL THEN 0
                WHEN b.last_read_page < b.page_count THEN 0
                ELSE 1
            END,
            b.sort_number,
            b.name
        LIMIT 1
    ), 0) AS INTEGER) as next_book_id
FROM reading_list_items rli
JOIN series s ON s.id = rli.series_id
WHERE rli.reading_list_id = sqlc.arg(reading_list_id)
ORDER BY rli.sort_order, COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: AddReadingListItem :one
INSERT INTO reading_list_items (reading_list_id, series_id, sort_order, note)
VALUES (
    sqlc.arg(reading_list_id),
    sqlc.arg(series_id),
    COALESCE((SELECT MAX(sort_order) + 10 FROM reading_list_items WHERE reading_list_id = sqlc.arg(reading_list_id)), 10),
    sqlc.arg(note)
)
ON CONFLICT(reading_list_id, series_id) DO UPDATE SET
    note = excluded.note,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: RemoveReadingListItem :exec
DELETE FROM reading_list_items WHERE reading_list_id = ? AND id = ?;

-- name: UpdateReadingListItemSortOrder :exec
UPDATE reading_list_items
SET sort_order = ?, updated_at = CURRENT_TIMESTAMP
WHERE reading_list_id = ? AND id = ?;

-- name: GetReferencedBookCoverPaths :many
SELECT DISTINCT cover_path FROM books WHERE cover_path IS NOT NULL AND cover_path != '';

-- name: GetReferencedSeriesCoverPaths :many
SELECT DISTINCT cover_path FROM series_stats WHERE cover_path IS NOT NULL AND cover_path != '';

-- name: GetBookCoverPathsByIDs :many
SELECT id, COALESCE(cover_path, '') AS cover_path
FROM books
WHERE id IN (sqlc.slice(ids));

-- name: GetSeriesCoverPathsByIDs :many
SELECT s.id, CAST(COALESCE(ss.cover_path, '') AS TEXT) AS cover_path
FROM series s
LEFT JOIN series_stats ss ON ss.series_id = s.id
WHERE s.id IN (sqlc.slice(ids));

-- name: ListCollectionsWithSeriesCount :many
SELECT
    c.id, c.name, c.description, c.cover_url, c.sort_order, c.source_type, c.source_review_id,
    c.created_at, c.updated_at,
    (SELECT COUNT(*) FROM collection_series cs WHERE cs.collection_id = c.id) AS series_count
FROM collections c
ORDER BY c.sort_order, c.name;

-- name: CreateSimpleCollection :one
INSERT INTO collections (name, description) VALUES (?, ?)
RETURNING id;

-- name: DeleteCollection :exec
DELETE FROM collections WHERE id = ?;

-- name: UpdateCollectionDetails :exec
UPDATE collections
SET name = ?, description = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: ListCollectionSeries :many
SELECT
    s.id AS series_id,
    s.name AS series_name,
    (SELECT b.cover_path FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name LIMIT 1) AS cover_path,
    s.book_count,
    cs.added_at
FROM collection_series cs
JOIN series s ON s.id = cs.series_id
WHERE cs.collection_id = ?
ORDER BY cs.sort_order, s.name;

-- name: RemoveSeriesFromCollection :exec
DELETE FROM collection_series WHERE collection_id = ? AND series_id = ?;

-- name: CollectionNameExists :one
SELECT 1 FROM collections WHERE name = ? COLLATE NOCASE LIMIT 1;

-- name: ListCollectionViews :many
SELECT
    'collection:' || c.id AS view_id,
    c.id,
    'collection' AS kind,
    c.name,
    COALESCE(c.description, '') AS description,
    CAST(NULL AS INTEGER) AS library_id,
    CAST('' AS TEXT) AS library_name,
    (SELECT COUNT(*) FROM collection_series cs WHERE cs.collection_id = c.id) AS series_count,
    c.source_type,
    c.source_review_id,
    c.sort_order,
    c.created_at,
    c.updated_at
FROM collections c
UNION ALL
SELECT
    'smart:' || sf.id AS view_id,
    sf.id,
    'smart' AS kind,
    sf.name,
    TRIM(
        COALESCE('tag=' || sf.active_tag, '') || ' ' ||
        COALESCE('author=' || sf.active_author, '') || ' ' ||
        COALESCE('status=' || sf.active_status, '') || ' ' ||
        COALESCE('letter=' || sf.active_letter, '') || ' ' ||
        COALESCE('read=' || sf.read_state, '') || ' ' ||
        COALESCE('rating>=' || sf.min_rating, '') || ' ' ||
        COALESCE('rating<=' || sf.max_rating, '') || ' ' ||
        COALESCE('progress>=' || sf.min_progress, '') || ' ' ||
        COALESCE('progress<=' || sf.max_progress, '') || ' ' ||
        COALESCE('added<=' || sf.added_within_days || 'd', '')
    ) AS description,
    sf.library_id,
    l.name AS library_name,
    (
        SELECT COUNT(DISTINCT s.id)
        FROM series s
        LEFT JOIN series_tags st ON s.id = st.series_id
        LEFT JOIN tags t ON st.tag_id = t.id
        LEFT JOIN series_authors sa ON s.id = sa.series_id
        LEFT JOIN authors a ON sa.author_id = a.id
        LEFT JOIN (
            SELECT
                series_id,
                COUNT(*) as book_count,
                SUM(CASE WHEN last_read_page IS NOT NULL AND last_read_page > 0 THEN 1 ELSE 0 END) as read_books,
                SUM(CASE WHEN page_count > 0 AND last_read_page >= page_count THEN 1 ELSE 0 END) as completed_books,
                CASE
                    WHEN SUM(CASE WHEN page_count > 0 THEN page_count ELSE 0 END) > 0
                    THEN SUM(CASE WHEN last_read_page IS NOT NULL AND last_read_page > 0 THEN MIN(last_read_page, page_count) ELSE 0 END) * 100.0 / SUM(CASE WHEN page_count > 0 THEN page_count ELSE 0 END)
                    ELSE 0
                END as progress_percent
            FROM books
            GROUP BY series_id
        ) rp ON rp.series_id = s.id
        WHERE s.library_id = sf.library_id
          AND (sf.active_status IS NULL OR s.status = sf.active_status)
          AND (sf.active_letter IS NULL OR s.name_initial = sf.active_letter)
          AND (sf.active_tag IS NULL OR t.name = sf.active_tag)
          AND (sf.active_author IS NULL OR a.name = sf.active_author)
          AND (sf.min_rating IS NULL OR s.rating >= sf.min_rating)
          AND (sf.max_rating IS NULL OR s.rating <= sf.max_rating)
          AND (sf.min_progress IS NULL OR COALESCE(rp.progress_percent, 0) >= sf.min_progress)
          AND (sf.max_progress IS NULL OR COALESCE(rp.progress_percent, 0) <= sf.max_progress)
          AND (sf.added_within_days IS NULL OR s.created_at >= datetime('now', '-' || sf.added_within_days || ' days'))
          AND (
            sf.read_state IS NULL
            OR (sf.read_state = 'unread' AND COALESCE(rp.read_books, 0) = 0)
            OR (sf.read_state = 'reading' AND COALESCE(rp.read_books, 0) > 0 AND COALESCE(rp.completed_books, 0) < COALESCE(rp.book_count, 0))
            OR (sf.read_state = 'completed' AND COALESCE(rp.book_count, 0) > 0 AND COALESCE(rp.completed_books, 0) = COALESCE(rp.book_count, 0))
          )
    ) AS series_count,
    'smart_filter' AS source_type,
    CAST(NULL AS INTEGER) AS source_review_id,
    CAST(0 AS INTEGER) AS sort_order,
    sf.created_at,
    sf.updated_at
FROM smart_filters sf
JOIN libraries l ON l.id = sf.library_id
ORDER BY kind, sort_order, name;

-- name: GetStaticCollectionView :one
SELECT
    'collection:' || c.id AS view_id,
    c.id,
    'collection' AS kind,
    c.name,
    c.description,
    (SELECT COUNT(*) FROM collection_series cs WHERE cs.collection_id = c.id) AS series_count,
    c.source_type,
    c.source_review_id,
    c.sort_order,
    c.created_at,
    c.updated_at
FROM collections c
WHERE c.id = ?
LIMIT 1;

-- name: ListStaticCollectionSeriesPaged :many
SELECT
    s.id,
    s.library_id,
    s.name,
    COALESCE(s.title, '') AS title,
    COALESCE(s.summary, '') AS summary,
    COALESCE(s.status, '') AS status,
    s.updated_at,
    s.book_count,
    s.total_pages,
    CAST(COALESCE((
        SELECT b.cover_path
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), '') AS TEXT) AS cover_path
FROM collection_series cs
JOIN series s ON s.id = cs.series_id
WHERE cs.collection_id = sqlc.arg(collection_id)
ORDER BY cs.sort_order, COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_value);

-- name: ListForwardSeriesRelations :many
SELECT sr.id, sr.target_series_id, s.name AS target_series_name, sr.relation_type
FROM series_relations sr
JOIN series s ON s.id = sr.target_series_id
WHERE sr.source_series_id = ?;

-- name: ListReverseSeriesRelations :many
SELECT sr.id, sr.source_series_id AS target_series_id, s.name AS target_series_name, sr.relation_type
FROM series_relations sr
JOIN series s ON s.id = sr.source_series_id
WHERE sr.target_series_id = ?;

-- name: SeriesExistsByID :one
SELECT id FROM series WHERE id = ?;

-- name: FindExistingSeriesRelation :one
SELECT id FROM series_relations
WHERE (source_series_id = sqlc.arg(left_id) AND target_series_id = sqlc.arg(right_id))
   OR (source_series_id = sqlc.arg(right_id) AND target_series_id = sqlc.arg(left_id))
LIMIT 1;

-- name: CreateSeriesRelation :exec
INSERT INTO series_relations (source_series_id, target_series_id, relation_type)
VALUES (?, ?, ?);

-- name: UpdateSeriesRelation :exec
UPDATE series_relations
SET relation_type = ?
WHERE id = ?;

-- name: DeleteSeriesRelation :exec
DELETE FROM series_relations WHERE id = ?;

-- name: ListSmartFiltersByLibrary :many
SELECT id, library_id, name, active_tag, active_author, active_status, active_letter,
       read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
       sort_by_field, sort_dir, page_size, created_at, updated_at
FROM smart_filters
WHERE library_id = ?
ORDER BY updated_at DESC, id DESC;

-- name: GetSmartFilterByID :one
SELECT id, library_id, name, active_tag, active_author, active_status, active_letter,
       read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
       sort_by_field, sort_dir, page_size, created_at, updated_at
FROM smart_filters
WHERE id = ?
LIMIT 1;

-- name: UpsertSmartFilter :one
INSERT INTO smart_filters (
    library_id, name, active_tag, active_author, active_status, active_letter,
    read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
    sort_by_field, sort_dir, page_size, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(library_id, name) DO UPDATE SET
    active_tag = excluded.active_tag,
    active_author = excluded.active_author,
    active_status = excluded.active_status,
    active_letter = excluded.active_letter,
    read_state = excluded.read_state,
    min_rating = excluded.min_rating,
    max_rating = excluded.max_rating,
    min_progress = excluded.min_progress,
    max_progress = excluded.max_progress,
    added_within_days = excluded.added_within_days,
    sort_by_field = excluded.sort_by_field,
    sort_dir = excluded.sort_dir,
    page_size = excluded.page_size,
    updated_at = CURRENT_TIMESTAMP
RETURNING id, library_id, name, active_tag, active_author, active_status, active_letter,
          read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
          sort_by_field, sort_dir, page_size, created_at, updated_at;

-- name: UpdateSmartFilter :one
UPDATE smart_filters
SET name = ?,
    active_tag = ?,
    active_author = ?,
    active_status = ?,
    active_letter = ?,
    read_state = ?,
    min_rating = ?,
    max_rating = ?,
    min_progress = ?,
    max_progress = ?,
    added_within_days = ?,
    sort_by_field = ?,
    sort_dir = ?,
    page_size = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING id, library_id, name, active_tag, active_author, active_status, active_letter,
          read_state, min_rating, max_rating, min_progress, max_progress, added_within_days,
          sort_by_field, sort_dir, page_size, created_at, updated_at;

-- name: DeleteSmartFilter :execrows
DELETE FROM smart_filters WHERE id = ?;

-- name: ClearAllBookCoverPaths :exec
UPDATE books SET cover_path = NULL, updated_at = CURRENT_TIMESTAMP
WHERE cover_path IS NOT NULL AND cover_path != '';

-- name: ClearAllSeriesStatsCoverPaths :exec
UPDATE series_stats SET cover_path = '' WHERE cover_path != '';

-- name: SetBookCoverIfMissing :execrows
UPDATE books
SET cover_path = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND (cover_path IS NULL OR cover_path = '');

-- name: ListExternalLibraryBooks :many
SELECT b.id AS book_id, b.series_id, s.name AS series_name, b.path
FROM books b
JOIN series s ON s.id = b.series_id
WHERE b.library_id = ?
ORDER BY s.name, b.path;

-- name: GetSeriesIDByBookID :one
SELECT series_id FROM books WHERE id = ?;

-- name: GetSeriesIDByBookPath :one
SELECT series_id FROM books WHERE path = ?;

-- name: GetReadingListItemProgressByList :many
SELECT
    rli.series_id,
    COALESCE(ss.read_book_count, 0) AS read_books,
    COALESCE(ss.completed_book_count, 0) AS completed_books,
    COALESCE(s.book_count, 0) AS total_books
FROM reading_list_items rli
JOIN series s ON s.id = rli.series_id
LEFT JOIN series_stats ss ON ss.series_id = rli.series_id
WHERE rli.reading_list_id = ?;

-- name: GetDashboardCoreStats :one
SELECT
    (SELECT COUNT(*) FROM series) AS total_series,
    (SELECT COUNT(*) FROM books) AS total_books,
    (SELECT COUNT(*) FROM books WHERE last_read_page > 0) AS read_books,
    (SELECT COALESCE(SUM(page_count), 0) FROM books) AS total_pages,
    (SELECT COUNT(DISTINCT date) FROM reading_activity WHERE date >= DATE('now', '-7 days')) AS active_days_7;

-- name: ListLibrarySizes :many
SELECT l.id AS library_id, l.name AS library_name, COALESCE(bs.total_size, 0) AS total_size
FROM libraries l
LEFT JOIN (
    SELECT library_id, SUM(size) AS total_size
    FROM books INDEXED BY idx_books_library_size
    GROUP BY library_id
) bs ON bs.library_id = l.id
ORDER BY bs.total_size DESC;

-- name: GetActivityHeatmap :many
SELECT date, SUM(pages_read) AS page_count
FROM reading_activity
WHERE date >= DATE('now', sqlc.arg(offset_clause))
GROUP BY date
ORDER BY date ASC;

-- name: LogReadingActivity :exec
INSERT INTO reading_activity (book_id, date, pages_read)
VALUES (?, DATE('now'), ?)
ON CONFLICT(book_id, date) DO UPDATE SET
    pages_read = MAX(reading_activity.pages_read, excluded.pages_read);

-- name: ListReadingBookmarks :many
SELECT id, book_id, page, note, created_at, updated_at
FROM reading_bookmarks
WHERE book_id = ?
ORDER BY page ASC, id ASC;

-- name: UpsertReadingBookmark :one
INSERT INTO reading_bookmarks (book_id, page, note)
VALUES (?, ?, ?)
ON CONFLICT(book_id, page) DO UPDATE SET
    note = excluded.note,
    updated_at = CURRENT_TIMESTAMP
RETURNING id, book_id, page, note, created_at, updated_at;

-- name: DeleteReadingBookmark :execrows
DELETE FROM reading_bookmarks
WHERE id = ? AND book_id = ?;

-- name: GetRecentReadAll :many
SELECT
    s.name AS series_name,
    s.id AS series_id,
    b.id AS book_id,
    b.name AS book_name,
    b.title AS book_title,
    ss.last_read_at,
    b.last_read_page,
    b.page_count,
    COALESCE(ss.cover_path, '') AS cover_path
FROM series_stats ss INDEXED BY idx_series_stats_last_read
JOIN series s ON s.id = ss.series_id
JOIN books b ON b.id = ss.last_read_book_id
WHERE ss.last_read_at IS NOT NULL
  AND ss.last_read_book_id > 0
  AND b.last_read_page IS NOT NULL
  AND b.last_read_page > 0
ORDER BY ss.last_read_at DESC, s.name ASC
LIMIT ?;

-- name: GetRecommendations :many
WITH preferred_tags AS (
    SELECT DISTINCT st.tag_id
    FROM series_tags st
    JOIN series s ON s.id = st.series_id
    WHERE s.is_favorite = 1
       OR (SELECT COUNT(*) FROM books b WHERE b.series_id = s.id AND b.last_read_page > 0) >= 2
),
unread_series AS (
    SELECT s.id
    FROM series s
    WHERE NOT EXISTS (SELECT 1 FROM books b WHERE b.series_id = s.id AND b.last_read_page > 0)
)
SELECT s.id, s.name, s.title, s.book_count,
    (SELECT b.cover_path FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name LIMIT 1) AS cover_path,
    COUNT(st.tag_id) AS score
FROM unread_series us
JOIN series s ON s.id = us.id
JOIN series_tags st ON st.series_id = s.id
JOIN preferred_tags pt ON pt.tag_id = st.tag_id
GROUP BY s.id
ORDER BY score DESC
LIMIT ?;

-- name: UpsertTaskRecord :exec
INSERT INTO tasks (
    key, type, scope, scope_id, scope_name, status, message, error,
    current, total, can_cancel, retryable, params,
    started_at, updated_at, finished_at, sequence
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    type = excluded.type,
    scope = excluded.scope,
    scope_id = excluded.scope_id,
    scope_name = excluded.scope_name,
    status = excluded.status,
    message = excluded.message,
    error = excluded.error,
    current = excluded.current,
    total = excluded.total,
    can_cancel = excluded.can_cancel,
    retryable = excluded.retryable,
    params = excluded.params,
    started_at = excluded.started_at,
    updated_at = excluded.updated_at,
    finished_at = excluded.finished_at,
    sequence = excluded.sequence;

-- name: MarkInterruptedTasks :execrows
UPDATE tasks
SET status = 'interrupted',
    message = ?,
    error = ?,
    updated_at = CURRENT_TIMESTAMP,
    finished_at = CURRENT_TIMESTAMP
WHERE status IN ('running', 'paused', 'cancelling');

-- name: GetLastTaskKeyForScope :one
SELECT key FROM tasks
WHERE scope = ? AND scope_id = ?
ORDER BY updated_at DESC
LIMIT 1;

-- name: CountHealthEmptyPages :one
SELECT COUNT(*)
FROM books b
WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
  AND b.page_count <= 0;

-- name: ListHealthEmptyPages :many
SELECT l.id AS library_id, l.name AS library_name, s.id AS series_id, s.name AS series_name,
       b.id AS book_id, b.name AS book_name, b.path,
       'page_count <= 0' AS detail, CAST(1 AS INTEGER) AS issue_count
FROM books b
JOIN series s ON s.id = b.series_id
JOIN libraries l ON l.id = b.library_id
WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
  AND b.page_count <= 0
ORDER BY b.updated_at DESC, b.id DESC
LIMIT sqlc.arg(limit_count);

-- name: CountHealthMissingCover :one
SELECT COUNT(*)
FROM books b
WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
  AND (b.cover_path IS NULL OR b.cover_path = '');

-- name: ListHealthMissingCover :many
SELECT l.id AS library_id, l.name AS library_name, s.id AS series_id, s.name AS series_name,
       b.id AS book_id, b.name AS book_name, b.path,
       'cover_path is empty' AS detail, CAST(1 AS INTEGER) AS issue_count
FROM books b
JOIN series s ON s.id = b.series_id
JOIN libraries l ON l.id = b.library_id
WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
  AND (b.cover_path IS NULL OR b.cover_path = '')
ORDER BY b.updated_at DESC, b.id DESC
LIMIT sqlc.arg(limit_count);

-- name: CountHealthMissingMetadata :one
SELECT COUNT(*)
FROM series s
WHERE (sqlc.arg(library_id) = 0 OR s.library_id = sqlc.arg(library_id))
  AND (
    s.title IS NULL OR s.title = ''
    OR s.summary IS NULL OR s.summary = ''
    OR (
        NOT EXISTS (SELECT 1 FROM series_tags st WHERE st.series_id = s.id)
        AND NOT EXISTS (SELECT 1 FROM series_authors sa WHERE sa.series_id = s.id)
    )
  );

-- name: ListHealthMissingMetadata :many
SELECT l.id AS library_id, l.name AS library_name, s.id AS series_id, s.name AS series_name,
       NULL AS book_id, '' AS book_name, s.path,
       CASE
           WHEN s.title IS NULL OR s.title = '' THEN 'missing title'
           WHEN s.summary IS NULL OR s.summary = '' THEN 'missing summary'
           ELSE 'missing tags and authors'
       END AS detail,
       CAST(1 AS INTEGER) AS issue_count
FROM series s
JOIN libraries l ON l.id = s.library_id
WHERE (sqlc.arg(library_id) = 0 OR s.library_id = sqlc.arg(library_id))
  AND (
    s.title IS NULL OR s.title = ''
    OR s.summary IS NULL OR s.summary = ''
    OR (
        NOT EXISTS (SELECT 1 FROM series_tags st WHERE st.series_id = s.id)
        AND NOT EXISTS (SELECT 1 FROM series_authors sa WHERE sa.series_id = s.id)
    )
  )
ORDER BY s.updated_at DESC, s.id DESC
LIMIT sqlc.arg(limit_count);

-- name: CountHealthDuplicateFileHash :one
SELECT COALESCE(SUM(cnt), 0) FROM (
    SELECT COUNT(*) AS cnt
    FROM books b
    WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
      AND b.file_hash IS NOT NULL AND b.file_hash != ''
    GROUP BY b.file_hash
    HAVING COUNT(*) > 1
);

-- name: ListHealthDuplicateFileHash :many
WITH duplicates AS (
    SELECT b.file_hash, COUNT(*) AS cnt
    FROM books b
    WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
      AND b.file_hash IS NOT NULL AND b.file_hash != ''
    GROUP BY b.file_hash
    HAVING COUNT(*) > 1
)
SELECT l.id AS library_id, l.name AS library_name, s.id AS series_id, s.name AS series_name,
       b.id AS book_id, b.name AS book_name, b.path,
       'file_hash=' || b.file_hash AS detail, d.cnt AS issue_count
FROM duplicates d
JOIN books b ON b.file_hash = d.file_hash
JOIN series s ON s.id = b.series_id
JOIN libraries l ON l.id = b.library_id
WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
ORDER BY d.cnt DESC, b.file_hash, b.id
LIMIT sqlc.arg(limit_count);

-- name: CountHealthMissingQuickHash :one
SELECT COUNT(*)
FROM books b
WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
  AND COALESCE(b.quick_hash, '') = '';

-- name: ListHealthMissingQuickHash :many
SELECT l.id AS library_id, l.name AS library_name, s.id AS series_id, s.name AS series_name,
       b.id AS book_id, b.name AS book_name, b.path,
       'quick_hash is empty' AS detail, CAST(1 AS INTEGER) AS issue_count
FROM books b
JOIN series s ON s.id = b.series_id
JOIN libraries l ON l.id = b.library_id
WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
  AND COALESCE(b.quick_hash, '') = ''
ORDER BY b.updated_at DESC, b.id DESC
LIMIT sqlc.arg(limit_count);

-- name: CountHealthDuplicateQuickHash :one
SELECT COALESCE(SUM(cnt), 0) FROM (
    SELECT COUNT(*) AS cnt
    FROM books b
    WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
      AND b.quick_hash IS NOT NULL AND b.quick_hash != ''
    GROUP BY b.quick_hash
    HAVING COUNT(*) > 1
);

-- name: ListHealthDuplicateQuickHash :many
WITH duplicates AS (
    SELECT b.quick_hash, COUNT(*) AS cnt
    FROM books b
    WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
      AND b.quick_hash IS NOT NULL AND b.quick_hash != ''
    GROUP BY b.quick_hash
    HAVING COUNT(*) > 1
)
SELECT l.id AS library_id, l.name AS library_name, s.id AS series_id, s.name AS series_name,
       b.id AS book_id, b.name AS book_name, b.path,
       'quick_hash=' || b.quick_hash AS detail, d.cnt AS issue_count
FROM duplicates d
JOIN books b ON b.quick_hash = d.quick_hash
JOIN series s ON s.id = b.series_id
JOIN libraries l ON l.id = b.library_id
WHERE (sqlc.arg(library_id) = 0 OR b.library_id = sqlc.arg(library_id))
ORDER BY d.cnt DESC, b.quick_hash, b.id
LIMIT sqlc.arg(limit_count);

-- name: CountHealthUnmatchedKOReader :one
SELECT COUNT(*) FROM koreader_progress kp WHERE kp.book_id IS NULL;

-- name: ListHealthUnmatchedKOReader :many
SELECT CAST(0 AS INTEGER) AS library_id, CAST('' AS TEXT) AS library_name,
       NULL AS series_id, '' AS series_name,
       NULL AS book_id, '' AS book_name,
       kp.document AS path,
       (kp.username || ' / ' || kp.device) AS detail,
       CAST(1 AS INTEGER) AS issue_count
FROM koreader_progress kp
WHERE kp.book_id IS NULL
ORDER BY kp.updated_at DESC, kp.id DESC
LIMIT ?;

-- name: GetConnectedSeriesRelations :many
WITH RECURSIVE
  connected (id) AS (
    SELECT CAST(sqlc.arg(start_series_id) AS INTEGER)
    UNION
    SELECT target_series_id FROM series_relations sr JOIN connected c ON sr.source_series_id = c.id
    UNION
    SELECT source_series_id FROM series_relations sr JOIN connected c ON sr.target_series_id = c.id
  )
SELECT sr.id, sr.source_series_id, sr.target_series_id, sr.relation_type,
       s1.name AS source_series_name,
       s2.name AS target_series_name,
       ss1.cover_path AS source_cover_path,
       ss2.cover_path AS target_cover_path
FROM series_relations sr
JOIN series s1 ON sr.source_series_id = s1.id
JOIN series s2 ON sr.target_series_id = s2.id
LEFT JOIN series_stats ss1 ON ss1.series_id = s1.id
LEFT JOIN series_stats ss2 ON ss2.series_id = s2.id
WHERE sr.source_series_id IN connected AND sr.target_series_id IN connected;

-- name: GetContinueReadingSequels :many
SELECT 
    s2.id AS series_id,
    s2.name AS series_name,
    ss2.cover_path AS cover_path,
    s2.book_count AS total_books,
    ss2.completed_book_count AS read_books,
    sr.relation_type,
    s1.name AS source_series_name
FROM series_relations sr
JOIN series s1 ON sr.source_series_id = s1.id
JOIN series_stats ss1 ON ss1.series_id = s1.id
JOIN series s2 ON sr.target_series_id = s2.id
LEFT JOIN series_stats ss2 ON ss2.series_id = s2.id
WHERE (sr.relation_type = 'sequel' OR sr.relation_type = 'spinoff' OR sr.relation_type = 'side_story')
  AND s1.book_count > 0 
  AND ss1.completed_book_count >= s1.book_count
  AND (s2.book_count > 0 AND (ss2.completed_book_count IS NULL OR ss2.completed_book_count < s2.book_count))
LIMIT 10;

-- name: GetAllSeriesRelations :many
SELECT * FROM series_relations;

-- name: GetAllSeriesRelationsForLibrary :many
SELECT sr.id, sr.source_series_id, sr.target_series_id, sr.relation_type,
       s1.name AS source_series_name,
       s2.name AS target_series_name,
       ss1.cover_path AS source_cover_path,
       ss2.cover_path AS target_cover_path
FROM series_relations sr
JOIN series s1 ON sr.source_series_id = s1.id
JOIN series s2 ON sr.target_series_id = s2.id
LEFT JOIN series_stats ss1 ON ss1.series_id = s1.id
LEFT JOIN series_stats ss2 ON ss2.series_id = s2.id
WHERE s1.library_id = ? OR s2.library_id = ?;

-- name: DeleteFranchiseCollections :exec
DELETE FROM collections WHERE source_type = 'system_franchise';
