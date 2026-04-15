CREATE TABLE IF NOT EXISTS libraries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    path TEXT NOT NULL UNIQUE,
    auto_scan BOOLEAN NOT NULL DEFAULT FALSE,
    koreader_sync_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    scan_interval INTEGER NOT NULL DEFAULT 60,
    scan_formats TEXT NOT NULL DEFAULT 'zip,cbz,rar,cbr',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS series (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    title TEXT,
    summary TEXT,
    publisher TEXT,
    status TEXT,
    rating REAL,
    language TEXT,
    locked_fields TEXT DEFAULT '',
    path TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_favorite BOOLEAN NOT NULL DEFAULT FALSE,
    volume_count INTEGER NOT NULL DEFAULT 0,
    book_count INTEGER NOT NULL DEFAULT 0,
    total_pages INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_series_library_id ON series(library_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_series_path ON series(path);

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS series_tags (
    series_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    PRIMARY KEY (series_id, tag_id),
    FOREIGN KEY(series_id) REFERENCES series(id) ON DELETE CASCADE,
    FOREIGN KEY(tag_id) REFERENCES tags(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS authors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(name, role)
);

CREATE TABLE IF NOT EXISTS series_authors (
    series_id INTEGER NOT NULL,
    author_id INTEGER NOT NULL,
    PRIMARY KEY (series_id, author_id),
    FOREIGN KEY(series_id) REFERENCES series(id) ON DELETE CASCADE,
    FOREIGN KEY(author_id) REFERENCES authors(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS books (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id INTEGER NOT NULL,
    library_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    path TEXT NOT NULL UNIQUE,
    size INTEGER NOT NULL,
    file_modified_at DATETIME NOT NULL,
    volume TEXT NOT NULL DEFAULT '',
    title TEXT,
    summary TEXT,
    number TEXT,
    sort_number REAL,
    page_count INTEGER NOT NULL DEFAULT 0,
    cover_path TEXT,
    last_read_page INTEGER,
    last_read_at DATETIME,
    file_hash TEXT,
    path_fingerprint TEXT,
    path_fingerprint_no_ext TEXT,
    filename_fingerprint TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (series_id) REFERENCES series(id) ON DELETE CASCADE,
    FOREIGN KEY (library_id) REFERENCES libraries(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_books_series_id ON books(series_id);
CREATE INDEX IF NOT EXISTS idx_books_library_id ON books(library_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_books_path ON books(path);

CREATE TABLE IF NOT EXISTS series_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(series_id) REFERENCES series(id) ON DELETE CASCADE
);

-- [#2] 自定义合集 / 智能书架
CREATE TABLE IF NOT EXISTS collections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    cover_url TEXT DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS collection_series (
    collection_id INTEGER NOT NULL,
    series_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (collection_id, series_id),
    FOREIGN KEY(collection_id) REFERENCES collections(id) ON DELETE CASCADE,
    FOREIGN KEY(series_id) REFERENCES series(id) ON DELETE CASCADE
);

-- [#5] 系列间关联（前传、续作、衍生等）
CREATE TABLE IF NOT EXISTS series_relations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_series_id INTEGER NOT NULL,
    target_series_id INTEGER NOT NULL,
    relation_type TEXT NOT NULL DEFAULT 'sequel',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source_series_id, target_series_id),
    FOREIGN KEY(source_series_id) REFERENCES series(id) ON DELETE CASCADE,
    FOREIGN KEY(target_series_id) REFERENCES series(id) ON DELETE CASCADE
);

-- [#6] 逐日阅读活动记录（精确活跃度热力图）
CREATE TABLE IF NOT EXISTS reading_activity (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id INTEGER NOT NULL,
    date TEXT NOT NULL,          -- YYYY-MM-DD
    pages_read INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(book_id, date),
    FOREIGN KEY(book_id) REFERENCES books(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_reading_activity_date ON reading_activity(date);

CREATE TABLE IF NOT EXISTS koreader_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    username TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS koreader_progress (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    document TEXT NOT NULL,
    progress TEXT NOT NULL,
    percentage REAL NOT NULL DEFAULT 0,
    device TEXT NOT NULL DEFAULT '',
    device_id TEXT NOT NULL DEFAULT '',
    book_id INTEGER,
    matched_by TEXT NOT NULL DEFAULT '',
    timestamp INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    raw_payload TEXT NOT NULL DEFAULT '',
    UNIQUE(username, document),
    FOREIGN KEY(book_id) REFERENCES books(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_koreader_progress_book_id ON koreader_progress(book_id);
CREATE INDEX IF NOT EXISTS idx_koreader_progress_username ON koreader_progress(username);

CREATE TABLE IF NOT EXISTS koreader_sync_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    direction TEXT NOT NULL,
    username TEXT NOT NULL DEFAULT '',
    document TEXT NOT NULL DEFAULT '',
    book_id INTEGER,
    status TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(book_id) REFERENCES books(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_koreader_sync_events_created_at ON koreader_sync_events(created_at);
