/**
 * 阅读器页图缓存。一条缓存的身份就是它的请求地址，因此参数换没换以 pageImageQuery 的结果为准，
 * 而不是以「哪几个参数变了」为准——档位、画质这些只在某些滤镜下才进地址。
 */

import { useCallback, useMemo, useRef, useState } from 'react';
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
  // imagesQuery 是这批 Object URL 当初的取图参数；与当前参数不符即整批作废。
  imagesQuery: string;
  // generation 每作废一次加一，用于丢弃跨过作废点的在途响应。按书各算各的：
  // 共用一个计数器时，另一本书的作废会把这本刚发出的请求也判成迟到。
  generation: number;
  imageUrls: Map<string, string>;
  imageRequests: Map<string, PageImageRequest>;
  preloadedImageUrls: Set<string>;
}

function createReaderBookCache(imagesQuery: string): ReaderBookCache {
  return {
    imagesQuery,
    generation: 0,
    imageUrls: new Map(),
    imageRequests: new Map(),
    preloadedImageUrls: new Set(),
  };
}

function isServerSideImageFilter(imageFilter: ImageFilter) {
  return !['none', 'nearest', 'average', 'bilinear'].includes(imageFilter);
}

interface PageImageQueryOptions {
  imageFilter: ImageFilter;
  w2xScale: number;
  w2xNoise: number;
  w2xFormat: string;
  autoCrop: boolean;
  readerImageFormat: ReaderImageFormat;
  readerImageQuality: number;
  targetWidth: number;
  targetHeight: number;
}

// pageImageQuery 是页图地址里 query 的唯一产地，也因此是缓存身份的唯一判据。
// 判据必须与地址同源：目标尺寸只在重采样滤镜下才进地址，另算一套身份会在其余滤镜下
// 无谓地把整份缓存清掉——换不来任何一个新地址。
function pageImageQuery({
  imageFilter,
  w2xScale,
  w2xNoise,
  w2xFormat,
  autoCrop,
  readerImageFormat,
  readerImageQuality,
  targetWidth,
  targetHeight,
}: PageImageQueryOptions): string {
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
  return params.toString();
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

  const imageQuery = useMemo(() => pageImageQuery({
    imageFilter,
    w2xScale,
    w2xNoise,
    w2xFormat,
    autoCrop,
    readerImageFormat,
    readerImageQuality,
    targetWidth,
    targetHeight,
  }), [autoCrop, imageFilter, readerImageFormat, readerImageQuality, targetHeight, targetWidth, w2xFormat, w2xNoise, w2xScale]);

  const clearImagesInCache = useCallback((cache: ReaderBookCache) => {
    cache.generation += 1;
    cache.imageRequests.forEach(({ controller }) => controller.abort());
    cache.imageRequests.clear();
    cache.preloadedImageUrls.clear();
    cache.imageUrls.forEach((objectUrl) => {
      URL.revokeObjectURL(objectUrl);
    });
    cache.imageUrls.clear();
  }, []);

  // getBookCache 是取到一本书缓存的唯一入口，作废也就落在这里：调用方一拿到缓存，里面的条目
  // 必定属于当前参数。作废因此是「当前状态的函数」而不是一次事件，谁先谁后跑都收敛到同一结果——
  // 挂一条 effect 去清的写法会与预热 effect 抢跑，清在后就把刚发出的请求判成迟到，当前页永久转圈。
  // 撤销 Object URL 与清空 cachedPageImageUrls 必须同处一次提交，否则 DOM 上的 <img> 会指向已撤销的地址。
  const getBookCache = useCallback((targetBookId: string) => {
    let cache = readerBookCacheRef.current.get(targetBookId);
    if (!cache) {
      cache = createReaderBookCache(imageQuery);
      readerBookCacheRef.current.set(targetBookId, cache);
      return cache;
    }
    if (cache.imagesQuery !== imageQuery) {
      cache.imagesQuery = imageQuery;
      clearImagesInCache(cache);
      if (targetBookId === currentBookIdRef.current) {
        setCachedPageImageUrls({});
      }
    }
    return cache;
  }, [clearImagesInCache, currentBookIdRef, imageQuery]);

  const getImageUrlForBook = useCallback((targetBookId: string, pageNum: number) => {
    return `/api/pages/${targetBookId}/${pageNum}${imageQuery ? `?${imageQuery}` : ''}`;
  }, [imageQuery]);

  const getImageUrl = useCallback((bookId: string | undefined, pageNum: number) => {
    return getImageUrlForBook(bookId ?? '', pageNum);
  }, [getImageUrlForBook]);

  const clearAllPageImageCaches = useCallback(() => {
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

    const generation = cache.generation;
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
        if (generation !== cache.generation || readerBookCacheRef.current.get(targetBookId) !== cache) {
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
