-- name: CreateLibrary :one
INSERT INTO libraries (name, path)
VALUES (?, ?)
RETURNING *;

-- name: GetLibrary :one
SELECT * FROM libraries WHERE id = ? LIMIT 1;

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
SELECT * FROM books WHERE series_id = ? ORDER BY sort_number, name;



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
SET last_read_page = ?, last_read_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpsertSeriesByPath :one
INSERT INTO series (
    library_id, name, path, title, summary, publisher, status, rating, language, locked_fields
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
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
    COUNT(DISTINCT NULLIF(b.volume, '')) as volume_count,
    COUNT(DISTINCT b.id) as actual_book_count,
    COUNT(DISTINCT CASE WHEN b.last_read_page > 0 THEN b.id END) as read_count,
    SUM(b.page_count) as total_pages
FROM series s
LEFT JOIN series_tags st ON s.id = st.series_id
LEFT JOIN tags t ON st.tag_id = t.id
LEFT JOIN books b ON s.id = b.series_id
WHERE s.library_id = ? 
GROUP BY s.id
ORDER BY s.name;

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
WHERE (nb.sort_number > cb.sort_number)
   OR (nb.sort_number = cb.sort_number AND nb.name > cb.name)
ORDER BY nb.sort_number, nb.name
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
