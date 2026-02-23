-- name: CreateLibrary :one
INSERT INTO libraries (id, name, path)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetLibrary :one
SELECT * FROM libraries WHERE id = ? LIMIT 1;

-- name: ListLibraries :many
SELECT * FROM libraries ORDER BY name;

-- name: CreateSeries :one
INSERT INTO series (
    id, library_id, name, path, title, summary, publisher, status
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetSeries :one
SELECT * FROM series WHERE id = ? LIMIT 1;

-- name: ListSeriesByLibrary :many
SELECT s.*, 
       (SELECT b.cover_path 
        FROM books b 
        WHERE b.series_id = s.id AND b.cover_path IS NOT NULL 
        ORDER BY b.sort_number, b.name 
        LIMIT 1) as cover_path 
FROM series s 
WHERE s.library_id = ? 
ORDER BY s.name;

-- name: CreateBook :one
INSERT INTO books (
    id, series_id, library_id, name, path, size, file_modified_at, 
    title, summary, number, sort_number, page_count, cover_path
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

-- name: CreateBookPage :one
INSERT INTO book_pages (
    id, book_id, file_name, media_type, number, size, width, height
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: ListBookPages :many
SELECT * FROM book_pages WHERE book_id = ? ORDER BY number;

-- name: ListBooksByLibrary :many
SELECT id, path, file_modified_at, size, cover_path FROM books WHERE library_id = ?;

-- name: DeleteBookByPath :exec
DELETE FROM books WHERE path = ?;

-- name: DeletePagesByBookPath :exec
DELETE FROM book_pages WHERE book_id IN (SELECT id FROM books WHERE path = ?);

-- name: UpsertBookByPath :exec
INSERT INTO books (
    id, series_id, library_id, name, path, size, file_modified_at, 
    title, summary, number, sort_number, page_count, cover_path
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(path) DO UPDATE SET
    series_id = excluded.series_id,
    library_id = excluded.library_id,
    name = excluded.name,
    size = excluded.size,
    file_modified_at = excluded.file_modified_at,
    title = excluded.title,
    summary = excluded.summary,
    number = excluded.number,
    sort_number = excluded.sort_number,
    page_count = excluded.page_count,
    cover_path = excluded.cover_path,
    updated_at = CURRENT_TIMESTAMP;

-- name: UpdateBookProgress :exec
UPDATE books 
SET last_read_page = ?, last_read_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: GetNextBookInSeries :one
SELECT nb.* FROM books nb
INNER JOIN books cb ON cb.id = ? AND nb.series_id = cb.series_id
WHERE (nb.sort_number > cb.sort_number)
   OR (nb.sort_number = cb.sort_number AND nb.name > cb.name)
ORDER BY nb.sort_number, nb.name
LIMIT 1;
