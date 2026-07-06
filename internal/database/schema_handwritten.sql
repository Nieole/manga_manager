-- Business note: DDL for tables accessed ONLY by hand-written store methods (raw SQL), never by
-- sqlc-generated queries. They are split out of schema.sql because sqlc would generate model structs
-- (SeriesCustomField / User / Session / UserBookProgress / UserSeriesReview ...) that collide by name with
-- the hand-written business structs in the database package (duplicate-type compile error + models.go drift).
-- sqlc.yaml reads only schema.sql, so keeping these tables here avoids sqlc generating the colliding models.
-- Migrate() loads and runs this via //go:embed (see the combined schemaSQL in store.go), table creation is
-- identical to having them in schema.sql. Keep this file ASCII-only too, in case it is ever fed to sqlc.

-- series_custom_fields: per-series key-value metadata (distinct from series_metadata_provenance).
CREATE TABLE IF NOT EXISTS series_custom_fields (
    series_id INTEGER NOT NULL,
    field_key TEXT NOT NULL,
    field_value TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (series_id, field_key),
    FOREIGN KEY (series_id) REFERENCES series(id) ON DELETE CASCADE
);

-- ============================================================================
-- Site accounts (multi-user)
-- ============================================================================
-- users: site login accounts. role is 'admin' (full control: config/users/libraries/scan/metadata/dedup)
-- or 'regular' (browse + own reading progress/bookmarks/reviews only, cannot edit shared library metadata).
-- password_hash stores a bcrypt digest, never plaintext. must_change_password is set when an admin creates
-- an account, forcing a password change on first login. The first admin inherits the old global reading
-- progress and KOReader accounts (see migration logic).
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT 'regular',
    display_name TEXT NOT NULL DEFAULT '',
    must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username);

-- sessions: server-side sessions. id stores the SHA-256 of the cookie token (the cookie holds the raw
-- random token, the DB never stores plaintext). csrf_token is issued to the client and required in the
-- X-CSRF-Token header for mutating requests. Sessions expire at expires_at, all of a user's sessions are
-- cleared on password change or logout.
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    csrf_token TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

-- ============================================================================
-- Per-user reading progress (multi-user phase 2)
-- ============================================================================
-- user_book_progress: each user's per-book progress (replaces the old global books.last_read_page/_at).
-- Old global progress migrates here for the first admin (see MigrateGlobalProgressToUser). Writes go here,
-- reads overlay the current user onto book responses. books.last_read_page/_at are kept as the migration
-- source and are no longer read/written by the web UI.
CREATE TABLE IF NOT EXISTS user_book_progress (
    user_id INTEGER NOT NULL,
    book_id INTEGER NOT NULL,
    last_read_page INTEGER,
    last_read_at DATETIME,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, book_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (book_id) REFERENCES books(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_book_progress_book ON user_book_progress(book_id);
CREATE INDEX IF NOT EXISTS idx_user_book_progress_user_read ON user_book_progress(user_id, last_read_page) WHERE last_read_page > 0;
CREATE INDEX IF NOT EXISTS idx_user_book_progress_user_time ON user_book_progress(user_id, last_read_at) WHERE last_read_at IS NOT NULL;

-- user_series_progress: per-user x per-series aggregate derived from user_book_progress (progress bar /
-- read+completed counts / recent read). Equivalent to the old series_stats progress columns but split per
-- user, series_stats keeps only the global cover/tag cache. List/search/smart-collection/dashboard LEFT
-- JOIN this by current user, and it is refreshed per (user, series) after each progress write.
CREATE TABLE IF NOT EXISTS user_series_progress (
    user_id INTEGER NOT NULL,
    series_id INTEGER NOT NULL,
    read_pages INTEGER NOT NULL DEFAULT 0,
    read_book_count INTEGER NOT NULL DEFAULT 0,
    completed_book_count INTEGER NOT NULL DEFAULT 0,
    last_read_at DATETIME,
    last_read_book_id INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, series_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (series_id) REFERENCES series(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_series_progress_last_read ON user_series_progress(user_id, last_read_at);
CREATE INDEX IF NOT EXISTS idx_user_series_progress_series ON user_series_progress(series_id);

-- ============================================================================
-- Deep statistics (item 6): per-user activity / reading time / series reviews
-- ============================================================================
-- user_reading_activity: per-user daily pages read, replacing the global reading_activity for the heatmap /
-- reading streak / annual-monthly review. Old global reading_activity migrates to the first admin, going
-- forward writes are dual (global + per-user).
CREATE TABLE IF NOT EXISTS user_reading_activity (
    user_id INTEGER NOT NULL,
    book_id INTEGER NOT NULL,
    date TEXT NOT NULL,
    pages_read INTEGER NOT NULL DEFAULT 0,
    read_seconds INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, book_id, date),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (book_id) REFERENCES books(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_reading_activity_user_date ON user_reading_activity(user_id, date);

-- user_book_reading_time: per-user, per-book accumulated "active reading" seconds (client-timed, additive).
CREATE TABLE IF NOT EXISTS user_book_reading_time (
    user_id INTEGER NOT NULL,
    book_id INTEGER NOT NULL,
    total_seconds INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, book_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (book_id) REFERENCES books(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_book_reading_time_user ON user_book_reading_time(user_id);

-- user_series_review: each user's personal rating (1-5) + short review per series (distinct from the global
-- series.rating scraped metadata).
CREATE TABLE IF NOT EXISTS user_series_review (
    user_id INTEGER NOT NULL,
    series_id INTEGER NOT NULL,
    rating REAL,
    review TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, series_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (series_id) REFERENCES series(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_series_review_series ON user_series_review(series_id);
