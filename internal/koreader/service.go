package koreader

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
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
	"manga-manager/internal/taskcontrol"
)

// kosync 进度载荷各字段的长度上限。设备侧本就只会推送文件路径/百分比串/设备名，
// 这些取值远小于上限；设限是为了挡住被改造过的客户端把单条记录撑到任意大小
// （document 还会进 UNIQUE 索引，撑大后连索引一起膨胀）。
const (
	maxProgressDocumentLen = 512
	maxProgressValueLen    = 512
	maxProgressDeviceLen   = 128
	// maxRegisterUsernameLen 限制设备自助注册的用户名长度。同理由：该路径未鉴权，
	// 而 username 会进 koreader_accounts 的 UNIQUE 索引。
	maxRegisterUsernameLen = 128
)

var (
	ErrUnauthorized      = errors.New("unauthorized")
	ErrForbidden         = errors.New("forbidden")
	ErrAlreadyConfigured = errors.New("account already configured")
	ErrAccountNotFound   = errors.New("account not found")
	ErrProgressNotFound  = errors.New("progress not found")
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

// RegisterDevice 实现 kosync /users/create 设备自助注册。KOReader 客户端提交的 password
// 已经是用户密钥的 MD5 十六进制串（与后续请求头 x-auth-key 完全同值），服务端拿不到原始
// 密钥，因此这里直接把该 MD5 作为 sync_key 存储；Authenticate 对这类“已是客户端哈希”的
// sync_key 会做等值比对，从而让新注册设备立即可认证。用户名已存在时返回 ErrAlreadyConfigured。
func (s *Service) RegisterDevice(ctx context.Context, username, passwordHash string) (database.KOReaderAccount, error) {
	username = strings.TrimSpace(username)
	passwordHash = NormalizeSyncKey(passwordHash)
	// 这条路径未鉴权，写入的两个值直接落库且 username 进 UNIQUE 索引，所以先把形状卡死：
	// kosync 协议里 password 就是 md5 十六进制串，必须显式过 IsValidSyncKey——不校验的话
	// 任意字符串都能被当作密钥存进去。
	if username == "" || len(username) > maxRegisterUsernameLen || !IsValidSyncKey(passwordHash) {
		slog.Warn("KOReader device registration rejected: malformed credentials",
			// 不打印 username 原文：未鉴权输入不应原样进日志。
			"username_length", len(username),
			"password_hash_length", len(passwordHash),
		)
		return database.KOReaderAccount{}, ErrUnauthorized
	}
	if _, err := s.store.GetKOReaderAccountByUsername(ctx, username); err == nil {
		return database.KOReaderAccount{}, ErrAlreadyConfigured
	} else if !errors.Is(err, sql.ErrNoRows) {
		return database.KOReaderAccount{}, err
	}
	account, err := s.store.CreateKOReaderAccount(ctx, database.CreateKOReaderAccountParams{
		Username: username,
		SyncKey:  passwordHash,
		Enabled:  true,
	})
	if err != nil {
		return database.KOReaderAccount{}, err
	}
	// 设备自助注册没有站点用户上下文，账号归属只能靠猜。站点只有一个用户时这个猜测无歧义；
	// 一旦有第二个用户，把账号挂到「首个管理员」名下就是纯粹的错误归属——SaveProgress 会顺着
	// 账户的 user_id 去写 user_book_progress 与 user_reading_activity，于是普通用户拿设备
	// 自助注册之后，他读到哪管理员的进度就跳到哪，而他自己账号下的进度全丢。
	// 因此多用户站点一律留 user_id=0（进度回落全局），由管理员在设置里显式指派。
	assigned := false
	userCount, countErr := s.store.CountUsers(ctx)
	if countErr != nil {
		slog.Warn("Failed to count site users for self-registered KOReader account",
			"account_id", account.ID, "error", countErr)
	} else if userCount == 1 {
		if adminID, e := s.store.FirstAdminUserID(ctx); e == nil {
			if e := s.store.SetKOReaderAccountUser(ctx, account.ID, adminID); e != nil {
				slog.Warn("Failed to assign self-registered KOReader account to admin",
					"account_id", account.ID, "error", e)
			} else {
				assigned = true
			}
		}
	}
	if !assigned {
		// 未归属的账号其同步进度回落全局，任何站点用户都看不到。这里落一条 direction="system"
		// 的审计行仅供事后查库定位——注意站内目前**没有任何界面会显示它**：四处消费
		// koreader_sync_events 的查询都带 `direction != 'system'`。
		_ = s.store.CreateKOReaderSyncEvent(ctx, database.CreateKOReaderSyncEventParams{
			Direction: "system",
			Username:  account.Username,
			Status:    "account_unassigned",
			Message:   "设备自助注册的账号未关联站点用户，其同步进度不计入任何用户，请在设置中指派",
		})
	}
	return account, nil
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
		if errors.Is(err, sql.ErrNoRows) {
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
	// 管理端创建的账号：sync_key 存的是原始密钥，客户端发送 md5(sync_key)，比对 HashKey(sync_key)。
	// 设备自助注册的账号：sync_key 存的就是客户端提交的 md5（与 x-auth-key 同值），再做等值比对。
	//
	// 两次比较都走 crypto/subtle 且**都必须求值**（用位或而非 || 短路）：这三个 kosync 端点全部未鉴权、
	// 无失败限流也无锁定，creds.Key 完全由请求头控制，正是逐字节计时探测最理想的条件。
	// Go 的字符串 != 在首个不同字节处短路，攻击者据此可以一位一位地把密钥试出来。
	//
	// 长度不等时 ConstantTimeCompare 直接返 0：泄漏的是服务端存储值的长度而不是内容
	// （expectedKey 恒为 32 位 hex；storedKey 是库里的原始值，历史数据可能是任意长度），可以接受，
	// 不必为此额外做 padding。
	expectedKey := HashKey(account.SyncKey)
	storedKey := NormalizeSyncKey(account.SyncKey)
	matchesExpected := subtle.ConstantTimeCompare([]byte(expectedKey), []byte(creds.Key))
	matchesStored := subtle.ConstantTimeCompare([]byte(storedKey), []byte(creds.Key))
	if matchesExpected|matchesStored != 1 {
		// 刻意不打印 expected_key_prefix：那是**正确答案**的前 32 bit，而这条分支任何未鉴权
		// 请求都能触发。把在线时序通道堵上、却留着这条可任意触发的离线通道，等于没修。
		slog.Warn("KOReader authenticate rejected: client key mismatch",
			"username", creds.Username,
			"account_id", account.ID,
			"stored_raw_key_length", len(account.SyncKey),
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

// validateProgressPayloadLengths 校验进度载荷各字段的长度。
// 这些字段直接落库（document 还进 UNIQUE 索引），必须限长：不限长的话，
// 一台被改造过的设备可以把单条记录撑到任意大小，反复推送即可撑爆数据库。
func validateProgressPayloadLengths(payload ProgressPayload) error {
	switch {
	case len(payload.Document) > maxProgressDocumentLen:
		return fmt.Errorf("document exceeds %d bytes", maxProgressDocumentLen)
	case len(payload.Progress) > maxProgressValueLen:
		return fmt.Errorf("progress exceeds %d bytes", maxProgressValueLen)
	case len(payload.Device) > maxProgressDeviceLen:
		return fmt.Errorf("device exceeds %d bytes", maxProgressDeviceLen)
	case len(payload.DeviceID) > maxProgressDeviceLen:
		return fmt.Errorf("device_id exceeds %d bytes", maxProgressDeviceLen)
	}
	return nil
}

func (s *Service) SaveProgress(ctx context.Context, creds Credentials, payload ProgressPayload) (SyncResult, error) {
	account, err := s.Authenticate(ctx, creds)
	if err != nil {
		return SyncResult{}, err
	}
	_ = account
	// KOReader 账户绑定的站点用户（多用户）；>0 时同步进度写入该用户，0 回落全局。
	syncUserID, _ := s.store.GetKOReaderAccountUserID(ctx, creds.Username)

	payload.Document = strings.TrimSpace(payload.Document)
	payload.Progress = strings.TrimSpace(payload.Progress)
	payload.Device = strings.TrimSpace(payload.Device)
	payload.DeviceID = strings.TrimSpace(payload.DeviceID)
	if payload.Document == "" || payload.Progress == "" || payload.Device == "" || payload.DeviceID == "" {
		return SyncResult{}, fmt.Errorf("invalid progress payload")
	}
	if err := validateProgressPayloadLengths(payload); err != nil {
		return SyncResult{}, err
	}
	if payload.Percentage < 0 {
		payload.Percentage = 0
	}
	if payload.Percentage > 1 {
		payload.Percentage = 1
	}

	existing, err := s.store.GetKOReaderProgress(ctx, creds.Username, payload.Document)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SyncResult{}, err
	}
	nowTS := time.Now().Unix()

	// kosync semantics are last-write-wins: the most recent push always wins,
	// even when its percentage is lower than the stored value (rollback / re-read).
	// The kosync payload carries no client timestamp, so the server receive time
	// (nowTS) is authoritative and every fresh push supersedes the stored record.
	// We still flag pushes that move progress backward so the diagnostics view can
	// distinguish intentional rollbacks from unexpected regressions.
	regressed := err == nil && existing.Percentage > payload.Percentage

	var (
		bookID    sql.NullInt64
		matchedBy string
	)
	matchConfig := s.currentMatchConfig()
	documentKey := normalizeDocumentForMatch(payload.Document, matchConfig)
	if match, matchErr := s.store.FindBookByDocumentFingerprint(ctx, documentKey, matchConfig.MatchMode, matchConfig.PathIgnoreExtension); matchErr == nil {
		bookID = sql.NullInt64{Int64: match.BookID, Valid: true}
		matchedBy = match.MatchedBy
		if applyErr := s.applyBookProgress(ctx, match, payload.Percentage, syncUserID); applyErr != nil {
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

	status := "ok"
	message := matchedBy
	if regressed {
		status = "progress_regressed"
		message = fmt.Sprintf("last-write-wins applied lower percentage %.4f (was %.4f) from device %q", payload.Percentage, existing.Percentage, payload.Device)
	}
	_ = s.store.CreateKOReaderSyncEvent(ctx, database.CreateKOReaderSyncEventParams{
		Direction: "push",
		Username:  creds.Username,
		Document:  payload.Document,
		BookID:    record.BookID,
		Status:    status,
		Message:   message,
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
		if errors.Is(err, sql.ErrNoRows) {
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
			pullUserID, _ := s.store.GetKOReaderAccountUserID(ctx, record.Username)
			if applyErr := s.applyBookProgress(ctx, match, record.Percentage, pullUserID); applyErr != nil {
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

// ResetProgress deletes a single koreader_progress row by id so an operator can
// clear a stale or wrong canonical record from the admin UI. It returns the
// deleted record (for the diagnostic event) and ErrProgressNotFound when the id
// does not exist.
func (s *Service) ResetProgress(ctx context.Context, id int64) (database.KOReaderProgress, error) {
	record, err := s.store.DeleteKOReaderProgress(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.KOReaderProgress{}, ErrProgressNotFound
		}
		return database.KOReaderProgress{}, err
	}
	_ = s.store.CreateKOReaderSyncEvent(ctx, database.CreateKOReaderSyncEventParams{
		Direction: "system",
		Username:  record.Username,
		Document:  record.Document,
		BookID:    record.BookID,
		Status:    "progress_reset",
		Message:   "KOReader 进度记录已被管理员重置",
	})
	return record, nil
}

func (s *Service) RebuildBookIdentities(ctx context.Context, limit int, progress func(current, total int)) (int, int, error) {
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
		if err := taskcontrol.Wait(ctx); err != nil {
			return updated, total, err
		}
		books, err := s.store.ListBooksMissingIdentityBatch(ctx, matchConfig.MatchMode, afterID, limit)
		if err != nil {
			return updated, total, err
		}
		if len(books) == 0 {
			break
		}

		for _, book := range books {
			if err := taskcontrol.Wait(ctx); err != nil {
				return updated, total, err
			}
			params := database.UpdateBookIdentityParams{ID: book.ID}
			if matchConfig.MatchMode == config.KOReaderMatchModeBinaryHash {
				fileHash, err := FingerprintFileContext(ctx, book.Path)
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
				progress(updated, total)
			}
		}
	}
	return updated, total, nil
}

func (s *Service) ReconcileProgress(ctx context.Context, limit int, progress func(current, total int)) (int, int, error) {
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
		if err := taskcontrol.Wait(ctx); err != nil {
			return updated, total, err
		}
		items, err := s.store.ListUnmatchedKOReaderProgressBatch(ctx, afterID, limit)
		if err != nil {
			return updated, total, err
		}
		if len(items) == 0 {
			break
		}

		for _, item := range items {
			if err := taskcontrol.Wait(ctx); err != nil {
				return updated, total, err
			}
			matchConfig := s.currentMatchConfig()
			documentKey := normalizeDocumentForMatch(item.Document, matchConfig)
			match, matchErr := s.store.FindBookByDocumentFingerprint(ctx, documentKey, matchConfig.MatchMode, matchConfig.PathIgnoreExtension)
			if matchErr == nil {
				if err := s.store.LinkKOReaderProgressToBook(ctx, item.ID, match.BookID, match.MatchedBy); err != nil {
					return updated, total, err
				}
				reconcileUserID, _ := s.store.GetKOReaderAccountUserID(ctx, item.Username)
				if applyErr := s.applyBookProgress(ctx, match, item.Percentage, reconcileUserID); applyErr != nil {
					slog.Warn("Failed to project reconciled progress onto book", "book_id", match.BookID, "error", applyErr)
				}
				updated++
			}
			processed++
			afterID = item.ID
			if progress != nil {
				progress(processed, total)
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
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
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
		if errors.Is(err, sql.ErrNoRows) {
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
		if errors.Is(err, sql.ErrNoRows) {
			return database.KOReaderAccount{}, ErrAccountNotFound
		}
		return database.KOReaderAccount{}, err
	}
	return account, nil
}

func (s *Service) DeleteAccount(ctx context.Context, id int64) error {
	if _, err := s.store.GetKOReaderAccountByID(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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

// applyBookProgress 把 KOReader 同步的百分比投影为书页进度。userID>0 时写入该站点用户的每用户进度，
// 否则回落到旧的全局进度（未关联账户 / 首启）。「不回退」保护按对应用户（或全局）的既有进度比较。
func (s *Service) applyBookProgress(ctx context.Context, match database.KOReaderBookMatch, percentage float64, userID int64) error {
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

	prev := int64(0)
	if userID > 0 {
		if p, ok, err := s.store.GetUserBookProgress(ctx, userID, match.BookID); err == nil && ok && p.LastReadPage.Valid {
			prev = p.LastReadPage.Int64
		}
	} else if match.LastReadPage != nil {
		prev = *match.LastReadPage
	}
	if prev > 0 && page < prev {
		return nil
	}

	now := time.Now()
	if userID > 0 {
		if err := s.store.SetUserBookProgress(ctx, userID, match.BookID, page, now); err != nil {
			return err
		}
	} else if err := s.store.UpdateBookProgress(ctx, database.UpdateBookProgressParams{
		ID:           match.BookID,
		LastReadPage: sql.NullInt64{Int64: page, Valid: true},
		LastReadAt:   sql.NullTime{Time: now, Valid: true},
	}); err != nil {
		return err
	}
	// 全局活动始终记；已关联站点用户时同时记每用户活动，使 KOReader 阅读也计入连续天数/热力图/年度回顾。
	if err := s.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: match.BookID, PagesRead: page, Date: database.ActivityDayKey(time.Now())}); err != nil {
		return err
	}
	if userID > 0 {
		return s.store.LogUserReadingActivity(ctx, userID, match.BookID, page)
	}
	return nil
}

func nullableInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	value := v.Int64
	return &value
}
