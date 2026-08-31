package database

import (
	"database/sql"
	"time"
)

type KOReaderSettings struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	SyncKey   string    `json:"-"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpsertKOReaderSettingsParams struct {
	Username string `json:"username"`
	SyncKey  string `json:"sync_key"`
}

type KOReaderAccount struct {
	ID          int64          `json:"id"`
	Username    string         `json:"username"`
	SyncKey     string         `json:"sync_key"`
	Enabled     bool           `json:"enabled"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	LastUsedAt  sql.NullTime   `json:"last_used_at"`
	LatestError sql.NullString `json:"latest_error"`
}

type CreateKOReaderAccountParams struct {
	Username string `json:"username"`
	SyncKey  string `json:"sync_key"`
	Enabled  bool   `json:"enabled"`
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
	BookID               int64  `json:"book_id"`
	Path                 string `json:"path"`
	PageCount            int64  `json:"page_count"`
	FileHash             string `json:"file_hash"`
	PathFingerprint      string `json:"path_fingerprint"`
	PathFingerprintNoExt string `json:"path_fingerprint_no_ext"`
	MatchedBy            string `json:"matched_by"`
	LastReadPage         *int64 `json:"last_read_page,omitempty"`
}

type BookIdentityCandidate struct {
	ID          int64  `json:"id"`
	LibraryID   int64  `json:"library_id"`
	LibraryPath string `json:"library_path"`
	Path        string `json:"path"`
}

type UpdateBookIdentityParams struct {
	ID                   int64  `json:"id"`
	FileHash             string `json:"file_hash"`
	QuickHash            string `json:"quick_hash"`
	PathFingerprint      string `json:"path_fingerprint"`
	PathFingerprintNoExt string `json:"path_fingerprint_no_ext"`
}

type KOReaderStats struct {
	Configured             bool         `json:"configured"`
	HasPassword            bool         `json:"has_password"`
	HasValidSyncKey        bool         `json:"has_valid_sync_key"`
	Username               string       `json:"username"`
	AccountCount           int64        `json:"account_count"`
	EnabledAccountCount    int64        `json:"enabled_account_count"`
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

type KOReaderSyncEvent struct {
	ID        int64         `json:"id"`
	Direction string        `json:"direction"`
	Username  string        `json:"username"`
	Document  string        `json:"document"`
	BookID    sql.NullInt64 `json:"book_id"`
	Status    string        `json:"status"`
	Message   string        `json:"message"`
	CreatedAt time.Time     `json:"created_at"`
}

type KOReaderDeviceDiagnostic struct {
	Username         string       `json:"username"`
	Device           string       `json:"device"`
	DeviceID         string       `json:"device_id"`
	TotalRecords     int64        `json:"total_records"`
	MatchedRecords   int64        `json:"matched_records"`
	UnmatchedRecords int64        `json:"unmatched_records"`
	LatestSyncAt     sql.NullTime `json:"latest_sync_at"`
	LatestDocument   string       `json:"latest_document"`
	LatestMatchedBy  string       `json:"latest_matched_by"`
	LatestError      string       `json:"latest_error"`
}

type KOReaderDeviceMatchMethod struct {
	Username  string `json:"username"`
	Device    string `json:"device"`
	DeviceID  string `json:"device_id"`
	MatchedBy string `json:"matched_by"`
	Count     int64  `json:"count"`
}

type KOReaderDeviceConflict struct {
	// SourceTable 是这一行来自哪张表：koreader_progress 或 koreader_sync_events。
	//
	// 必须显式带出来。这个列表是两张表的 UNION ALL，而两张表各自有独立的 AUTOINCREMENT
	// 序列、都从 1 开始，所以同一个 id 同时是一条进度和一条事件的合法主键——不是理论可能，
	// 是常态，调用方仅凭裸整数无法判断这一行能否喂给按进度主键操作的接口（见 ProgressID）。
	SourceTable string `json:"source_table"`
	// SourceID 是该行在其来源表里的主键。要定位这一行必须 (SourceTable, SourceID) 一起看。
	SourceID int64 `json:"source_id"`
	// ProgressID 只在这一行确实对应一条 koreader_progress 记录时才有值。
	// 「重置进度」这类按进度主键删除的操作只能用它——用 SourceID 会删错行。
	ProgressID sql.NullInt64 `json:"progress_id"`
	Type       string        `json:"type"`
	Severity   string        `json:"severity"`
	Username   string        `json:"username"`
	Device     string        `json:"device"`
	DeviceID   string        `json:"device_id"`
	Document   string        `json:"document"`
	BookID     sql.NullInt64 `json:"book_id"`
	MatchedBy  string        `json:"matched_by"`
	Status     string        `json:"status"`
	Message    string        `json:"message"`
	Percentage float64       `json:"percentage"`
	UpdatedAt  time.Time     `json:"updated_at"`
}
