package external

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"manga-manager/internal/database"
)

var (
	ErrSessionNotFound = errors.New("external session not found")
	ErrSessionNotReady = errors.New("external session not ready")
)

type SessionSnapshot struct {
	SessionID      string    `json:"session_id"`
	LibraryID      int64     `json:"library_id"`
	ExternalPath   string    `json:"external_path"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	ScannedFiles   int       `json:"scanned_files"`
	MatchedBooks   int       `json:"matched_books"`
	UnmatchedFiles int       `json:"unmatched_files"`
	TotalBooks     int       `json:"total_books"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
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
	ID             string
	LibraryID      int64
	LibraryPath    string
	ExternalPath   string
	Status         string
	Error          string
	ScannedFiles   int
	MatchedBooks   int
	UnmatchedFiles int
	TotalBooks     int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Series         map[int64]*seriesEntry
	MatchedKeys    map[string]struct{}
}

type Manager struct {
	store    database.Store
	ttl      time.Duration
	mu       sync.RWMutex
	sessions map[string]*session
}

func NewManager(store database.Store, ttl time.Duration) *Manager {
	return &Manager{
		store:    store,
		ttl:      ttl,
		sessions: make(map[string]*session),
	}
}

func (m *Manager) CreateSession(ctx context.Context, libraryID int64, externalPath string) (SessionSnapshot, error) {
	lib, err := m.store.GetLibrary(ctx, libraryID)
	if err != nil {
		return SessionSnapshot{}, err
	}

	info, err := os.Stat(externalPath)
	if err != nil {
		return SessionSnapshot{}, err
	}
	if !info.IsDir() {
		return SessionSnapshot{}, fmt.Errorf("external path is not a directory")
	}

	now := time.Now()
	s := &session{
		ID:           fmt.Sprintf("%d-%d", libraryID, now.UnixNano()),
		LibraryID:    libraryID,
		LibraryPath:  lib.Path,
		ExternalPath: externalPath,
		Status:       "scanning",
		CreatedAt:    now,
		UpdatedAt:    now,
		Series:       make(map[int64]*seriesEntry),
		MatchedKeys:  make(map[string]struct{}),
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

func (m *Manager) ScanSession(ctx context.Context, sessionID string, progress func(current, total int, message string)) (SessionSnapshot, error) {
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
		key, _, err := relativePathKeys(libraryPath, book.Path)
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
		key, _, keyErr := relativePathKeys(externalPath, path)
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
			progress(index+1, total, fmt.Sprintf("已扫描 %d / %d 个外部资源文件", index+1, total))
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
	m.mu.Unlock()

	plan := TransferPlan{SeriesCount: len(seriesIDs)}
	for _, seriesID := range seriesIDs {
		books, err := m.store.ListBooksBySeries(ctx, seriesID)
		if err != nil {
			return TransferPlan{}, err
		}
		for _, book := range books {
			if book.LibraryID != libraryID {
				continue
			}
			matchKey, displayRel, err := relativePathKeys(libraryPath, book.Path)
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
		SessionID:      s.ID,
		LibraryID:      s.LibraryID,
		ExternalPath:   s.ExternalPath,
		Status:         s.Status,
		Error:          s.Error,
		ScannedFiles:   s.ScannedFiles,
		MatchedBooks:   s.MatchedBooks,
		UnmatchedFiles: s.UnmatchedFiles,
		TotalBooks:     s.TotalBooks,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}

func relativePathKeys(root, fullPath string) (matchKey string, displayRel string, err error) {
	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", "", err
	}
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", "", fmt.Errorf("path %q is outside root %q", fullPath, root)
	}
	display := filepath.ToSlash(rel)
	return strings.ToLower(display), display, nil
}

func isSupportedArchive(path string) bool {
	switch strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".") {
	case "zip", "cbz", "rar", "cbr":
		return true
	default:
		return false
	}
}
