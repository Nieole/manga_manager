CREATE TABLE IF NOT EXISTS libraries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    path TEXT NOT NULL UNIQUE,
    scan_mode TEXT NOT NULL DEFAULT 'none',
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
    name_initial TEXT NOT NULL DEFAULT '#',
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
CREATE INDEX IF NOT EXISTS idx_series_name_initial ON series(name_initial);
CREATE INDEX IF NOT EXISTS idx_series_library_initial ON series(library_id, name_initial);
CREATE INDEX IF NOT EXISTS idx_series_library_status ON series(library_id, status);
CREATE INDEX IF NOT EXISTS idx_series_library_updated ON series(library_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_series_library_created ON series(library_id, created_at);
CREATE INDEX IF NOT EXISTS idx_series_library_name ON series(library_id, name);
CREATE INDEX IF NOT EXISTS idx_series_library_initial_name ON series(library_id, name_initial, name);
CREATE INDEX IF NOT EXISTS idx_series_library_status_name ON series(library_id, status, name);
CREATE INDEX IF NOT EXISTS idx_series_library_updated_name ON series(library_id, updated_at, name);
CREATE INDEX IF NOT EXISTS idx_series_library_created_name ON series(library_id, created_at, name);
CREATE INDEX IF NOT EXISTS idx_series_library_rating ON series(library_id, rating, name);
CREATE INDEX IF NOT EXISTS idx_series_library_books ON series(library_id, book_count, name);
CREATE INDEX IF NOT EXISTS idx_series_library_volumes ON series(library_id, volume_count, name);
CREATE INDEX IF NOT EXISTS idx_series_library_pages ON series(library_id, total_pages, name);
CREATE INDEX IF NOT EXISTS idx_series_library_favorite ON series(library_id, is_favorite, name);
CREATE INDEX IF NOT EXISTS idx_series_library_status_books ON series(library_id, status, book_count, name);

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
    quick_hash TEXT,
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
CREATE INDEX IF NOT EXISTS idx_books_file_hash ON books(file_hash);
CREATE INDEX IF NOT EXISTS idx_books_quick_hash ON books(quick_hash);
CREATE INDEX IF NOT EXISTS idx_books_path_fingerprint ON books(path_fingerprint);
CREATE INDEX IF NOT EXISTS idx_books_path_fingerprint_no_ext ON books(path_fingerprint_no_ext);
CREATE INDEX IF NOT EXISTS idx_books_series_sort ON books(series_id, volume, sort_number, name);
CREATE INDEX IF NOT EXISTS idx_books_series_read ON books(series_id, last_read_page);
CREATE INDEX IF NOT EXISTS idx_books_read_progress_series ON books(last_read_page, series_id) WHERE last_read_page > 0;
CREATE INDEX IF NOT EXISTS idx_books_cover_pick ON books(series_id, sort_number, name) WHERE cover_path IS NOT NULL AND cover_path != '';
CREATE INDEX IF NOT EXISTS idx_books_library_modified ON books(library_id, file_modified_at);

CREATE TABLE IF NOT EXISTS page_manifest (
    book_id INTEGER NOT NULL,
    page_number INTEGER NOT NULL,
    entry_name TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    media_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (book_id, page_number),
    FOREIGN KEY(book_id) REFERENCES books(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_page_manifest_book_id ON page_manifest(book_id, page_number);

CREATE TABLE IF NOT EXISTS series_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(series_id) REFERENCES series(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS metadata_reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id INTEGER NOT NULL,
    provider TEXT NOT NULL,
    source_url TEXT NOT NULL DEFAULT '',
    source_id INTEGER NOT NULL DEFAULT 0,
    source_query TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    raw_payload TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    applied_at DATETIME,
    rejected_at DATETIME,
    FOREIGN KEY(series_id) REFERENCES series(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_metadata_reviews_series_status ON metadata_reviews(series_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_metadata_reviews_status ON metadata_reviews(status, updated_at);

CREATE TABLE IF NOT EXISTS metadata_review_fields (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    review_id INTEGER NOT NULL,
    field_name TEXT NOT NULL,
    current_value TEXT NOT NULL DEFAULT '',
    proposed_value TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT '',
    source_url TEXT NOT NULL DEFAULT '',
    locked BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(review_id, field_name),
    FOREIGN KEY(review_id) REFERENCES metadata_reviews(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_metadata_review_fields_review_id ON metadata_review_fields(review_id);

CREATE TABLE IF NOT EXISTS ai_grouping_reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id INTEGER NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    summary TEXT NOT NULL DEFAULT '',
    raw_payload TEXT NOT NULL DEFAULT '',
    candidate_count INTEGER NOT NULL DEFAULT 0,
    collection_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    applied_at DATETIME,
    rejected_at DATETIME,
    FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_ai_grouping_reviews_library_status ON ai_grouping_reviews(library_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_ai_grouping_reviews_status ON ai_grouping_reviews(status, updated_at);

CREATE TABLE IF NOT EXISTS ai_grouping_review_collections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    review_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    series_ids TEXT NOT NULL DEFAULT '[]',
    series_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    created_collection_id INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(review_id) REFERENCES ai_grouping_reviews(id) ON DELETE CASCADE,
    FOREIGN KEY(created_collection_id) REFERENCES collections(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_grouping_review_collections_review_id ON ai_grouping_review_collections(review_id);

CREATE TABLE IF NOT EXISTS series_metadata_provenance (
    series_id INTEGER NOT NULL,
    field_name TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    source_url TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    review_id INTEGER,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(series_id, field_name),
    FOREIGN KEY(series_id) REFERENCES series(id) ON DELETE CASCADE,
    FOREIGN KEY(review_id) REFERENCES metadata_reviews(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_series_metadata_provenance_series_id ON series_metadata_provenance(series_id, field_name);

-- [#2] 自定义合集 / 智能书架
CREATE TABLE IF NOT EXISTS collections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    cover_url TEXT DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    source_type TEXT NOT NULL DEFAULT 'manual',
    source_review_id INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(source_review_id) REFERENCES ai_grouping_reviews(id) ON DELETE SET NULL
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

CREATE TABLE IF NOT EXISTS smart_filters (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    active_tag TEXT,
    active_author TEXT,
    active_status TEXT,
    active_letter TEXT,
    read_state TEXT,
    min_rating REAL,
    max_rating REAL,
    min_progress REAL,
    max_progress REAL,
    added_within_days INTEGER,
    sort_by_field TEXT NOT NULL DEFAULT 'name',
    sort_dir TEXT NOT NULL DEFAULT 'asc',
    page_size INTEGER NOT NULL DEFAULT 30,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(library_id, name),
    FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE
);


CREATE TABLE IF NOT EXISTS reading_lists (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS reading_list_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    reading_list_id INTEGER NOT NULL,
    series_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(reading_list_id, series_id),
    FOREIGN KEY(reading_list_id) REFERENCES reading_lists(id) ON DELETE CASCADE,
    FOREIGN KEY(series_id) REFERENCES series(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_reading_list_items_list_id ON reading_list_items(reading_list_id);
CREATE INDEX IF NOT EXISTS idx_reading_list_items_series_id ON reading_list_items(series_id);

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

CREATE TABLE IF NOT EXISTS reading_bookmarks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id INTEGER NOT NULL,
    page INTEGER NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(book_id, page),
    FOREIGN KEY(book_id) REFERENCES books(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_reading_bookmarks_book_id ON reading_bookmarks(book_id);

CREATE TABLE IF NOT EXISTS tasks (
    key TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'system',
    scope_id INTEGER,
    scope_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    current INTEGER NOT NULL DEFAULT 0,
    total INTEGER NOT NULL DEFAULT 0,
    can_cancel BOOLEAN NOT NULL DEFAULT FALSE,
    retryable BOOLEAN NOT NULL DEFAULT FALSE,
    params TEXT NOT NULL DEFAULT '',
    started_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    finished_at DATETIME,
    sequence INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_tasks_updated_at ON tasks(updated_at);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_scope ON tasks(scope, scope_id);

CREATE TABLE IF NOT EXISTS koreader_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    username TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS koreader_accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    sync_key TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_koreader_accounts_username ON koreader_accounts(username);

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
