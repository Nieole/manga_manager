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
    library_id, name, path, title, summary, publisher, status, rating, language, locked_fields
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetSeries :one
SELECT * FROM series WHERE id = ? LIMIT 1;

-- name: ListSeriesByLibrary :many
SELECT s.*, 
       (SELECT b.cover_path 
        FROM books b 
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
        ORDER BY b.sort_number, b.name 
        LIMIT 1) as cover_path 
FROM series s 
WHERE s.library_id = ? 
ORDER BY s.name;

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
    volume_count, book_count, total_pages
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(path) DO UPDATE SET
    library_id = excluded.library_id,
    name = excluded.name,
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
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: UpsertTag :one
INSERT INTO tags (name) VALUES (?)
ON CONFLICT(name) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: LinkSeriesTag :exec
INSERT OR IGNORE INTO series_tags (series_id, tag_id) VALUES (?, ?);

-- name: GetSeriesByLibrary :many
SELECT 
    s.*, 
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
