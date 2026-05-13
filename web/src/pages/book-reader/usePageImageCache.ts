import { useCallback, useRef, useState } from 'react';
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

interface UsePageImageCacheOptions {
  imageFilter: ImageFilter;
  w2xScale: number;
  w2xNoise: number;
  w2xFormat: string;
  autoCrop: boolean;
  readerImageFormat: ReaderImageFormat;
  readerImageQuality: number;
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
    if (imageFilter && imageFilter !== 'none') {
      params.set('filter', imageFilter);
      if (imageFilter === 'waifu2x' || imageFilter === 'realcugan') {
        params.set('w2x_scale', String(w2xScale));
        params.set('w2x_noise', String(w2xNoise));
        params.set('w2x_format', w2xFormat);
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
  }, [autoCrop, imageFilter, readerImageFormat, readerImageQuality, w2xFormat, w2xNoise, w2xScale]);

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
    ensurePageImageLoaded,
    isPagedImageReady,
  };
}
