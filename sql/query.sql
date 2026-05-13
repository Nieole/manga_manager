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
    CAST(COALESCE((
        SELECT b.cover_path
        FROM books b
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name
        LIMIT 1
    ), '') AS TEXT) as cover_path
FROM series s
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
    ), 0) AS INTEGER) as cover_book_id
FROM series s
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
