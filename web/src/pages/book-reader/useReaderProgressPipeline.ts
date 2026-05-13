import { useCallback, useEffect, useRef } from 'react';
import axios from 'axios';
import type { ReaderBookCache } from './usePageImageCache';
import type { Page, ReaderBookInfo, ReadMode } from './types';

function isIgnoredImageLoadError(err: unknown) {
  return err instanceof DOMException && err.name === 'AbortError';
}

interface UseReaderProgressPipelineOptions {
  bookId?: string;
  loading: boolean;
  pages: Page[];
  pagesBelongToCurrentBook: boolean;
  currentPageIndex: number;
  readMode: ReadMode;
  doublePage: boolean;
  preloadCount: number;
  nextBookId: number | null;
  pagesBookIdRef: {
    current: string | null;
  };
  getBookCache: (bookId: string) => ReaderBookCache;
  getImageUrlForBook: (bookId: string, pageNumber: number) => string;
  ensurePageImageLoaded: (bookId: string, pageNumber: number) => Promise<string>;
  fetchPagesForBook: (bookId: string) => Promise<Page[]>;
  fetchBookInfoForBook: (bookId: string) => Promise<ReaderBookInfo>;
  retainBookCaches: (bookIds: Array<string | null | undefined>) => void;
  setCurrentPageIndex: (index: number) => void;
  queueOfflineReaderProgress: (pageNumber: number) => void;
}

export function useReaderProgressPipeline({
  bookId,
  loading,
  pages,
  pagesBelongToCurrentBook,
  currentPageIndex,
  readMode,
  doublePage,
  preloadCount,
  nextBookId,
  pagesBookIdRef,
  getBookCache,
  getImageUrlForBook,
  ensurePageImageLoaded,
  fetchPagesForBook,
  fetchBookInfoForBook,
  retainBookCaches,
  setCurrentPageIndex,
  queueOfflineReaderProgress,
}: UseReaderProgressPipelineOptions) {
  const observer = useRef<IntersectionObserver | null>(null);

  const updateProgress = useCallback((pageNumber: number) => {
    if (!bookId || loading) return;
    if (bookId !== pagesBookIdRef.current) return;
    if (pageNumber <= 0) return;

    axios.post(`/api/books/${bookId}/progress`, { page: pageNumber })
      .catch((err) => {
        if (!axios.isAxiosError(err) || !err.response) {
          queueOfflineReaderProgress(pageNumber);
        }
        console.error('Failed to update read progress', err);
      });
  }, [bookId, loading, pagesBookIdRef, queueOfflineReaderProgress]);

  useEffect(() => {
    if (!bookId || !pagesBelongToCurrentBook || !pages.length || preloadCount <= 0 || loading) return undefined;

    const timer = setTimeout(() => {
      const cache = getBookCache(bookId);
      const startIndex = currentPageIndex + (readMode === 'paged' && doublePage ? 2 : 1);
      const endIndex = Math.min(startIndex + preloadCount, pages.length);
      for (let i = startIndex; i < endIndex; i += 1) {
        const imageUrl = getImageUrlForBook(bookId, pages[i].number);
        if (cache.preloadedImageUrls.has(imageUrl)) {
          continue;
        }
        cache.preloadedImageUrls.add(imageUrl);
        ensurePageImageLoaded(bookId, pages[i].number).catch((err) => {
          if (!isIgnoredImageLoadError(err)) {
            console.error('Failed to preload reader page image', err);
          }
          cache.preloadedImageUrls.delete(imageUrl);
        });
      }
    }, 300);

    return () => clearTimeout(timer);
  }, [
    bookId,
    currentPageIndex,
    doublePage,
    ensurePageImageLoaded,
    getBookCache,
    getImageUrlForBook,
    loading,
    pages,
    pagesBelongToCurrentBook,
    preloadCount,
    readMode,
  ]);

  useEffect(() => {
    if (!bookId || !pagesBelongToCurrentBook || loading || pages.length === 0 || !pages[currentPageIndex]) return;

    ensurePageImageLoaded(bookId, pages[currentPageIndex].number).catch((err) => {
      if (!isIgnoredImageLoadError(err)) {
        console.error('Failed to warm current reader page image', err);
      }
    });
    if (readMode === 'paged' && doublePage && pages[currentPageIndex + 1]) {
      ensurePageImageLoaded(bookId, pages[currentPageIndex + 1].number).catch((err) => {
        if (!isIgnoredImageLoadError(err)) {
          console.error('Failed to warm secondary reader page image', err);
        }
      });
    }
  }, [bookId, currentPageIndex, doublePage, ensurePageImageLoaded, loading, pages, pagesBelongToCurrentBook, readMode]);

  useEffect(() => {
    if (!bookId || !pagesBelongToCurrentBook || loading || pages.length === 0 || !nextBookId) return undefined;
    if (preloadCount <= 0) return undefined;
    const visiblePageCount = readMode === 'paged' && doublePage ? 2 : 1;
    const remainingPages = Math.max(0, pages.length - (currentPageIndex + visiblePageCount));
    const nextBookPreloadCount = preloadCount - remainingPages;
    if (nextBookPreloadCount <= 0) return undefined;

    const nextBookIdString = String(nextBookId);
    let cancelled = false;

    Promise.all([
      fetchPagesForBook(nextBookIdString),
      fetchBookInfoForBook(nextBookIdString),
    ]).then(([nextPages]) => {
      if (cancelled) return;
      retainBookCaches([bookId, nextBookIdString]);

      const nextCache = getBookCache(nextBookIdString);
      nextPages.slice(0, nextBookPreloadCount).forEach((page) => {
        const imageUrl = getImageUrlForBook(nextBookIdString, page.number);
        if (nextCache.preloadedImageUrls.has(imageUrl)) {
          return;
        }
        nextCache.preloadedImageUrls.add(imageUrl);
        ensurePageImageLoaded(nextBookIdString, page.number).catch((err) => {
          if (!isIgnoredImageLoadError(err)) {
            console.error('Failed to preload next book reader page image', err);
          }
          nextCache.preloadedImageUrls.delete(imageUrl);
        });
      });
    }).catch((err) => {
      if (!cancelled) {
        console.error('Failed to warm next reader book', err);
      }
    });

    return () => {
      cancelled = true;
    };
  }, [
    bookId,
    currentPageIndex,
    doublePage,
    ensurePageImageLoaded,
    fetchBookInfoForBook,
    fetchPagesForBook,
    getBookCache,
    getImageUrlForBook,
    loading,
    nextBookId,
    pages.length,
    pagesBelongToCurrentBook,
    preloadCount,
    readMode,
    retainBookCaches,
  ]);

  useEffect(() => {
    if (loading || !pagesBelongToCurrentBook || pages.length === 0 || readMode !== 'webtoon') return undefined;

    let debounceTimeout: number;
    const options = {
      root: null,
      rootMargin: '0px',
      threshold: 0.5,
    };

    observer.current = new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          const pageNumStr = entry.target.getAttribute('data-page-number');
          if (pageNumStr) {
            const pageNum = parseInt(pageNumStr, 10);
            setCurrentPageIndex(pageNum - 1);
            clearTimeout(debounceTimeout);
            debounceTimeout = window.setTimeout(() => {
              updateProgress(pageNum);
            }, 1000);
          }
        }
      });
    }, options);

    const imgs = document.querySelectorAll('.ReaderScrollContainer img');
    imgs.forEach((img) => observer.current?.observe(img));

    return () => {
      observer.current?.disconnect();
      clearTimeout(debounceTimeout);
    };
  }, [loading, pages, pagesBelongToCurrentBook, readMode, setCurrentPageIndex, updateProgress]);

  useEffect(() => {
    if (!loading && readMode === 'paged' && pagesBelongToCurrentBook && pages.length > 0 && pages[currentPageIndex]) {
      const timer = setTimeout(() => {
        updateProgress(pages[currentPageIndex].number);
      }, 1000);
      return () => clearTimeout(timer);
    }

    return undefined;
  }, [currentPageIndex, loading, pages, pagesBelongToCurrentBook, readMode, updateProgress]);
}
