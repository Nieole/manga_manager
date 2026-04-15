package koreader

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrRegistrationClosed = errors.New("registration closed")
	ErrAlreadyConfigured  = errors.New("account already configured")
	ErrAccountNotFound    = errors.New("account not found")
	ErrProgressNotFound   = errors.New("progress not found")
)

type Service struct {
	store database.Store
	cfg   *config.Manager
}

type Credentials struct {
	Username string
	Key      string
}

type ProgressPayload struct {
	Document   string  `json:"document"`
	Progress   string  `json:"progress"`
	Percentage float64 `json:"percentage"`
	Device     string  `json:"device"`
	DeviceID   string  `json:"device_id"`
}

type SyncResult struct {
	Record  database.KOReaderProgress
	Matched bool
	BookID  *int64
}

func NewService(store database.Store, cfg *config.Manager) *Service {
	return &Service{store: store, cfg: cfg}
}

func (s *Service) Register(ctx context.Context, username, key string, allowRegistration bool) (database.KOReaderSettings, error) {
	_ = ctx
	_ = username
	_ = key
	_ = allowRegistration
	return database.KOReaderSettings{}, ErrRegistrationClosed
}

func (s *Service) Authenticate(ctx context.Context, creds Credentials) (database.KOReaderAccount, error) {
	creds.Username = strings.TrimSpace(creds.Username)
	creds.Key = NormalizeSyncKey(creds.Key)
	slog.Info("KOReader authenticate attempt",
		"username", creds.Username,
		"client_key_prefix", keyPreview(creds.Key),
	)
	if creds.Username == "" || creds.Key == "" {
		slog.Warn("KOReader authenticate rejected: missing credentials",
			"username", creds.Username,
			"client_key_prefix", keyPreview(creds.Key),
		)
		return database.KOReaderAccount{}, ErrUnauthorized
	}

	account, err := s.store.GetKOReaderAccountByUsername(ctx, creds.Username)
	if err != nil {
		if err == sql.ErrNoRows {
			slog.Warn("KOReader authenticate rejected: account not found",
				"username", creds.Username,
				"client_key_prefix", keyPreview(creds.Key),
			)
			return database.KOReaderAccount{}, ErrForbidden
		}
		slog.Error("KOReader authenticate failed: account lookup error",
			"username", creds.Username,
			"error", err,
		)
		return database.KOReaderAccount{}, err
	}
	if !account.Enabled {
		slog.Warn("KOReader authenticate rejected: account disabled",
			"username", creds.Username,
			"account_id", account.ID,
		)
		return database.KOReaderAccount{}, ErrForbidden
	}
	if account.Username == "" || account.SyncKey == "" {
		slog.Warn("KOReader authenticate rejected: account missing stored sync key",
			"username", creds.Username,
			"account_id", account.ID,
		)
		return database.KOReaderAccount{}, ErrForbidden
	}
	expectedKey := HashKey(account.SyncKey)
	if expectedKey != creds.Key {
		slog.Warn("KOReader authenticate rejected: client key mismatch",
			"username", creds.Username,
			"account_id", account.ID,
			"stored_raw_key_length", len(account.SyncKey),
			"expected_key_prefix", keyPreview(expectedKey),
			"client_key_prefix", keyPreview(creds.Key),
		)
		return database.KOReaderAccount{}, ErrUnauthorized
	}
	slog.Info("KOReader authenticate succeeded",
		"username", creds.Username,
		"account_id", account.ID,
		"client_key_prefix", keyPreview(creds.Key),
	)
	return account, nil
}

func (s *Service) SaveProgress(ctx context.Context, creds Credentials, payload ProgressPayload) (SyncResult, error) {
	account, err := s.Authenticate(ctx, creds)
	if err != nil {
		return SyncResult{}, err
	}
	_ = account

	payload.Document = strings.TrimSpace(payload.Document)
	payload.Progress = strings.TrimSpace(payload.Progress)
	payload.Device = strings.TrimSpace(payload.Device)
	payload.DeviceID = strings.TrimSpace(payload.DeviceID)
	if payload.Document == "" || payload.Progress == "" || payload.Device == "" || payload.DeviceID == "" {
		return SyncResult{}, fmt.Errorf("invalid progress payload")
	}
	if payload.Percentage < 0 {
		payload.Percentage = 0
	}
	if payload.Percentage > 1 {
		payload.Percentage = 1
	}

	existing, err := s.store.GetKOReaderProgress(ctx, creds.Username, payload.Document)
	if err != nil && err != sql.ErrNoRows {
		return SyncResult{}, err
	}
	nowTS := time.Now().Unix()

	// Do not regress canonical progress.
	if err == nil && existing.Percentage > payload.Percentage {
		return SyncResult{
			Record:  existing,
			Matched: existing.BookID.Valid,
			BookID:  nullableInt64Ptr(existing.BookID),
		}, nil
	}

	var (
		bookID    sql.NullInt64
		matchedBy string
	)
	matchConfig := s.currentMatchConfig()
	documentKey := normalizeDocumentForMatch(payload.Document, matchConfig)
	if match, matchErr := s.store.FindBookByDocumentFingerprint(ctx, documentKey, matchConfig.MatchMode, matchConfig.PathIgnoreExtension); matchErr == nil {
		bookID = sql.NullInt64{Int64: match.BookID, Valid: true}
		matchedBy = match.MatchedBy
		if applyErr := s.applyBookProgress(ctx, match, payload.Percentage); applyErr != nil {
			slog.Warn("Failed to project KOReader progress onto book", "book_id", match.BookID, "error", applyErr)
		}
	}

	rawPayload, _ := json.Marshal(payload)
	record, err := s.store.UpsertKOReaderProgress(ctx, database.UpsertKOReaderProgressParams{
		Username:   creds.Username,
		Document:   payload.Document,
		Progress:   payload.Progress,
		Percentage: payload.Percentage,
		Device:     payload.Device,
		DeviceID:   payload.DeviceID,
		BookID:     bookID,
		MatchedBy:  matchedBy,
		Timestamp:  nowTS,
		RawPayload: string(rawPayload),
	})
	if err != nil {
		return SyncResult{}, err
	}

	_ = s.store.CreateKOReaderSyncEvent(ctx, database.CreateKOReaderSyncEventParams{
		Direction: "push",
		Username:  creds.Username,
		Document:  payload.Document,
		BookID:    record.BookID,
		Status:    "ok",
		Message:   matchedBy,
	})

	return SyncResult{
		Record:  record,
		Matched: record.BookID.Valid,
		BookID:  nullableInt64Ptr(record.BookID),
	}, nil
}

func (s *Service) GetProgress(ctx context.Context, creds Credentials, document string) (database.KOReaderProgress, error) {
	if _, err := s.Authenticate(ctx, creds); err != nil {
		return database.KOReaderProgress{}, err
	}

	record, err := s.store.GetKOReaderProgress(ctx, creds.Username, strings.TrimSpace(document))
	if err != nil {
		if err == sql.ErrNoRows {
			return database.KOReaderProgress{}, ErrProgressNotFound
		}
		return database.KOReaderProgress{}, err
	}

	if !record.BookID.Valid {
		matchConfig := s.currentMatchConfig()
		documentKey := normalizeDocumentForMatch(record.Document, matchConfig)
		if match, matchErr := s.store.FindBookByDocumentFingerprint(ctx, documentKey, matchConfig.MatchMode, matchConfig.PathIgnoreExtension); matchErr == nil {
			_ = s.store.LinkKOReaderProgressToBook(ctx, record.ID, match.BookID, match.MatchedBy)
			record.BookID = sql.NullInt64{Int64: match.BookID, Valid: true}
			record.MatchedBy = match.MatchedBy
			if applyErr := s.applyBookProgress(ctx, match, record.Percentage); applyErr != nil {
				slog.Warn("Failed to project KOReader pull progress onto book", "book_id", match.BookID, "error", applyErr)
			}
		}
	}

	_ = s.store.CreateKOReaderSyncEvent(ctx, database.CreateKOReaderSyncEventParams{
		Direction: "pull",
		Username:  creds.Username,
		Document:  record.Document,
		BookID:    record.BookID,
		Status:    "ok",
		Message:   record.MatchedBy,
	})

	return record, nil
}

func (s *Service) RebuildBookIdentities(ctx context.Context, limit int, progress func(current, total int, message string)) (int, int, error) {
	if limit <= 0 {
		limit = 500
	}
	matchConfig := s.currentMatchConfig()
	missingCount, err := s.store.CountBooksMissingIdentity(ctx, matchConfig.MatchMode)
	if err != nil {
		return 0, 0, err
	}

	total := int(missingCount)
	updated := 0
	var afterID int64
	for {
		books, err := s.store.ListBooksMissingIdentityBatch(ctx, matchConfig.MatchMode, afterID, limit)
		if err != nil {
			return updated, total, err
		}
		if len(books) == 0 {
			break
		}

		for _, book := range books {
			params := database.UpdateBookIdentityParams{ID: book.ID}
			if matchConfig.MatchMode == config.KOReaderMatchModeBinaryHash {
				fileHash, err := FingerprintFile(book.Path)
				if err != nil {
					slog.Warn("Failed to fingerprint book", "book_id", book.ID, "path", book.Path, "error", err)
					afterID = book.ID
					continue
				}
				params.FileHash = fileHash
			} else {
				params.PathFingerprint = FingerprintRelativePath(book.LibraryPath, book.Path, false)
				params.PathFingerprintNoExt = FingerprintRelativePath(book.LibraryPath, book.Path, true)
			}

			if err := s.store.UpdateBookIdentity(ctx, params); err != nil {
				return updated, total, err
			}

			updated++
			afterID = book.ID
			if progress != nil {
				progress(updated, total, fmt.Sprintf("已重建 %d / %d 本书籍的 KOReader %s索引", updated, total, readableMatchMode(matchConfig)))
			}
		}
	}
	return updated, total, nil
}

func (s *Service) ReconcileProgress(ctx context.Context, limit int, progress func(current, total int, message string)) (int, int, error) {
	if limit <= 0 {
		limit = 500
	}
	unmatchedCount, err := s.store.CountUnmatchedKOReaderProgress(ctx)
	if err != nil {
		return 0, 0, err
	}

	total := int(unmatchedCount)
	updated := 0
	processed := 0
	var afterID int64
	for {
		items, err := s.store.ListUnmatchedKOReaderProgressBatch(ctx, afterID, limit)
		if err != nil {
			return updated, total, err
		}
		if len(items) == 0 {
			break
		}

		for _, item := range items {
			matchConfig := s.currentMatchConfig()
			documentKey := normalizeDocumentForMatch(item.Document, matchConfig)
			match, matchErr := s.store.FindBookByDocumentFingerprint(ctx, documentKey, matchConfig.MatchMode, matchConfig.PathIgnoreExtension)
			if matchErr == nil {
				if err := s.store.LinkKOReaderProgressToBook(ctx, item.ID, match.BookID, match.MatchedBy); err != nil {
					return updated, total, err
				}
				if applyErr := s.applyBookProgress(ctx, match, item.Percentage); applyErr != nil {
					slog.Warn("Failed to project reconciled progress onto book", "book_id", match.BookID, "error", applyErr)
				}
				updated++
			}
			processed++
			afterID = item.ID
			if progress != nil {
				progress(processed, total, fmt.Sprintf("已处理 %d / %d 条 KOReader 同步记录", processed, total))
			}
		}
	}
	return updated, total, nil
}

func (s *Service) ListAccounts(ctx context.Context) ([]database.KOReaderAccount, error) {
	return s.store.ListKOReaderAccounts(ctx)
}

func (s *Service) CreateAccount(ctx context.Context, username string) (database.KOReaderAccount, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return database.KOReaderAccount{}, ErrUnauthorized
	}
	if _, err := s.store.GetKOReaderAccountByUsername(ctx, username); err == nil {
		return database.KOReaderAccount{}, ErrAlreadyConfigured
	} else if err != nil && err != sql.ErrNoRows {
		return database.KOReaderAccount{}, err
	}
	syncKey, err := GenerateSyncKey()
	if err != nil {
		return database.KOReaderAccount{}, err
	}
	return s.store.CreateKOReaderAccount(ctx, database.CreateKOReaderAccountParams{
		Username: username,
		SyncKey:  syncKey,
		Enabled:  true,
	})
}

func (s *Service) RotateAccountKey(ctx context.Context, id int64) (database.KOReaderAccount, error) {
	if _, err := s.store.GetKOReaderAccountByID(ctx, id); err != nil {
		if err == sql.ErrNoRows {
			return database.KOReaderAccount{}, ErrAccountNotFound
		}
		return database.KOReaderAccount{}, err
	}
	syncKey, err := GenerateSyncKey()
	if err != nil {
		return database.KOReaderAccount{}, err
	}
	return s.store.RotateKOReaderAccountKey(ctx, id, syncKey)
}

func (s *Service) SetAccountEnabled(ctx context.Context, id int64, enabled bool) (database.KOReaderAccount, error) {
	account, err := s.store.SetKOReaderAccountEnabled(ctx, id, enabled)
	if err != nil {
		if err == sql.ErrNoRows {
			return database.KOReaderAccount{}, ErrAccountNotFound
		}
		return database.KOReaderAccount{}, err
	}
	return account, nil
}

func (s *Service) DeleteAccount(ctx context.Context, id int64) error {
	if _, err := s.store.GetKOReaderAccountByID(ctx, id); err != nil {
		if err == sql.ErrNoRows {
			return ErrAccountNotFound
		}
		return err
	}
	return s.store.DeleteKOReaderAccount(ctx, id)
}

func GenerateSyncKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func keyPreview(value string) string {
	value = NormalizeSyncKey(value)
	if value == "" {
		return "<empty>"
	}
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

type MatchConfig struct {
	MatchMode           string
	PathIgnoreExtension bool
}

func (s *Service) currentMatchConfig() MatchConfig {
	if s.cfg == nil {
		return MatchConfig{MatchMode: config.KOReaderMatchModeBinaryHash}
	}
	current := s.cfg.Snapshot()
	return MatchConfig{
		MatchMode:           current.KOReader.MatchMode,
		PathIgnoreExtension: current.KOReader.PathIgnoreExtension,
	}
}

func normalizeDocumentForMatch(document string, cfg MatchConfig) string {
	document = strings.TrimSpace(document)
	if document == "" {
		return ""
	}
	if cfg.MatchMode == config.KOReaderMatchModeFilePath {
		return FingerprintDocumentPath(document, cfg.PathIgnoreExtension)
	}
	return strings.ToLower(document)
}

func NormalizeDocumentForMatch(document string, matchMode string, pathIgnoreExtension bool) string {
	return normalizeDocumentForMatch(document, MatchConfig{
		MatchMode:           matchMode,
		PathIgnoreExtension: pathIgnoreExtension,
	})
}

func (s *Service) IndexedBookCount(ctx context.Context) (int64, error) {
	stats, err := s.store.GetKOReaderStats(ctx)
	if err != nil {
		return 0, err
	}
	matchConfig := s.currentMatchConfig()
	missingCount, err := s.store.CountBooksMissingIdentity(ctx, matchConfig.MatchMode)
	if err != nil {
		return 0, err
	}
	indexed := stats.TotalBooks - missingCount
	if indexed < 0 {
		indexed = 0
	}
	return indexed, nil
}

func readableMatchMode(cfg MatchConfig) string {
	if cfg.MatchMode == config.KOReaderMatchModeFilePath {
		return "路径"
	}
	return "二进制哈希"
}

func (s *Service) applyBookProgress(ctx context.Context, match database.KOReaderBookMatch, percentage float64) error {
	if match.PageCount <= 0 {
		return nil
	}
	page := int64(float64(match.PageCount) * percentage)
	if page < 1 {
		page = 1
	}
	if page > match.PageCount {
		page = match.PageCount
	}
	if match.LastReadPage != nil && page < *match.LastReadPage {
		return nil
	}
	if err := s.store.UpdateBookProgress(ctx, database.UpdateBookProgressParams{
		ID:           match.BookID,
		LastReadPage: sql.NullInt64{Int64: page, Valid: true},
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
	}); err != nil {
		return err
	}
	return s.store.LogReadingActivity(ctx, match.BookID, int(page))
}

func nullableInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	value := v.Int64
	return &value
}
