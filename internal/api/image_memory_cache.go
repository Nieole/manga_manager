package api

import "manga-manager/internal/bookcontent"

type imageMemoryCache = bookcontent.MemoryCache

const defaultImageCacheBytes = bookcontent.DefaultImageCacheBytes

func newImageMemoryCache(maxBytes int64) *imageMemoryCache {
	return bookcontent.NewMemoryCache(maxBytes)
}
