package bookcontent

import (
	"context"
	"fmt"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/parser"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
)

// LibraryAccess is the existing library read admission capability, not a second task lock.
type LibraryAccess interface {
	LibraryAccessVersion() uint64
	AcquireLibraryRead(int64) (func(), error)
}

// AcquireRead resolves library ownership before admission and only publishes a stable source snapshot.
func (s *Service) AcquireRead(ctx context.Context, id int64, access LibraryAccess) (func(), error) {
	version := access.LibraryAccessVersion()
	now := time.Now()
	cached, ok := s.sources.Get(id)
	ok = ok && now.Sub(cached.at) < 30*time.Second
	source := cached.source
	if !ok {
		var err error
		source, err = s.lookup(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrSourceMissing, err)
		}
	}
	release, err := access.AcquireLibraryRead(source.LibraryID)
	if err != nil {
		return nil, err
	}
	if !ok && version == access.LibraryAccessVersion() {
		s.sources.Add(id, cachedSource{source: source, at: now})
	}
	return release, nil
}

// Source identifies a book's current content without reader state or HTTP fields.
type Source struct {
	ID             int64
	LibraryID      int64
	Path           string
	FileModifiedAt time.Time
	Size           int64
	PageCount      int64
}

// SourceLookup resolves book identity to archive facts.
type SourceLookup func(context.Context, int64) (Source, error)

type cachedSource struct {
	source Source
	at     time.Time
}

// Service owns content lookup, manifests, variants and processing cache decisions.
type Service struct {
	lookup    SourceLookup
	config    func() config.Config
	memory    *MemoryCache
	sources   *lru.Cache[int64, cachedSource]
	manifests *lru.Cache[string, []parser.PageMetadata]
	reader    *ArchiveReader
	variants  singleflight.Group
}

// New binds source lookup and runtime configuration to bounded content caches.
func New(lookup SourceLookup, currentConfig func() config.Config, memory *MemoryCache, sourceSize, manifestSize int) *Service {
	if sourceSize < 1 {
		sourceSize = 512
	}
	if manifestSize < 1 {
		manifestSize = 128
	}
	sources, _ := lru.New[int64, cachedSource](sourceSize)
	manifests, _ := lru.New[string, []parser.PageMetadata](manifestSize)
	if memory == nil {
		memory = NewMemoryCache(DefaultImageCacheBytes)
	}
	return &Service{lookup: lookup, config: currentConfig, memory: memory, sources: sources, manifests: manifests, reader: NewArchiveReader(nil)}
}

// Source resolves a book, retaining the read path's bounded thirty-second lookup cache.
func (s *Service) Source(ctx context.Context, id int64) (Source, error) {
	now := time.Now()
	if cached, ok := s.sources.Get(id); ok && now.Sub(cached.at) < 30*time.Second {
		return cached.source, nil
	}
	source, err := s.lookup(ctx, id)
	if err != nil {
		return Source{}, err
	}
	s.sources.Add(id, cachedSource{source: source, at: now})
	return source, nil
}

// Manifest carries a page directory and the source on which it depends.
type Manifest struct {
	Source   Source
	Pages    []parser.PageMetadata
	CacheHit bool
}

// List resolves a book and returns its page directory as one complete query.
func (s *Service) List(ctx context.Context, id int64) (Manifest, error) {
	source, err := s.Source(ctx, id)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrSourceMissing, err)
	}
	return s.ListSource(ctx, source)
}

// ListSource serves consumers that already hold authoritative archive facts.
func (s *Service) ListSource(ctx context.Context, source Source) (Manifest, error) {
	key := sourceKey(source)
	if pages, ok := s.manifests.Get(key); ok {
		return Manifest{Source: source, Pages: append([]parser.PageMetadata(nil), pages...), CacheHit: true}, nil
	}
	pages, err := s.reader.List(ctx, source.Path)
	if err != nil {
		return Manifest{Source: source}, err
	}
	s.manifests.Add(key, append([]parser.PageMetadata(nil), pages...))
	return Manifest{Source: source, Pages: pages}, nil
}

func sourceKey(source Source) string {
	return fmt.Sprintf("%d|%s|%d|%d", source.ID, source.Path, source.FileModifiedAt.UnixNano(), source.Size)
}

// PurgeSources invalidates the coupled source and directory projections after library changes.
func (s *Service) PurgeSources() { s.sources.Purge(); s.manifests.Purge() }
