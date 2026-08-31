import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient, isAxiosError } from '../../api/client';
import { storageFailureMessageFromError } from '../../api/storageFailure';
import type { ReaderBookCache } from './usePageImageCache';
import type { Page, ReaderBookInfo } from './types';

interface UseReaderBookDataOptions {
  bookId?: string;
  currentBookIdRef: {
    current: string | null;
  };
  tRef: {
    current: (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;
  };
  getBookCache: (bookId: string) => ReaderBookCache;
  setCachedPageImageUrls: (urls: Record<number, string>) => void;
  cachedImageUrlsForBook: (bookId: string, pages: Page[]) => Record<number, string>;
  retainBookCaches: (bookIds: Array<string | null | undefined>) => void;
  setCurrentPageIndex: (index: number) => void;
  setSliderValue: (page: number) => void;
}

export function useReaderBookData({
  bookId,
  currentBookIdRef,
  tRef,
  getBookCache,
  setCachedPageImageUrls,
  cachedImageUrlsForBook,
  retainBookCaches,
  setCurrentPageIndex,
  setSliderValue,
}: UseReaderBookDataOptions) {
  const [pages, setPages] = useState<Page[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [nextBookId, setNextBookId] = useState<number | null>(null);
  const [bookTitle, setBookTitle] = useState('');
  const [bookVolume, setBookVolume] = useState('');
  const [pagesBookId, setPagesBookId] = useState<string | null>(null);
  const pagesBookIdRef = useRef<string | null>(null);
  const seriesIdRef = useRef<number | null>(null);
  const nextBookIdRef = useRef<number | null>(null);

  const pagesBelongToCurrentBook = Boolean(bookId && bookId === pagesBookId);
  const activePages = pagesBelongToCurrentBook ? pages : [];
  // 有 loadError 就必须退出加载态：失败路径上 pagesBookId 永远补不上，
  // 少了这个前置条件 readerLoading 会恒为 true，而两套主题都先判 readerLoading 再判
  // loadError——用户看到的就是永远转不完的圈（全屏深色底，即「黑屏」），
  // 错误界面与 reader.error.* 那几条词条一次也渲染不到。
  const readerLoading = !loadError && (loading || Boolean(bookId && !pagesBelongToCurrentBook));

  const fetchPagesForBook = useCallback((targetBookId: string) => {
    const cache = getBookCache(targetBookId);
    if (cache.pages) {
      return Promise.resolve(cache.pages);
    }

    return apiClient.get<Page[]>(`/api/pages/${targetBookId}`).then((res) => {
      const sorted = [...res.data].sort((a, b) => a.number - b.number);
      cache.pages = sorted;
      return sorted;
    });
  }, [getBookCache]);

  const fetchBookInfoForBook = useCallback((targetBookId: string) => {
    const cache = getBookCache(targetBookId);
    if (cache.bookInfo) {
      return Promise.resolve(cache.bookInfo);
    }

    return apiClient.get<ReaderBookInfo>(`/api/book-info/${targetBookId}`).then((res) => {
      cache.bookInfo = res.data;
      return res.data;
    });
  }, [getBookCache]);

  const fetchNextBookIdForBook = useCallback((targetBookId: string) => {
    const cache = getBookCache(targetBookId);
    if (cache.nextBookId !== undefined) {
      return Promise.resolve(cache.nextBookId);
    }

    return apiClient.get<ReaderBookInfo>(`/api/book-next/${targetBookId}`)
      .then((res) => {
        cache.nextBookId = res.data.id ?? null;
        return cache.nextBookId;
      })
      .catch((err) => {
        if (!isAxiosError(err) || err.response?.status !== 404) {
          console.error('Failed to load next book', err);
        }
        cache.nextBookId = null;
        return null;
      });
  }, [getBookCache]);

  // 主加载 effect 只应在切换书籍（bookId 变化）时执行。但其内部用到的若干回调
  // （cachedImageUrlsForBook / fetch* 等）会随图像处理参数（imageFilter、waifu2x
  // 等，经 getImageUrlForBook）变化而重建引用；若把它们放进依赖数组，切换滤镜会让
  // 加载 effect 重跑并执行 setCurrentPageIndex(0)，导致翻页模式被冲回第一页。
  // 这里用 ref 固定最新引用，使加载 effect 仅依赖 bookId。
  const loadDepsRef = useRef({
    fetchPagesForBook,
    fetchBookInfoForBook,
    fetchNextBookIdForBook,
    cachedImageUrlsForBook,
    retainBookCaches,
    setCachedPageImageUrls,
    setCurrentPageIndex,
    setSliderValue,
  });
  loadDepsRef.current = {
    fetchPagesForBook,
    fetchBookInfoForBook,
    fetchNextBookIdForBook,
    cachedImageUrlsForBook,
    retainBookCaches,
    setCachedPageImageUrls,
    setCurrentPageIndex,
    setSliderValue,
  };

  useEffect(() => {
    if (!bookId) return undefined;
    const targetBookId = bookId;
    const deps = loadDepsRef.current;
    let cancelled = false;

    setPagesBookId(null);
    pagesBookIdRef.current = null;
    setPages([]);
    deps.setCachedPageImageUrls({});
    setLoading(true);
    setLoadError(null);
    deps.setCurrentPageIndex(0);
    setNextBookId(null);
    nextBookIdRef.current = null;
    setBookTitle('');
    setBookVolume('');
    deps.retainBookCaches([targetBookId]);

    Promise.all([
      deps.fetchPagesForBook(targetBookId),
      deps.fetchBookInfoForBook(targetBookId),
    ]).then(([sorted, bookInfo]) => {
      if (cancelled || currentBookIdRef.current !== targetBookId) return;
      if (sorted.length === 0) {
        setLoadError(tRef.current('reader.error.noPages'));
        setLoading(false);
        return;
      }

      setPages(sorted);
      deps.setCachedPageImageUrls(deps.cachedImageUrlsForBook(targetBookId, sorted));
      setPagesBookId(targetBookId);
      pagesBookIdRef.current = targetBookId;

      const lastPage = bookInfo.last_read_page?.Valid ? bookInfo.last_read_page.Int64 ?? 1 : 1;
      seriesIdRef.current = bookInfo.series_id || null;
      setBookTitle(bookInfo.title?.Valid ? bookInfo.title.String ?? bookInfo.name : bookInfo.name);
      setBookVolume(bookInfo.volume || '');

      deps.fetchNextBookIdForBook(targetBookId).then((nextId) => {
        if (cancelled || currentBookIdRef.current !== targetBookId) return;
        setNextBookId(nextId);
        nextBookIdRef.current = nextId;
        deps.retainBookCaches([targetBookId, nextId ? String(nextId) : null]);
      });

      if (lastPage > 0) {
        const safePage = Math.min(lastPage, sorted.length > 0 ? sorted.length : lastPage);
        const targetIdx = safePage - 1;
        deps.setSliderValue(safePage);
        deps.setCurrentPageIndex(Math.max(0, targetIdx));
      }

      setLoading(false);
    }).catch((err) => {
      if (cancelled || currentBookIdRef.current !== targetBookId) return;
      console.error('Failed to load book data', err);
      // 存储失败（盘掉了 / 文件没了 / 归档打不开）有分类，能说出该去做什么；
      // 其余错误才退回后端的 error 字段与通用文案。
      const storageMessage = storageFailureMessageFromError(err, tRef.current);
      const message = storageMessage
        ?? (isAxiosError(err)
          ? err.response?.data?.error || err.message || tRef.current('reader.error.loadFailed')
          : tRef.current('reader.error.loadFailed'));
      setLoadError(message);
      setLoading(false);
    });

    return () => {
      cancelled = true;
    };
    // 仅在切换书籍时重新加载；effect 内部用到的回调均通过 loadDepsRef 取最新引用，
    // 故意不放进依赖数组，避免切换图像滤镜（waifu2x 等）时重跑加载逻辑导致页码被重置。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bookId]);

  return {
    pages,
    activePages,
    loading,
    loadError,
    bookTitle,
    bookVolume,
    nextBookId,
    pagesBelongToCurrentBook,
    readerLoading,
    pagesBookIdRef,
    seriesIdRef,
    nextBookIdRef,
    fetchPagesForBook,
    fetchBookInfoForBook,
  };
}
