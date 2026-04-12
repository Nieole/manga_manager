package database

import (
	"database/sql"
	"time"
)

type KOReaderSettings struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UpsertKOReaderSettingsParams struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
}

type KOReaderProgress struct {
	ID         int64         `json:"id"`
	Username   string        `json:"username"`
	Document   string        `json:"document"`
	Progress   string        `json:"progress"`
	Percentage float64       `json:"percentage"`
	Device     string        `json:"device"`
	DeviceID   string        `json:"device_id"`
	BookID     sql.NullInt64 `json:"book_id"`
	MatchedBy  string        `json:"matched_by"`
	Timestamp  int64         `json:"timestamp"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	RawPayload string        `json:"raw_payload"`
}

type UpsertKOReaderProgressParams struct {
	Username   string        `json:"username"`
	Document   string        `json:"document"`
	Progress   string        `json:"progress"`
	Percentage float64       `json:"percentage"`
	Device     string        `json:"device"`
	DeviceID   string        `json:"device_id"`
	BookID     sql.NullInt64 `json:"book_id"`
	MatchedBy  string        `json:"matched_by"`
	Timestamp  int64         `json:"timestamp"`
	RawPayload string        `json:"raw_payload"`
}

type KOReaderBookMatch struct {
	BookID              int64  `json:"book_id"`
	Path                string `json:"path"`
	PageCount           int64  `json:"page_count"`
	FileHash            string `json:"file_hash"`
	PathFingerprint     string `json:"path_fingerprint"`
	FilenameFingerprint string `json:"filename_fingerprint"`
	MatchedBy           string `json:"matched_by"`
	LastReadPage        *int64 `json:"last_read_page,omitempty"`
}

type BookIdentityCandidate struct {
	ID          int64  `json:"id"`
	LibraryID   int64  `json:"library_id"`
	LibraryPath string `json:"library_path"`
	Path        string `json:"path"`
}

type UpdateBookIdentityParams struct {
	ID                  int64  `json:"id"`
	FileHash            string `json:"file_hash"`
	PathFingerprint     string `json:"path_fingerprint"`
	FilenameFingerprint string `json:"filename_fingerprint"`
}

type KOReaderStats struct {
	Configured             bool         `json:"configured"`
	HasPassword            bool         `json:"has_password"`
	Username               string       `json:"username"`
	TotalBooks             int64        `json:"total_books"`
	HashedBooks            int64        `json:"hashed_books"`
	UnmatchedProgressCount int64        `json:"unmatched_progress_count"`
	MatchedProgressCount   int64        `json:"matched_progress_count"`
	LatestSyncAt           sql.NullTime `json:"latest_sync_at"`
}

type CreateKOReaderSyncEventParams struct {
	Direction string        `json:"direction"`
	Username  string        `json:"username"`
	Document  string        `json:"document"`
	BookID    sql.NullInt64 `json:"book_id"`
	Status    string        `json:"status"`
	Message   string        `json:"message"`
}
