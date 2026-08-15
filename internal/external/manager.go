package external

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"manga-manager/internal/database"
)

// Store 是本包真正需要的那三个查询。
//
// 刻意**不**用 database.Store：不是为了「解耦」——参数与返回值仍是 database 包里 sqlc
// 生成的类型，import 边一分不减——而是让「逐 seriesID 一次 SELECT * 查系列书目」这类
// N+1 性能回归在编译期就写不出来：窄接口里没有 ListBooksBySeries，想加回去得先改这个
// 声明，这一步会被 code review 看见。
//
// 接口里也没有 ExecTx：交出 *Queries 就等于把它的全部方法一并交出去，收窄就成了纯粹的
// 表演。本包不需要事务，正好可以名副其实。
type Store interface {
	GetLibrary(ctx context.Context, id int64) (database.Library, error)
	ListExternalLibraryBooksByLibrary(ctx context.Context, libraryID int64) ([]database.ExternalLibraryBookRow, error)
	ListExternalTransferBooksBySeries(ctx context.Context, seriesIDs []int64) ([]database.ListExternalTransferBooksBySeriesRow, error)
}

var (
	ErrSessionNotFound = errors.New("external session not found")
	ErrSessionNotReady = errors.New("external session not ready")
	// ErrExternalPathInvalid 表示外部路径不是一个可用的绝对目录。
	ErrExternalPathInvalid = errors.New("external path must be an absolute directory")
	// ErrExternalPathInsideLibrary 表示外部路径与资料库根目录重叠。
	// 往库内传输会让下一次扫描把这些副本收编成重复书籍，用户还得手动去重。
	ErrExternalPathInsideLibrary = errors.New("external path overlaps the library root")
)

// pathsOverlap 报告两个路径是否有包含关系（含相等）。
//
// 与 scanner 的 pathUnderRoot 同口径：用 filepath.Rel 而不是补分隔符再 HasPrefix，
// 顺带处理了 . 与 .. 的规范化；Windows 上文件系统大小写不敏感，先归一化再比。
func pathsOverlap(a, b string) bool {
	if runtime.GOOS == "windows" {
		a = strings.ToLower(a)
		b = strings.ToLower(b)
	}
	if a == b {
		return true
	}
	for _, pair := range [2][2]string{{a, b}, {b, a}} {
		rel, err := filepath.Rel(pair[1], pair[0])
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

type SessionSnapshot struct {
	SessionID       string    `json:"session_id"`
	LibraryID       int64     `json:"library_id"`
	ExternalPath    string    `json:"external_path"`
	IgnoreExtension bool      `json:"ignore_extension"`
	Status          string    `json:"status"`
	Error           string    `json:"error,omitempty"`
	ScannedFiles    int       `json:"scanned_files"`
	MatchedBooks    int       `json:"matched_books"`
	UnmatchedFiles  int       `json:"unmatched_files"`
	TotalBooks      int       `json:"total_books"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SeriesCoverage struct {
	SeriesID           int64  `json:"series_id"`
	SeriesName         string `json:"series_name"`
	ExternalMatchCount int    `json:"external_match_count"`
	ExternalTotalCount int    `json:"external_total_count"`
	ExternalSyncStatus string `json:"external_sync_status"`
}

type TransferOperation struct {
	BookID       int64
	SeriesID     int64
	SeriesName   string
	SourcePath   string
	Destination  string
	RelativePath string
	MatchKey     string
}

type TransferPlan struct {
	SeriesCount   int                 `json:"series_count"`
	MissingBooks  int                 `json:"missing_books"`
	ExistingBooks int                 `json:"existing_books"`
	Operations    []TransferOperation `json:"-"`
}

type seriesEntry struct {
	SeriesID   int64
	SeriesName string
	Matched    int
	Total      int
}

type session struct {
	ID              string
	LibraryID       int64
	LibraryPath     string
	ExternalPath    string
	IgnoreExtension bool
	Status          string
	Error           string
	ScannedFiles    int
	MatchedBooks    int
	UnmatchedFiles  int
	TotalBooks      int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Series          map[int64]*seriesEntry
	MatchedKeys     map[string]struct{}
}

type Manager struct {
	store    Store
	ttl      time.Duration
	mu       sync.RWMutex
	sessions map[string]*session
}

func NewManager(store Store, ttl time.Duration) *Manager {
	return &Manager{
		store:    store,
		ttl:      ttl,
		sessions: make(map[string]*session),
	}
}

func (m *Manager) CreateSession(ctx context.Context, libraryID int64, externalPath string, ignoreExtension bool) (SessionSnapshot, error) {
	lib, err := m.store.GetLibrary(ctx, libraryID)
	if err != nil {
		return SessionSnapshot{}, err
	}

	// 必须是绝对路径。相对路径会被静默解释成**服务进程的工作目录**——用户以为传到了
	// 外接盘，实际写进了服务的运行目录。filepath.Abs 帮不上忙：它只是把 "." 拼上 CWD
	// 之后返回一个完全合法的目录，错误就此变得不可见。
	if !filepath.IsAbs(externalPath) {
		return SessionSnapshot{}, ErrExternalPathInvalid
	}
	externalPath = filepath.Clean(externalPath)

	// 拒掉 POSIX 根：拿 "/" 当外部库会去遍历整个文件系统。
	// VolumeName 的判断是为了放过 Windows 的 D:\ 与 \\server\share\——
	// 「整块外接盘 / NAS 根目录当外部库」正是这个功能的头号用法，误杀它才是回归。
	if filepath.Dir(externalPath) == externalPath && filepath.VolumeName(externalPath) == "" {
		return SessionSnapshot{}, ErrExternalPathInvalid
	}

	info, err := os.Stat(externalPath)
	if err != nil {
		return SessionSnapshot{}, err
	}
	if !info.IsDir() {
		return SessionSnapshot{}, fmt.Errorf("external path is not a directory")
	}

	// 外部路径与库根重叠时，传输产生的副本会被下一次扫描收编成重复书籍，用户还得手动去重。
	// 注意这里只挡**本库**：external_path 指向另一个资料库根时后果一模一样，但那需要
	// 拉全部库列表，留作后续（此处不假装已经全堵住）。
	if pathsOverlap(externalPath, filepath.Clean(lib.Path)) {
		return SessionSnapshot{}, ErrExternalPathInsideLibrary
	}

	now := time.Now()
	s := &session{
		ID:              fmt.Sprintf("%d-%d", libraryID, now.UnixNano()),
		LibraryID:       libraryID,
		LibraryPath:     lib.Path,
		ExternalPath:    externalPath,
		IgnoreExtension: ignoreExtension,
		Status:          "scanning",
		CreatedAt:       now,
		UpdatedAt:       now,
		Series:          make(map[int64]*seriesEntry),
		MatchedKeys:     make(map[string]struct{}),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(now)
	m.sessions[s.ID] = s
	return snapshotFromSession(s), nil
}

func (m *Manager) GetSession(libraryID int64, sessionID string) (SessionSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now())
	s, ok := m.sessions[sessionID]
	if !ok || s.LibraryID != libraryID {
		return SessionSnapshot{}, ErrSessionNotFound
	}
	return snapshotFromSession(s), nil
}

func (m *Manager) GetSeriesCoverage(libraryID int64, sessionID string, seriesIDs []int64) ([]SeriesCoverage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now())
	s, ok := m.sessions[sessionID]
	if !ok || s.LibraryID != libraryID {
		return nil, ErrSessionNotFound
	}

	items := make([]SeriesCoverage, 0)
	appendEntry := func(entry *seriesEntry) {
		status := "missing"
		if entry.Total > 0 && entry.Matched >= entry.Total {
			status = "complete"
		} else if entry.Matched > 0 {
			status = "partial"
		}
		items = append(items, SeriesCoverage{
			SeriesID:           entry.SeriesID,
			SeriesName:         entry.SeriesName,
			ExternalMatchCount: entry.Matched,
			ExternalTotalCount: entry.Total,
			ExternalSyncStatus: status,
		})
	}

	if len(seriesIDs) == 0 {
		entries := make([]*seriesEntry, 0, len(s.Series))
		for _, entry := range s.Series {
			entries = append(entries, entry)
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].SeriesName == entries[j].SeriesName {
				return entries[i].SeriesID < entries[j].SeriesID
			}
			return entries[i].SeriesName < entries[j].SeriesName
		})
		for _, entry := range entries {
			appendEntry(entry)
		}
		return items, nil
	}

	for _, seriesID := range seriesIDs {
		entry, ok := s.Series[seriesID]
		if !ok {
			items = append(items, SeriesCoverage{
				SeriesID:           seriesID,
				ExternalMatchCount: 0,
				ExternalTotalCount: 0,
				ExternalSyncStatus: "missing",
			})
			continue
		}
		appendEntry(entry)
	}
	return items, nil
}

// ScanSession 遍历外部路径并与本库书目对账。progress 只报两个计数，不带展示文案：
// 用户可见文字由调用方按语种渲染，本包渲染的话英文用户会看到中文。
func (m *Manager) ScanSession(ctx context.Context, sessionID string, progress func(current, total int)) (SessionSnapshot, error) {
	m.mu.Lock()
	m.pruneLocked(time.Now())
	s, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return SessionSnapshot{}, ErrSessionNotFound
	}
	s.Status = "scanning"
	s.Error = ""
	s.ScannedFiles = 0
	s.MatchedBooks = 0
	s.UnmatchedFiles = 0
	s.TotalBooks = 0
	s.UpdatedAt = time.Now()
	s.Series = make(map[int64]*seriesEntry)
	s.MatchedKeys = make(map[string]struct{})
	libraryID := s.LibraryID
	libraryPath := s.LibraryPath
	externalPath := s.ExternalPath
	ignoreExtension := s.IgnoreExtension
	m.mu.Unlock()

	books, err := m.store.ListExternalLibraryBooksByLibrary(ctx, libraryID)
	if err != nil {
		m.setFailure(sessionID, err)
		return SessionSnapshot{}, err
	}

	type bookRef struct {
		BookID     int64
		SeriesID   int64
		SeriesName string
	}

	bookByKey := make(map[string]bookRef, len(books))
	seriesMap := make(map[int64]*seriesEntry)
	for _, book := range books {
		key, _, err := relativePathKeys(libraryPath, book.Path, ignoreExtension)
		if err != nil {
			continue
		}
		bookByKey[key] = bookRef{BookID: book.BookID, SeriesID: book.SeriesID, SeriesName: book.SeriesName}
		entry := seriesMap[book.SeriesID]
		if entry == nil {
			entry = &seriesEntry{SeriesID: book.SeriesID, SeriesName: book.SeriesName}
			seriesMap[book.SeriesID] = entry
		}
		entry.Total++
	}

	paths := make([]string, 0)
	err = filepath.WalkDir(externalPath, func(path string, d os.DirEntry, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr // 响应取消：中止外部盘遍历
		}
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !isSupportedArchive(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		m.setFailure(sessionID, err)
		return SessionSnapshot{}, err
	}

	matchedBookIDs := make(map[int64]struct{})
	matchedKeys := make(map[string]struct{})
	unmatchedFiles := 0
	total := len(paths)
	for index, path := range paths {
		if ctxErr := ctx.Err(); ctxErr != nil {
			m.setFailure(sessionID, ctxErr) // 响应取消：中止匹配循环
			return SessionSnapshot{}, ctxErr
		}
		key, _, keyErr := relativePathKeys(externalPath, path, ignoreExtension)
		if keyErr != nil {
			unmatchedFiles++
		} else if ref, ok := bookByKey[key]; ok {
			matchedKeys[key] = struct{}{}
			if _, seen := matchedBookIDs[ref.BookID]; !seen {
				matchedBookIDs[ref.BookID] = struct{}{}
				if entry := seriesMap[ref.SeriesID]; entry != nil {
					entry.Matched++
				}
			}
		} else {
			unmatchedFiles++
		}

		m.mu.Lock()
		if current, exists := m.sessions[sessionID]; exists {
			current.ScannedFiles = index + 1
			current.UpdatedAt = time.Now()
		}
		m.mu.Unlock()

		if progress != nil {
			progress(index+1, total)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	current, exists := m.sessions[sessionID]
	if !exists {
		return SessionSnapshot{}, ErrSessionNotFound
	}
	current.Status = "ready"
	current.Error = ""
	current.ScannedFiles = total
	current.TotalBooks = len(books)
	current.MatchedBooks = len(matchedBookIDs)
	current.UnmatchedFiles = unmatchedFiles
	current.Series = seriesMap
	current.MatchedKeys = matchedKeys
	current.UpdatedAt = time.Now()
	return snapshotFromSession(current), nil
}

func (m *Manager) PrepareTransfer(ctx context.Context, libraryID int64, sessionID string, seriesIDs []int64) (TransferPlan, error) {
	m.mu.Lock()
	m.pruneLocked(time.Now())
	s, ok := m.sessions[sessionID]
	if !ok || s.LibraryID != libraryID {
		m.mu.Unlock()
		return TransferPlan{}, ErrSessionNotFound
	}
	if s.Status != "ready" {
		m.mu.Unlock()
		return TransferPlan{}, ErrSessionNotReady
	}
	libraryPath := s.LibraryPath
	externalPath := s.ExternalPath
	matchedKeys := make(map[string]struct{}, len(s.MatchedKeys))
	for key := range s.MatchedKeys {
		matchedKeys[key] = struct{}{}
	}
	ignoreExtension := s.IgnoreExtension
	m.mu.Unlock()

	// 先去重：seriesIDs 里若有重复（如 series_ids:[7,7]），会把系列 7 的每本书规划两遍
	// ——missing_books 翻倍、Operations 里出现源与目标都相同的重复项，用户看到的数字和
	// 进度条分母都是错的。
	unique := make([]int64, 0, len(seriesIDs))
	seen := make(map[int64]struct{}, len(seriesIDs))
	for _, id := range seriesIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	plan := TransferPlan{SeriesCount: len(unique)}
	// 一次批量取齐所有系列的书，避免逐 seriesID 一次 SELECT * 的 N+1（books 表列多、
	// 含 summary 长文本）；调用方必须把这里算出的 plan 原样传下去，不得为图省事
	// 再调一次 PrepareTransfer 重新规划——那等于把这整段 DB 往返再跑一遍。
	books, err := m.store.ListExternalTransferBooksBySeries(ctx, unique)
	if err != nil {
		return TransferPlan{}, err
	}
	for _, book := range books {
		if book.LibraryID != libraryID {
			continue
		}
		matchKey, displayRel, err := relativePathKeys(libraryPath, book.Path, ignoreExtension)
		if err != nil {
			continue
		}
		if _, ok := matchedKeys[matchKey]; ok {
			plan.ExistingBooks++
			continue
		}
		plan.MissingBooks++
		plan.Operations = append(plan.Operations, TransferOperation{
			BookID:       book.ID,
			SeriesID:     book.SeriesID,
			SeriesName:   book.Volume,
			SourcePath:   book.Path,
			Destination:  filepath.Join(externalPath, filepath.FromSlash(displayRel)),
			RelativePath: displayRel,
			MatchKey:     matchKey,
		})
	}
	return plan, nil
}

func (m *Manager) MarkTransferred(libraryID int64, sessionID string, op TransferOperation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now())
	s, ok := m.sessions[sessionID]
	if !ok || s.LibraryID != libraryID {
		return ErrSessionNotFound
	}
	if _, exists := s.MatchedKeys[op.MatchKey]; exists {
		return nil
	}
	s.MatchedKeys[op.MatchKey] = struct{}{}
	s.MatchedBooks++
	if entry := s.Series[op.SeriesID]; entry != nil && entry.Matched < entry.Total {
		entry.Matched++
	}
	s.UpdatedAt = time.Now()
	return nil
}

func (m *Manager) ClearSession(libraryID int64, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[sessionID]; ok && s.LibraryID == libraryID {
		delete(m.sessions, sessionID)
	}
}

func (m *Manager) setFailure(sessionID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[sessionID]; ok {
		s.Status = "failed"
		s.Error = err.Error()
		s.UpdatedAt = time.Now()
	}
}

func (m *Manager) pruneLocked(now time.Time) {
	for key, s := range m.sessions {
		if now.Sub(s.UpdatedAt) > m.ttl {
			delete(m.sessions, key)
		}
	}
}

func snapshotFromSession(s *session) SessionSnapshot {
	return SessionSnapshot{
		SessionID:       s.ID,
		LibraryID:       s.LibraryID,
		ExternalPath:    s.ExternalPath,
		IgnoreExtension: s.IgnoreExtension,
		Status:          s.Status,
		Error:           s.Error,
		ScannedFiles:    s.ScannedFiles,
		MatchedBooks:    s.MatchedBooks,
		UnmatchedFiles:  s.UnmatchedFiles,
		TotalBooks:      s.TotalBooks,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

func relativePathKeys(root, fullPath string, ignoreExtension bool) (matchKey string, displayRel string, err error) {
	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", "", err
	}
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", "", fmt.Errorf("path %q is outside root %q", fullPath, root)
	}
	display := filepath.ToSlash(rel)
	match := display
	if ignoreExtension {
		ext := filepath.Ext(match)
		if ext != "" {
			match = strings.TrimSuffix(match, ext)
		}
	}
	return strings.ToLower(match), display, nil
}

func isSupportedArchive(path string) bool {
	switch strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".") {
	case "zip", "cbz", "rar", "cbr":
		return true
	default:
		return false
	}
}
