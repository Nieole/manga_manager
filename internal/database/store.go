// 业务说明：本文件是业务实现，属于 SQLite 数据访问层，负责把漫画库、系列、阅读进度、任务和元数据状态持久化为稳定数据模型。
// 它连接 sqlc 生成查询与上层领域服务，是资料库筛选、搜索同步和关系图谱的数据基础。
// 维护时应保持 schema、查询定义、事务边界和迁移兼容，避免破坏既有用户数据。

package database

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaCoreSQL string

//go:embed schema_handwritten.sql
var schemaHandwrittenSQL string

// schemaSQL 合并核心 schema 与「仅手写 SQL 访问」的表 schema（series_custom_fields、多用户/每用户进度/
// 深度统计等）。后者刻意不放进 schema.sql：它们不需 sqlc 查询，而 sqlc 会为其表生成与手写业务结构
// （SeriesCustomField/User/Session/UserBookProgress/UserSeriesReview 等）重名的模型，造成编译冲突与
// models.go 漂移。sqlc.yaml 只读 schema.sql，故拆分即可规避；Migrate 执行本合并串，建表行为不变。
var schemaSQL = schemaCoreSQL + "\n" + schemaHandwrittenSQL

type Store interface {
	Querier
	Close() error
	// PingContext 校验底层数据库连接可用，供健康检查等存活/就绪探测使用。
	PingContext(ctx context.Context) error
	// Store 是 sqlc 生成查询之上的领域边界：Controller 和 Scanner 只依赖这里暴露的业务操作。
	// 新增方法时应优先判断它是“通用 SQL 查询”还是“跨表业务动作”，前者放 query.sql，后者在 Store 中封装事务。
	ListExternalLibraryBooksByLibrary(ctx context.Context, libraryID int64) ([]ExternalLibraryBookRow, error)
	UpdateSeriesMetadata(ctx context.Context, arg UpdateSeriesMetadataParams) (Series, error)
	ExecTx(ctx context.Context, fn func(*Queries) error) error
	SearchSeriesPaged(ctx context.Context, libraryID int64, f SeriesListFilters, limit, offset int32, sortBy string) ([]SearchSeriesPagedRow, int, error)
	SearchSeriesCursor(ctx context.Context, libraryID int64, f SeriesListFilters, limit int32, sortBy, cursor string) ([]SearchSeriesPagedRow, string, bool, error)
	SearchGlobalSeries(ctx context.Context, keyword string, limit int32) ([]SeriesSearchHit, error)
	SearchGlobalBooks(ctx context.Context, keyword string, limit int32) ([]BookSearchHit, error)
	SearchProtocolSeries(ctx context.Context, keyword string, limit, offset int32) ([]ProtocolSeriesRow, int, error)
	RebuildSeriesSearchIndex(ctx context.Context) error
	RebuildBookSearchIndex(ctx context.Context) error
	GetDashboardStats(ctx context.Context) (*DashboardStats, error)
	GetDashboardStructuralStats(ctx context.Context) (*DashboardStructuralStats, error)
	GetDashboardVolatileStats(ctx context.Context) (*DashboardVolatileStats, error)
	ListProtocolSeriesByIDs(ctx context.Context, ids []int64) ([]ProtocolSeriesRow, error)
	SearchTags(ctx context.Context, query string, limit int) ([]Tag, error)
	SearchAuthors(ctx context.Context, query string, limit int) ([]Author, error)
	BulkEditSeries(ctx context.Context, seriesIDs []int64, edit BulkSeriesEdit) error
	RenameTag(ctx context.Context, tagID int64, newName string) error
	MergeTags(ctx context.Context, sourceID, targetID int64) error
	DeleteTag(ctx context.Context, tagID int64) error
	SetBookCover(ctx context.Context, bookID int64, coverPath string) error
	ListSeriesCustomFields(ctx context.Context, seriesID int64) ([]SeriesCustomField, error)
	ReplaceSeriesCustomFields(ctx context.Context, seriesID int64, fields []SeriesCustomField) error
	FindDuplicateBooks(ctx context.Context) ([]DuplicateBookRow, error)
	UpsertTask(ctx context.Context, task TaskRecord) error
	ListTasks(ctx context.Context, filters TaskFilters) ([]TaskRecord, error)
	DeleteTasks(ctx context.Context, filters TaskFilters) (int64, error)
	GetHealthReport(ctx context.Context, filters HealthIssueFilters) (HealthReport, error)
	GetKOReaderSettings(ctx context.Context) (KOReaderSettings, error)
	UpsertKOReaderSettings(ctx context.Context, arg UpsertKOReaderSettingsParams) (KOReaderSettings, error)
	ListKOReaderAccounts(ctx context.Context) ([]KOReaderAccount, error)
	CreateKOReaderAccount(ctx context.Context, arg CreateKOReaderAccountParams) (KOReaderAccount, error)
	GetKOReaderAccountByUsername(ctx context.Context, username string) (KOReaderAccount, error)
	GetKOReaderAccountByID(ctx context.Context, id int64) (KOReaderAccount, error)
	RotateKOReaderAccountKey(ctx context.Context, id int64, syncKey string) (KOReaderAccount, error)
	SetKOReaderAccountEnabled(ctx context.Context, id int64, enabled bool) (KOReaderAccount, error)
	DeleteKOReaderAccount(ctx context.Context, id int64) error
	GetKOReaderStats(ctx context.Context) (KOReaderStats, error)
	GetLatestKOReaderFailure(ctx context.Context) (KOReaderSyncEvent, error)
	BackfillSeriesInitials(ctx context.Context) error
	ListKOReaderDeviceDiagnostics(ctx context.Context) ([]KOReaderDeviceDiagnostic, error)
	ListKOReaderDeviceMatchMethods(ctx context.Context) ([]KOReaderDeviceMatchMethod, error)
	ListKOReaderDeviceConflicts(ctx context.Context, limit int) ([]KOReaderDeviceConflict, error)
	CountBooksMissingIdentity(ctx context.Context, matchMode string) (int64, error)
	CountUnmatchedKOReaderProgress(ctx context.Context) (int64, error)
	FindBookByDocumentFingerprint(ctx context.Context, documentKey, matchMode string, pathIgnoreExtension bool) (KOReaderBookMatch, error)
	UpsertKOReaderProgress(ctx context.Context, arg UpsertKOReaderProgressParams) (KOReaderProgress, error)
	GetKOReaderProgress(ctx context.Context, username, document string) (KOReaderProgress, error)
	DeleteKOReaderProgress(ctx context.Context, id int64) (KOReaderProgress, error)
	ListBooksMissingIdentityBatch(ctx context.Context, matchMode string, afterID int64, limit int) ([]BookIdentityCandidate, error)
	CountBooksMissingQuickHash(ctx context.Context) (int64, error)
	ListBooksMissingQuickHashBatch(ctx context.Context, afterID int64, limit int) ([]BookIdentityCandidate, error)
	UpdateBookIdentity(ctx context.Context, arg UpdateBookIdentityParams) error
	ListUnmatchedKOReaderProgress(ctx context.Context, limit int) ([]KOReaderProgress, error)
	ListUnmatchedKOReaderProgressBatch(ctx context.Context, afterID int64, limit int) ([]KOReaderProgress, error)
	LinkKOReaderProgressToBook(ctx context.Context, progressID, bookID int64, matchedBy string) error
	CreateKOReaderSyncEvent(ctx context.Context, arg CreateKOReaderSyncEventParams) error
	GetReadingListItemProgress(ctx context.Context, readingListID, userID int64) (map[int64]ReadingListSeriesProgress, error)
	SearchSmartCollectionSeries(ctx context.Context, filter SmartCollectionFilter, limit, offset int) ([]SearchSeriesPagedRow, int, error)
	// 站点账户体系（多用户）——用户与会话存储，见 users.go。
	CountUsers(ctx context.Context) (int64, error)
	CountAdmins(ctx context.Context) (int64, error)
	FirstAdminUserID(ctx context.Context) (int64, error)
	CreateUser(ctx context.Context, arg CreateUserParams) (User, error)
	GetUserByID(ctx context.Context, id int64) (User, error)
	GetUserByUsername(ctx context.Context, username string) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
	UpdateUserPassword(ctx context.Context, id int64, passwordHash string, mustChange bool) error
	UpdateUserProfile(ctx context.Context, id int64, displayName, role string) error
	DeleteUser(ctx context.Context, id int64) error
	CreateSession(ctx context.Context, sess Session) error
	GetSessionWithUser(ctx context.Context, id string, now time.Time) (Session, User, error)
	TouchSession(ctx context.Context, id string, lastSeen, expiresAt time.Time) error
	DeleteSession(ctx context.Context, id string) error
	DeleteSessionsForUser(ctx context.Context, userID int64) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
	// 每用户阅读进度（多用户阶段2）——见 user_progress.go。
	SetUserBookProgress(ctx context.Context, userID, bookID, page int64, at time.Time) error
	ClearUserBookProgress(ctx context.Context, userID, bookID int64) error
	SetUserBooksReadState(ctx context.Context, userID int64, bookIDs []int64, isRead bool, at time.Time) error
	GetUserBookProgress(ctx context.Context, userID, bookID int64) (UserBookProgress, bool, error)
	GetUserBookProgressMap(ctx context.Context, userID int64, bookIDs []int64) (map[int64]UserBookProgress, error)
	GetUserRecentReadAll(ctx context.Context, userID, limit int64) ([]GetRecentReadAllRow, error)
	GetUserRecentReadSeries(ctx context.Context, userID, libraryID, limit int64) ([]GetRecentReadSeriesRow, error)
	ListUserReadingListItems(ctx context.Context, userID, readingListID int64) ([]ListReadingListItemsRow, error)
	RefreshUserSeriesProgressForAllUsers(ctx context.Context, seriesID int64) error
	GetUserTopReadingTags(ctx context.Context, userID, limit int64) ([]GetTopReadingTagsRow, error)
	// RefreshSeriesDerivedData 重算系列的全部派生数据（冗余统计 + series_stats + 每用户聚合），
	// 供「系列组成发生变化」的调用方（删书、批量移除）统一收口。
	RefreshSeriesDerivedData(ctx context.Context, seriesID int64) error
	// ForEachReferencedCoverPath 流式遍历被引用的封面路径（见 cover_paths.go 的契约注释）。
	ForEachReferencedCoverPath(ctx context.Context, fn func(path string) error) error
	// SampleCandidateSeriesForAI 随机取候选系列供 AI 推荐（见 ai_candidates.go）。
	SampleCandidateSeriesForAI(ctx context.Context, userID int64, limit int64) ([]CandidateSeriesForAI, error)
	// CountSmartCollectionSeries 与智能书架列表共用同一份查询，保证计数与列表口径一致。
	CountSmartCollectionSeries(ctx context.Context, filter SmartCollectionFilter) (int, error)
	ListSmartFilters(ctx context.Context) ([]SmartFilter, error)
	GetUserReadBooksCount(ctx context.Context, userID int64) (int64, error)
	MigrateGlobalProgressToUser(ctx context.Context, userID int64) error
	GetKOReaderAccountUserID(ctx context.Context, username string) (int64, error)
	SetKOReaderAccountUser(ctx context.Context, accountID, userID int64) error
	AssignOrphanKOReaderAccountsToUser(ctx context.Context, userID int64) error
	// 深度统计（第 6 项）——见 user_stats.go。
	LogUserReadingActivity(ctx context.Context, userID, bookID, pages int64) error
	GetUserActivityHeatmap(ctx context.Context, userID int64, offsetClause string) ([]ActivityDay, error)
	GetUserReadingStreak(ctx context.Context, userID int64) (current, longest int, err error)
	AddUserBookReadingTime(ctx context.Context, userID, bookID, seconds int64) error
	GetUserTotalReadingTime(ctx context.Context, userID int64) (int64, error)
	GetUserBookReadingTimeTop(ctx context.Context, userID int64, limit int) ([]BookReadingTimeRow, error)
	GetUserPeriodStats(ctx context.Context, userID int64, year, month int) (UserPeriodStats, error)
	UpsertUserSeriesReview(ctx context.Context, userID, seriesID int64, rating *float64, review string) error
	GetUserSeriesReview(ctx context.Context, userID, seriesID int64) (UserSeriesReview, bool, error)
	DeleteUserSeriesReview(ctx context.Context, userID, seriesID int64) error
	MigrateGlobalActivityToUser(ctx context.Context, userID int64) error
}

type ReadingListSeriesProgress struct {
	SeriesID       int64 `json:"series_id"`
	ReadBooks      int64 `json:"read_books"`
	CompletedBooks int64 `json:"completed_books"`
	TotalBooks     int64 `json:"total_books"`
}

type LibrarySize struct {
	LibraryID   int64  `json:"library_id"`
	LibraryName string `json:"library_name"`
	TotalSize   int64  `json:"total_size"`
}

type SqlStore struct {
	*Queries
	db *sql.DB
}

// DB 返回底层数据库连接（供自定义查询使用）
func (s *SqlStore) DB() *sql.DB {
	return s.db
}

func NewStore(dbPath string) (Store, error) {
	// 针对 100MB 级别的数据库进行精简优化：
	// mmap_size=268435456 (256MB，足以将百兆级数据库整个隐射进内存，消除系统的换页压力)
	// cache_size=-128000  (128MB，页缓存亦完全够用，不需要分配夸张的 500MB 防御性冗余)
	// busy_timeout=15000  (保持长超时，预防因高并发读写引发 sqlite3 busy lock)
	// temp_store=2        (MEMORY：百兆规模下，内存聚合 ORDER 操作极快，保持使用内存)
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// 开启连接池支持
	// 对于现代无并发 CGO 限制的 purely go sqlite，我们设置并行度
	maxConns := runtime.NumCPU() * 2
	if maxConns < 8 {
		maxConns = 8
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns / 2)

	return &SqlStore{
		Queries: New(db),
		db:      db,
	}, nil
}

func (s *SqlStore) Close() error {
	return s.db.Close()
}

func (s *SqlStore) PingContext(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SqlStore) DeleteKOReaderAccount(ctx context.Context, id int64) error {
	return s.ExecTx(ctx, func(q *Queries) error {
		account, err := q.GetKOReaderAccountByID(ctx, id)
		if err != nil {
			return err
		}
		if _, err := q.db.ExecContext(ctx, `DELETE FROM koreader_progress WHERE username = ?`, account.Username); err != nil {
			return err
		}
		if _, err := q.db.ExecContext(ctx, `DELETE FROM koreader_sync_events WHERE username = ?`, account.Username); err != nil {
			return err
		}
		_, err = q.db.ExecContext(ctx, `DELETE FROM koreader_accounts WHERE id = ?`, id)
		return err
	})
}

// ExecTx 提供一个事务包裹器以进行批量执行，这对防止 SQLite 并发锁极为关键
// ExecTx 提供一个事务包裹器以进行批量执行，这对防止 SQLite 并发锁极为关键。
//
// 收尾必须走 defer 而不是只在错误分支回滚：DSN 里的 _txlock=immediate 让 BeginTx 当场就握住
// SQLite 写锁（见 migrate.go 的 sqliteDSN），fn 一旦 panic，事务既不提交也不回滚地悬在那里——
// 连接不还池、写锁不释放，此后整个进程的写入都要等满 busy_timeout 再报 database is locked，
// 只有重启能救。HTTP 路径上尤其隐蔽：middleware.Recoverer 把 panic 吃掉之后服务看着还活着，
// 写入却已经全死了。
//
// 这里刻意**不** recover：defer 在栈展开时本就会执行，回滚不需要 recover 帮忙。
// recover 后重新 panic 会把 traceback 截断在本函数的 defer 帧上，丢掉 fn 内真正的崩溃点；
// 而把 panic 转成 error 更糟——程序 bug 会伪装成一次普通的事务失败，被 scanner 的 flush
// 计入 failedArchives 后继续跑，用「静默的数据丢失」换掉了「响亮的崩溃」。
// panic 该穿透就让它穿透，回滚由 defer 保证。
func (s *SqlStore) ExecTx(ctx context.Context, fn func(*Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// 正常提交或已显式回滚之后再 Rollback 只返回 ErrTxDone，是无害的空操作。
	defer func() { _ = tx.Rollback() }()

	q := s.Queries.WithTx(tx)
	if err := fn(q); err != nil {
		// ctx 被取消时 database/sql 的 awaitDone 已经替我们回滚过，这里的 Rollback 必然返回
		// ErrTxDone，属于正常收尾而非回滚失败，不该污染错误信息。
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			// 两个 %w 而不是 %v：上层要靠 errors.Is 认出原始错误（如 scrape 链路的
			// errNoMetadataChanges），%v 拼接会把错误链拍平成字符串。
			return fmt.Errorf("tx err: %w, rb err: %w", err, rbErr)
		}
		return err
	}
	return tx.Commit()
}

func (s *SqlStore) RefreshSeriesStats(ctx context.Context, seriesID int64) error {
	_, err := s.db.ExecContext(ctx, refreshSeriesStatsStatement("s.id = ?"), seriesID)
	return err
}

// DuplicateBookRow 是重复文件检测返回的单本书信息（按 file_hash 分组，同哈希即内容相同）。
type DuplicateBookRow struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	PageCount  int64  `json:"page_count"`
	FileHash   string `json:"file_hash"`
	SeriesID   int64  `json:"series_id"`
	SeriesName string `json:"series_name"`
}

// FindDuplicateBooks 返回所有 file_hash 相同且出现多次的书籍（按哈希、ID 排序），供去重工作流分组展示。
// 依赖 identity_scan / repair_scan 计算出的 file_hash；未算哈希的书不参与。
func (s *SqlStore) FindDuplicateBooks(ctx context.Context) ([]DuplicateBookRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.name, b.path, b.size, b.page_count, b.file_hash, b.series_id, COALESCE(NULLIF(s.title, ''), s.name) AS series_name
		FROM books b
		JOIN series s ON s.id = b.series_id
		WHERE b.file_hash IS NOT NULL AND b.file_hash != ''
		  AND b.file_hash IN (
		      SELECT file_hash FROM books
		      WHERE file_hash IS NOT NULL AND file_hash != ''
		      GROUP BY file_hash HAVING COUNT(*) > 1
		  )
		ORDER BY b.file_hash, b.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DuplicateBookRow
	for rows.Next() {
		var r DuplicateBookRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Path, &r.Size, &r.PageCount, &r.FileHash, &r.SeriesID, &r.SeriesName); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SeriesCustomField 是系列级用户自定义 key-value 元数据条目。
type SeriesCustomField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ListSeriesCustomFields 返回某系列的全部自定义字段（按 key 排序）。
func (s *SqlStore) ListSeriesCustomFields(ctx context.Context, seriesID int64) ([]SeriesCustomField, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT field_key, field_value FROM series_custom_fields WHERE series_id = ? ORDER BY field_key`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fields []SeriesCustomField
	for rows.Next() {
		var f SeriesCustomField
		if err := rows.Scan(&f.Key, &f.Value); err != nil {
			return nil, err
		}
		fields = append(fields, f)
	}
	return fields, rows.Err()
}

// ReplaceSeriesCustomFields 整体替换某系列的自定义字段（先清空再写入非空 key），事务保证一致。
func (s *SqlStore) ReplaceSeriesCustomFields(ctx context.Context, seriesID int64, fields []SeriesCustomField) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM series_custom_fields WHERE series_id = ?`, seriesID); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		key := strings.TrimSpace(f.Key)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO series_custom_fields (series_id, field_key, field_value) VALUES (?, ?, ?)`, seriesID, key, f.Value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetBookCover 无条件更新书籍封面路径（与只在缺失时写入的 SetBookCoverIfMissing 不同），供“设为封面 / 上传封面”使用。
func (s *SqlStore) SetBookCover(ctx context.Context, bookID int64, coverPath string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE books SET cover_path = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, coverPath, bookID)
	return err
}

// seriesIDsForTag 返回关联了指定标签的所有系列 ID，用于在标签改名/合并/删除后刷新派生统计。
func (s *SqlStore) seriesIDsForTag(ctx context.Context, tagID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT series_id FROM series_tags WHERE tag_id = ?`, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *SqlStore) refreshSeriesStatsBatch(ctx context.Context, seriesIDs []int64) {
	for _, sid := range seriesIDs {
		if err := s.RefreshSeriesStats(ctx, sid); err != nil {
			slog.Warn("refresh series stats failed", "series_id", sid, "error", err)
		}
	}
}

// RenameTag 重命名标签。若新名与已有标签重名会因 UNIQUE 约束返回错误——调用方应转而合并。
func (s *SqlStore) RenameTag(ctx context.Context, tagID int64, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("tag name cannot be empty")
	}
	affected, err := s.seriesIDsForTag(ctx, tagID)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tags SET name = ? WHERE id = ?`, newName, tagID); err != nil {
		return err
	}
	s.refreshSeriesStatsBatch(ctx, affected)
	return nil
}

// MergeTags 把 sourceID 标签的所有系列关联迁移到 targetID，然后删除 source 标签。
func (s *SqlStore) MergeTags(ctx context.Context, sourceID, targetID int64) error {
	if sourceID == targetID {
		return nil
	}
	affected, err := s.seriesIDsForTag(ctx, sourceID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO series_tags (series_id, tag_id) SELECT series_id, ? FROM series_tags WHERE tag_id = ?`, targetID, sourceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM series_tags WHERE tag_id = ?`, sourceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, sourceID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.refreshSeriesStatsBatch(ctx, affected)
	return nil
}

// DeleteTag 删除标签（series_tags 经 ON DELETE CASCADE 一并清理），并刷新受影响系列统计。
func (s *SqlStore) DeleteTag(ctx context.Context, tagID int64) error {
	affected, err := s.seriesIDsForTag(ctx, tagID)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, tagID); err != nil {
		return err
	}
	s.refreshSeriesStatsBatch(ctx, affected)
	return nil
}

// BulkSeriesEdit 描述对一批系列的增量元数据编辑；nil / 空表示该维度不改。
// 与 updateSeriesInfo 的“全量替换”不同，这里 AddTags/RemoveTags 是增量语义，适合多选批量。
type BulkSeriesEdit struct {
	AddTags    []string
	RemoveTags []string
	Status     *string
	Publisher  *string
}

// sqlInBatchSize 是 IN (...) 展开的单批 ID 数上限。
//
// 硬约束是 SQLite 的 SQLITE_MAX_VARIABLE_NUMBER（32766）：超过就直接报
// "too many SQL variables"，批量编辑在大选区下会整个失败。
// 但取 500 而不是贴着上限，是因为超长的参数列表本身就很慢——同一份 4 万个 ID，
// 500 一批比贴着变量上限分两批快约一个数量级（准备语句与参数绑定的开销随参数数量非线性增长）。
const sqlInBatchSize = 500

// sqlInBatches 把 ids 切成不超过 sqlInBatchSize 的批次，逐批调用 fn。
// 调用方拿到的是「这一批」的占位符与参数，需要自己保证跨批的语义（见各调用点注释）。
func sqlInBatches(ids []int64, fn func(placeholders string, args []interface{}) error) error {
	for start := 0; start < len(ids); start += sqlInBatchSize {
		end := start + sqlInBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		placeholders, args := sqlInClause(ids[start:end])
		if err := fn(placeholders, args); err != nil {
			return err
		}
	}
	return nil
}

func sqlInClause(ids []int64) (string, []interface{}) {
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return strings.Join(placeholders, ","), args
}

// BulkEditSeries 在单个事务内对选中系列做增量元数据编辑，随后刷新受影响系列的派生统计。
func (s *SqlStore) BulkEditSeries(ctx context.Context, seriesIDs []int64, edit BulkSeriesEdit) error {
	if len(seriesIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // Commit 之后的 Rollback 返回 ErrTxDone，无害

	q := s.Queries.WithTx(tx)

	// 以下三处都按 sqlInBatchSize 分批展开 IN (...)：整个 BulkEditSeries 仍在同一个事务里，
	// 所以分批只影响语句条数，不影响原子性——用户选中上万个系列时，要么全成要么全不成，这一点没变。
	if edit.Status != nil {
		if err := sqlInBatches(seriesIDs, func(placeholders string, inArgs []interface{}) error {
			args := append([]interface{}{*edit.Status}, inArgs...)
			_, execErr := tx.ExecContext(ctx, `UPDATE series SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id IN (`+placeholders+`)`, args...)
			return execErr
		}); err != nil {
			return err
		}
	}
	if edit.Publisher != nil {
		if err := sqlInBatches(seriesIDs, func(placeholders string, inArgs []interface{}) error {
			args := append([]interface{}{*edit.Publisher}, inArgs...)
			_, execErr := tx.ExecContext(ctx, `UPDATE series SET publisher = ?, updated_at = CURRENT_TIMESTAMP WHERE id IN (`+placeholders+`)`, args...)
			return execErr
		}); err != nil {
			return err
		}
	}

	for _, name := range edit.AddTags {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tag, err := q.UpsertTag(ctx, name)
		if err != nil {
			return err
		}
		// 一条语句挂一批，而不是每个系列一条：选中数万个系列时后者就是数万条 INSERT。
		// 用 SELECT id FROM series WHERE id IN (...) 取代直接 VALUES，顺带把选区里
		// 已不存在的系列 id 挡在外面——否则外键约束会让整个批量编辑失败。
		if err := sqlInBatches(seriesIDs, func(placeholders string, inArgs []interface{}) error {
			args := append([]interface{}{tag.ID}, inArgs...)
			_, execErr := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO series_tags (series_id, tag_id) SELECT id, ? FROM series WHERE id IN (`+placeholders+`)`,
				args...)
			return execErr
		}); err != nil {
			return err
		}
	}

	for _, name := range edit.RemoveTags {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if err := sqlInBatches(seriesIDs, func(placeholders string, inArgs []interface{}) error {
			args := append(append([]interface{}{}, inArgs...), name)
			_, execErr := tx.ExecContext(ctx,
				`DELETE FROM series_tags WHERE series_id IN (`+placeholders+`) AND tag_id IN (SELECT id FROM tags WHERE name = ?)`,
				args...)
			return execErr
		}); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// 刷新派生统计（tag_names_cache / author_names_cache 等）；单个失败不影响整体。
	for _, sid := range seriesIDs {
		if err := s.RefreshSeriesStats(ctx, sid); err != nil {
			slog.Warn("refresh series stats after bulk edit failed", "series_id", sid, "error", err)
		}
	}
	return nil
}

// GetReadingListItemProgress 返回某阅读清单各系列的进度。userID>0 时取该用户的 user_series_progress，
// 否则用全局 series_stats（旧行为 / 首启 / 单元测试）。
func (s *SqlStore) GetReadingListItemProgress(ctx context.Context, readingListID, userID int64) (map[int64]ReadingListSeriesProgress, error) {
	out := make(map[int64]ReadingListSeriesProgress)
	if userID > 0 {
		rows, err := s.db.QueryContext(ctx, `
			SELECT rli.series_id,
			       COALESCE(usp.read_book_count, 0),
			       COALESCE(usp.completed_book_count, 0),
			       COALESCE(s.book_count, 0)
			FROM reading_list_items rli
			JOIN series s ON s.id = rli.series_id
			LEFT JOIN user_series_progress usp ON usp.series_id = rli.series_id AND usp.user_id = ?
			WHERE rli.reading_list_id = ?`, userID, readingListID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var p ReadingListSeriesProgress
			if err := rows.Scan(&p.SeriesID, &p.ReadBooks, &p.CompletedBooks, &p.TotalBooks); err != nil {
				return nil, err
			}
			out[p.SeriesID] = p
		}
		return out, rows.Err()
	}
	rows, err := s.Queries.GetReadingListItemProgressByList(ctx, readingListID)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.SeriesID] = ReadingListSeriesProgress{
			SeriesID:       r.SeriesID,
			ReadBooks:      r.ReadBooks,
			CompletedBooks: r.CompletedBooks,
			TotalBooks:     r.TotalBooks,
		}
	}
	return out, nil
}

func (s *SqlStore) refreshSeriesStatsForBook(ctx context.Context, bookID int64) error {
	seriesID, err := s.Queries.GetSeriesIDByBookID(ctx, bookID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	return s.RefreshSeriesStats(ctx, seriesID)
}

func (s *SqlStore) CreateBook(ctx context.Context, arg CreateBookParams) (Book, error) {
	book, err := s.Queries.CreateBook(ctx, arg)
	if err != nil {
		return Book{}, err
	}
	if err := s.RefreshSeriesStats(ctx, book.SeriesID); err != nil {
		return Book{}, err
	}
	return book, nil
}

func (s *SqlStore) UpsertBookByPath(ctx context.Context, arg UpsertBookByPathParams) (Book, error) {
	book, err := s.Queries.UpsertBookByPath(ctx, arg)
	if err != nil {
		return Book{}, err
	}
	if err := s.RefreshSeriesStats(ctx, book.SeriesID); err != nil {
		return Book{}, err
	}
	return book, nil
}

func (s *SqlStore) DeleteBook(ctx context.Context, id int64) error {
	seriesID, err := s.Queries.GetSeriesIDByBookID(ctx, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := s.Queries.DeleteBook(ctx, id); err != nil {
		return err
	}
	if err == nil {
		return s.RefreshSeriesStats(ctx, seriesID)
	}
	return nil
}

func (s *SqlStore) DeleteBookByPath(ctx context.Context, path string) error {
	seriesID, err := s.Queries.GetSeriesIDByBookPath(ctx, path)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := s.Queries.DeleteBookByPath(ctx, path); err != nil {
		return err
	}
	if err == nil {
		return s.RefreshSeriesStats(ctx, seriesID)
	}
	return nil
}

func (s *SqlStore) UpdateBookProgress(ctx context.Context, arg UpdateBookProgressParams) error {
	if err := s.Queries.UpdateBookProgress(ctx, arg); err != nil {
		return err
	}
	return s.refreshSeriesStatsForBook(ctx, arg.ID)
}

func (s *SqlStore) UpdateSeriesStatistics(ctx context.Context, arg UpdateSeriesStatisticsParams) error {
	if err := s.Queries.UpdateSeriesStatistics(ctx, arg); err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, arg.ID)
}

func (s *SqlStore) LinkSeriesTag(ctx context.Context, arg LinkSeriesTagParams) error {
	if err := s.Queries.LinkSeriesTag(ctx, arg); err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, arg.SeriesID)
}

func (s *SqlStore) ClearSeriesTags(ctx context.Context, seriesID int64) error {
	if err := s.Queries.ClearSeriesTags(ctx, seriesID); err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, seriesID)
}

func (s *SqlStore) LinkSeriesAuthor(ctx context.Context, arg LinkSeriesAuthorParams) error {
	if err := s.Queries.LinkSeriesAuthor(ctx, arg); err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, arg.SeriesID)
}

func (s *SqlStore) ClearSeriesAuthors(ctx context.Context, seriesID int64) error {
	if err := s.Queries.ClearSeriesAuthors(ctx, seriesID); err != nil {
		return err
	}
	return s.RefreshSeriesStats(ctx, seriesID)
}
