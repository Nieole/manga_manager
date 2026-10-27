package bookcontent

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/images"
	"manga-manager/internal/storageio"
)

// ErrSourceMissing, ErrInvalidDimensions and ErrBusy classify content failures for adapters.
var (
	ErrSourceMissing     = errors.New("book source unavailable")
	ErrInvalidDimensions = errors.New("requested image size exceeds limits")
	ErrBusy              = errors.New("storage is busy")
)

// PageOptions describes a page variant; KnownVersion is a previously delivered content token.
type PageOptions struct {
	Width, Height                             int
	Format, Quality, Filter                   string
	AutoCrop, FitInside                       bool
	Waifu2xScale, Waifu2xNoise, Waifu2xFormat string
	KnownVersion                              string
}

// PageResult contains materialized bytes, their identity and observable processing facts.
// Data may be shared by the byte cache and must be treated as immutable.
type PageResult struct {
	Source                        Source
	Data                          []byte
	ContentType                   string
	Version                       string
	Transform                     string
	CacheSource                   string
	Derived                       bool
	Fallback                      string
	NotModified                   bool
	ArchiveOpen, ManifestCacheHit bool
	StorageProfile, VolumeKey     string
	IOWait                        time.Duration
}

// Page resolves, reads and transforms a page without exposing archive lifetime recipes.
func (s *Service) Page(ctx context.Context, id, number int64, options PageOptions) (PageResult, error) {
	source, err := s.Source(ctx, id)
	if err != nil {
		return PageResult{Source: Source{ID: id}}, fmt.Errorf("%w: %w", ErrSourceMissing, err)
	}
	base := PageResult{Source: source}
	if err := images.ValidateTargetDimensions(options.Width, options.Height); err != nil {
		return base, fmt.Errorf("%w: %w", ErrInvalidDimensions, err)
	}
	options.Width = images.SnapTargetDimension(options.Width)
	options.Height = images.SnapTargetDimension(options.Height)
	options.Filter = normalizeFilter(options.Filter)
	if number < 1 {
		return base, ErrPageNotFound
	}
	cfg := s.config()
	key := fmt.Sprintf("v5|%s|page:%d|size:%dx%d|format:%q|quality:%q|filter:%q|ai:%q:%q:%q|crop:%t|fit:%t|engines:%q:%q", sourceKey(source), number, options.Width, options.Height, options.Format, options.Quality, options.Filter, options.Waifu2xScale, options.Waifu2xNoise, options.Waifu2xFormat, options.AutoCrop, options.FitInside, cfg.Scanner.Waifu2xPath, cfg.Scanner.RealCuganPath)
	base.Version = contentVersion(key)
	base.Transform = transformProfile(options)
	if options.KnownVersion == base.Version {
		base.NotModified = true
		base.CacheSource = "client"
		return base, nil
	}
	requested := options.Width > 0 || options.Height > 0 || options.Format != "" || options.Quality != "" || images.FilterChangesPixels(options.Filter, options.Width, options.Height) || options.AutoCrop
	diskEnabled := diskCacheEnabled(cfg, source)
	if requested {
		if data, ok := s.memory.Get(key); ok {
			base.Data = data
			base.ContentType = detectImageContentType(data)
			base.Derived = true
			base.CacheSource = "memory"
			return base, nil
		}
		if diskEnabled {
			if data, ct, ok := readDiskImageCache(ctx, config.PageCacheDir(cfg), key); ok {
				s.cacheMemory(key, data)
				base.Data = data
				base.ContentType = ct
				base.Derived = true
				base.CacheSource = "disk"
				return base, nil
			}
		}
	}
	value, err, _ := s.variants.Do(key, func() (any, error) {
		result := base
		if requested {
			if data, ok := s.memory.Get(key); ok {
				result.Data = data
				result.ContentType = detectImageContentType(data)
				result.Derived = true
				result.CacheSource = "memory"
				return result, nil
			}
		}
		workCtx := context.WithoutCancel(ctx)
		policy := config.ResolveStoragePolicy(cfg, source.Path)
		lease, err := storageio.Default.Acquire(workCtx, storageio.Request{VolumeKey: policy.VolumeKey, Limit: policy.IOPolicy.ArchiveOpenConcurrency, Kind: storageio.WorkKindReader, PauseWhenReading: policy.IOPolicy.PauseBackgroundWhenReading})
		if err != nil {
			return result, fmt.Errorf("%w: %w", ErrBusy, err)
		}
		defer lease.Release()
		result.StorageProfile = policy.StorageProfile
		result.VolumeKey = policy.VolumeKey
		result.IOWait = lease.Wait
		manifest, err := s.ListSource(workCtx, source)
		result.ManifestCacheHit = manifest.CacheHit
		if err != nil {
			if number > source.PageCount && source.PageCount > 0 {
				return result, ErrPageNotFound
			}
			return result, err
		}
		if number < 1 || number > int64(len(manifest.Pages)) {
			return result, ErrPageNotFound
		}
		materialized, err := s.reader.NamedPage(workCtx, source.Path, manifest.Pages[number-1])
		result.ArchiveOpen = true
		if err != nil {
			return result, err
		}
		lease.Release()
		opts := images.ProcessOptions{Width: options.Width, Height: options.Height, Format: options.Format, Filter: options.Filter, AutoCrop: options.AutoCrop, FitInside: options.FitInside, Waifu2xPath: cfg.Scanner.Waifu2xPath, RealCuganPath: cfg.Scanner.RealCuganPath, Waifu2xScale: 2, Waifu2xNoise: 0, Waifu2xFormat: "webp"}
		if v, e := strconv.Atoi(options.Quality); e == nil {
			opts.Quality = v
		}
		if v, e := strconv.Atoi(options.Waifu2xScale); e == nil {
			opts.Waifu2xScale = v
		}
		if v, e := strconv.Atoi(options.Waifu2xNoise); e == nil {
			opts.Waifu2xNoise = v
		}
		if options.Waifu2xFormat != "" {
			opts.Waifu2xFormat = options.Waifu2xFormat
		}
		outcome, err := images.ProcessImageDetailed(workCtx, materialized.Data, materialized.Page.MediaType, opts)
		if err != nil {
			slog.WarnContext(workCtx, "Image process error, fallback to raw source", "error", err)
			result.Data = materialized.Data
			result.ContentType = materialized.Page.MediaType
			result.Fallback = "process-error"
		} else {
			result.Data = outcome.Data
			result.ContentType = outcome.ContentType
			result.Fallback = outcome.SkippedFilter
			result.Derived = requested && !outcome.Passthrough && outcome.SkippedFilter == ""
		}
		result.CacheSource = "raw"
		if result.Derived {
			result.CacheSource = "processed"
			s.cacheMemory(key, result.Data)
			if diskEnabled {
				if err := writeDiskImageCache(config.PageCacheDir(cfg), key, result.Data, result.ContentType); err != nil {
					slog.WarnContext(workCtx, "Failed to write processed page disk cache", "error", err)
				}
			}
		}
		if result.Fallback != "" {
			result.Version = contentVersion(key + "|fallback:" + result.Fallback)
			if result.Fallback != "process-error" {
				slog.WarnContext(workCtx, "Page image delivered without the requested AI filter", "book_id", id, "page", number, "filter", result.Fallback)
			}
		}
		return result, nil
	})
	result := value.(PageResult)
	if err == nil && options.KnownVersion == result.Version {
		result.NotModified = true
		result.CacheSource = "client"
	}
	return result, err
}

func contentVersion(key string) string { return fmt.Sprintf("%x", sha1.Sum([]byte(key))) }

func normalizeFilter(filter string) string {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "", "none", "nearest", "average", "bilinear":
		return ""
	default:
		return filter
	}
}

func transformProfile(o PageOptions) string {
	parts := []string{}
	if o.Format != "" {
		parts = append(parts, "format:"+o.Format)
	}
	if o.Width != 0 || o.Height != 0 {
		resize := fmt.Sprintf("resize:%dx%d", o.Width, o.Height)
		if o.FitInside {
			resize += ":inside"
		}
		parts = append(parts, resize)
	}
	if o.Filter != "" {
		parts = append(parts, "filter:"+o.Filter)
	}
	if o.AutoCrop {
		parts = append(parts, "auto_crop")
	}
	if o.Waifu2xScale != "" || o.Waifu2xNoise != "" || o.Waifu2xFormat != "" {
		parts = append(parts, "ai:"+o.Waifu2xScale+":"+o.Waifu2xNoise+":"+o.Waifu2xFormat)
	}
	if len(parts) == 0 {
		return "raw"
	}
	return strings.Join(parts, "|")
}

// DiskCacheEnabled applies storage policy to a source's processed-page cache.
func (s *Service) DiskCacheEnabled(source Source) bool { return diskCacheEnabled(s.config(), source) }

func diskCacheEnabled(cfg config.Config, source Source) bool {
	if !cfg.Cache.PageDiskCacheEnabled {
		return false
	}
	policy := config.ResolveStoragePolicy(cfg, source.Path)
	return !policy.IOPolicy.DisableSameDiskPageCache || !config.SameVolume(cfg.Cache.Dir, source.Path)
}

func (s *Service) cacheMemory(key string, data []byte) {
	if len(data) <= 4<<20 {
		s.memory.Add(key, data)
	}
}
