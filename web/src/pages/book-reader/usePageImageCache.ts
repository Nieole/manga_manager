import { useCallback, useRef, useState } from 'react';
import { isResamplingFilter } from './readerImageSizing';
import type { ImageFilter, Page, ReaderBookInfo, ReaderImageFormat } from './types';

interface PageImageRequest {
  promise: Promise<string>;
  controller: AbortController;
}

export interface ReaderBookCache {
  pages?: Page[];
  bookInfo?: ReaderBookInfo;
  nextBookId?: number | null;
  imageUrls: Map<string, string>;
  imageRequests: Map<string, PageImageRequest>;
  preloadedImageUrls: Set<string>;
}

function createReaderBookCache(): ReaderBookCache {
  return {
    imageUrls: new Map(),
    imageRequests: new Map(),
    preloadedImageUrls: new Set(),
  };
}

function isServerSideImageFilter(imageFilter: ImageFilter) {
  return !['none', 'nearest', 'average', 'bilinear'].includes(imageFilter);
}

interface UsePageImageCacheOptions {
  imageFilter: ImageFilter;
  w2xScale: number;
  w2xNoise: number;
  w2xFormat: string;
  autoCrop: boolean;
  readerImageFormat: ReaderImageFormat;
  readerImageQuality: number;
  // 重采样滤镜要下发的目标尺寸档位，0 表示这条边不作约束。见 readerImageSizing。
  targetWidth: number;
  targetHeight: number;
  currentBookIdRef: React.MutableRefObject<string | null>;
}

export function usePageImageCache({
  imageFilter,
  w2xScale,
  w2xNoise,
  w2xFormat,
  autoCrop,
  readerImageFormat,
  readerImageQuality,
  targetWidth,
  targetHeight,
  currentBookIdRef,
}: UsePageImageCacheOptions) {
  const [cachedPageImageUrls, setCachedPageImageUrls] = useState<Record<number, string>>({});
  const readerBookCacheRef = useRef<Map<string, ReaderBookCache>>(new Map());
  const imageCacheGenerationRef = useRef(0);

  const imageOptionsKey = [
    imageFilter,
    w2xScale,
    w2xNoise,
    w2xFormat,
    autoCrop ? 'crop' : 'no-crop',
    readerImageFormat,
    readerImageQuality,
    targetWidth,
    targetHeight,
  ].join('|');

  const getBookCache = useCallback((targetBookId: string) => {
    let cache = readerBookCacheRef.current.get(targetBookId);
    if (!cache) {
      cache = createReaderBookCache();
      readerBookCacheRef.current.set(targetBookId, cache);
    }
    return cache;
  }, []);

  const getImageUrlForBook = useCallback((targetBookId: string, pageNum: number) => {
    const params = new URLSearchParams();
    if (imageFilter && isServerSideImageFilter(imageFilter)) {
      params.set('filter', imageFilter);
      if (imageFilter === 'waifu2x' || imageFilter === 'realcugan') {
        params.set('w2x_scale', String(w2xScale));
        params.set('w2x_noise', String(w2xNoise));
        params.set('w2x_format', w2xFormat);
      } else if (isResamplingFilter(imageFilter) && (targetWidth > 0 || targetHeight > 0)) {
        // 重采样滤镜是缩放的插值核：只给核不给尺寸，服务端无事可做，六项选下来画面逐字节相同。
        // fit=inside 让服务端把这两条边当成框——等比缩进去，不拉伸也不放大。
        if (targetWidth > 0) params.set('w', String(targetWidth));
        if (targetHeight > 0) params.set('h', String(targetHeight));
        params.set('fit', 'inside');
      }
    }
    if (autoCrop) {
      params.set('auto_crop', 'true');
    }
    if (readerImageFormat !== 'original') {
      params.set('format', readerImageFormat);
      params.set('q', String(readerImageQuality));
    }
    const query = params.toString();
    return `/api/pages/${targetBookId}/${pageNum}${query ? `?${query}` : ''}`;
  }, [autoCrop, imageFilter, readerImageFormat, readerImageQuality, targetHeight, targetWidth, w2xFormat, w2xNoise, w2xScale]);

  const getImageUrl = useCallback((bookId: string | undefined, pageNum: number) => {
    return getImageUrlForBook(bookId ?? '', pageNum);
  }, [getImageUrlForBook]);

  const clearImagesInCache = useCallback((cache: ReaderBookCache) => {
    cache.imageRequests.forEach(({ controller }) => controller.abort());
    cache.imageRequests.clear();
    cache.preloadedImageUrls.clear();
    cache.imageUrls.forEach((objectUrl) => {
      URL.revokeObjectURL(objectUrl);
    });
    cache.imageUrls.clear();
  }, []);

  const clearAllPageImageCaches = useCallback(() => {
    imageCacheGenerationRef.current += 1;
    readerBookCacheRef.current.forEach((cache) => clearImagesInCache(cache));
    setCachedPageImageUrls({});
  }, [clearImagesInCache]);

  const cachedImageUrlsForBook = useCallback((targetBookId: string, bookPages: Page[]) => {
    const cache = readerBookCacheRef.current.get(targetBookId);
    if (!cache) return {};
    const cachedUrls: Record<number, string> = {};
    bookPages.forEach((page) => {
      const objectUrl = cache.imageUrls.get(getImageUrlForBook(targetBookId, page.number));
      if (objectUrl) {
        cachedUrls[page.number] = objectUrl;
      }
    });
    return cachedUrls;
  }, [getImageUrlForBook]);

  const retainBookCaches = useCallback((bookIds: Array<string | null | undefined>) => {
    const keep = new Set(bookIds.filter((id): id is string => Boolean(id)));
    readerBookCacheRef.current.forEach((cache, cacheBookId) => {
      if (!keep.has(cacheBookId)) {
        clearImagesInCache(cache);
        readerBookCacheRef.current.delete(cacheBookId);
      }
    });
  }, [clearImagesInCache]);

  const releasePageImagesOutsideWindow = useCallback((targetBookId: string, keepPageNumbers: number[]) => {
    const cache = readerBookCacheRef.current.get(targetBookId);
    if (!cache) return;

    const keepPages = new Set(keepPageNumbers);
    const keepUrls = new Set<string>();
    keepPages.forEach((pageNumber) => {
      keepUrls.add(getImageUrlForBook(targetBookId, pageNumber));
    });

    cache.imageRequests.forEach(({ controller }, requestUrl) => {
      if (!keepUrls.has(requestUrl)) {
        controller.abort();
        cache.imageRequests.delete(requestUrl);
      }
    });
    cache.preloadedImageUrls.forEach((requestUrl) => {
      if (!keepUrls.has(requestUrl)) {
        cache.preloadedImageUrls.delete(requestUrl);
      }
    });
    cache.imageUrls.forEach((objectUrl, requestUrl) => {
      if (!keepUrls.has(requestUrl)) {
        URL.revokeObjectURL(objectUrl);
        cache.imageUrls.delete(requestUrl);
      }
    });

    if (targetBookId === currentBookIdRef.current) {
      setCachedPageImageUrls((prev) => {
        let changed = false;
        const next: Record<number, string> = {};
        Object.entries(prev).forEach(([pageNumber, objectUrl]) => {
          const page = Number(pageNumber);
          if (keepPages.has(page)) {
            next[page] = objectUrl;
          } else {
            changed = true;
          }
        });
        return changed ? next : prev;
      });
    }
  }, [currentBookIdRef, getImageUrlForBook]);

  const ensurePageImageLoaded = useCallback((targetBookId: string, pageNum: number) => {
    const cache = getBookCache(targetBookId);
    const requestUrl = getImageUrlForBook(targetBookId, pageNum);
    const cachedObjectUrl = cache.imageUrls.get(requestUrl);
    if (cachedObjectUrl) {
      if (targetBookId === currentBookIdRef.current) {
        setCachedPageImageUrls((prev) => prev[pageNum] ? prev : { ...prev, [pageNum]: cachedObjectUrl });
      }
      return Promise.resolve(cachedObjectUrl);
    }

    const inFlight = cache.imageRequests.get(requestUrl);
    if (inFlight) {
      return inFlight.promise;
    }

    const generation = imageCacheGenerationRef.current;
    const controller = new AbortController();
    const request = fetch(requestUrl, { signal: controller.signal })
      .then((response) => {
        if (!response.ok) {
          throw new Error(`Failed to load page ${pageNum}: ${response.status}`);
        }
        return response.blob();
      })
      .then((blob) => {
        const objectUrl = URL.createObjectURL(blob);
        if (generation !== imageCacheGenerationRef.current || !readerBookCacheRef.current.has(targetBookId)) {
          URL.revokeObjectURL(objectUrl);
          throw new DOMException('Stale reader image request', 'AbortError');
        }
        cache.imageUrls.set(requestUrl, objectUrl);
        if (targetBookId === currentBookIdRef.current) {
          setCachedPageImageUrls((prev) => {
            if (prev[pageNum] === objectUrl) {
              return prev;
            }
            return { ...prev, [pageNum]: objectUrl };
          });
        }
        return objectUrl;
      })
      .finally(() => {
        cache.imageRequests.delete(requestUrl);
      });

    cache.imageRequests.set(requestUrl, { promise: request, controller });
    return request;
  }, [currentBookIdRef, getBookCache, getImageUrlForBook]);

  const isPagedImageReady = useCallback((pageNum: number) => {
    return Boolean(cachedPageImageUrls[pageNum]);
  }, [cachedPageImageUrls]);

  return {
    cachedPageImageUrls,
    setCachedPageImageUrls,
    imageOptionsKey,
    getBookCache,
    getImageUrlForBook,
    getImageUrl,
    clearAllPageImageCaches,
    cachedImageUrlsForBook,
    retainBookCaches,
    releasePageImagesOutsideWindow,
    ensurePageImageLoaded,
    isPagedImageReady,
  };
}
