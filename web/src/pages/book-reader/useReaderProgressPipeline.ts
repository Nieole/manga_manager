/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import { useEffect } from 'react';
import type { ReaderBookCache } from './usePageImageCache';
import type { Page, ReaderBookInfo, ReadMode } from './types';
import { reportedProgressPage } from './readerProgress';

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
  updateProgress: (pageNumber: number) => void;
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
  getBookCache,
  getImageUrlForBook,
  ensurePageImageLoaded,
  fetchPagesForBook,
  fetchBookInfoForBook,
  retainBookCaches,
  updateProgress,
}: UseReaderProgressPipelineOptions) {

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
    if (!loading && pagesBelongToCurrentBook && pages.length > 0 && pages[currentPageIndex]) {
      // 双页翻页模式上报跨页里更靠后那页，确保末页能达到 page_count（否则书永不算读完）。
      const pageNumber = reportedProgressPage(pages, currentPageIndex, readMode === 'paged', doublePage);
      if (pageNumber != null) {
        const timer = setTimeout(() => {
          updateProgress(pageNumber);
        }, 1000);
        return () => clearTimeout(timer);
      }
    }

    return undefined;
  }, [currentPageIndex, loading, pages, pagesBelongToCurrentBook, readMode, doublePage, updateProgress]);
}
