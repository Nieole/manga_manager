import { useCallback, useEffect, useRef, useState } from 'react';
import axios from 'axios';
import type { ReaderBookCache } from './usePageImageCache';
import type { Page, ReaderBookInfo } from './types';

interface UseReaderBookDataOptions {
  bookId?: string;
  currentBookIdRef: {
    current: string | null;
  };
  tRef: {
    current: (key: string) => string;
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
  const readerLoading = loading || Boolean(bookId && !pagesBelongToCurrentBook);

  const fetchPagesForBook = useCallback((targetBookId: string) => {
    const cache = getBookCache(targetBookId);
    if (cache.pages) {
      return Promise.resolve(cache.pages);
    }

    return axios.get<Page[]>(`/api/pages/${targetBookId}`).then((res) => {
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

    return axios.get<ReaderBookInfo>(`/api/book-info/${targetBookId}`).then((res) => {
      cache.bookInfo = res.data;
      return res.data;
    });
  }, [getBookCache]);

  const fetchNextBookIdForBook = useCallback((targetBookId: string) => {
    const cache = getBookCache(targetBookId);
    if (cache.nextBookId !== undefined) {
      return Promise.resolve(cache.nextBookId);
    }

    return axios.get<ReaderBookInfo>(`/api/book-next/${targetBookId}`)
      .then((res) => {
        cache.nextBookId = res.data.id ?? null;
        return cache.nextBookId;
      })
      .catch((err) => {
        if (!axios.isAxiosError(err) || err.response?.status !== 404) {
          console.error('Failed to load next book', err);
        }
        cache.nextBookId = null;
        return null;
      });
  }, [getBookCache]);

  useEffect(() => {
    if (!bookId) return undefined;
    const targetBookId = bookId;
    let cancelled = false;

    // eslint-disable-next-line react-hooks/set-state-in-effect
    setPagesBookId(null);
    pagesBookIdRef.current = null;
    setPages([]);
    setCachedPageImageUrls({});
    setLoading(true);
    setLoadError(null);
    setCurrentPageIndex(0);
    setNextBookId(null);
    nextBookIdRef.current = null;
    setBookTitle('');
    setBookVolume('');
    retainBookCaches([targetBookId]);

    Promise.all([
      fetchPagesForBook(targetBookId),
      fetchBookInfoForBook(targetBookId),
    ]).then(([sorted, bookInfo]) => {
      if (cancelled || currentBookIdRef.current !== targetBookId) return;
      if (sorted.length === 0) {
        setLoadError(tRef.current('reader.error.noPages'));
        setLoading(false);
        return;
      }

      setPages(sorted);
      setCachedPageImageUrls(cachedImageUrlsForBook(targetBookId, sorted));
      setPagesBookId(targetBookId);
      pagesBookIdRef.current = targetBookId;

      const lastPage = bookInfo.last_read_page?.Valid ? bookInfo.last_read_page.Int64 ?? 1 : 1;
      seriesIdRef.current = bookInfo.series_id || null;
      setBookTitle(bookInfo.title?.Valid ? bookInfo.title.String ?? bookInfo.name : bookInfo.name);
      setBookVolume(bookInfo.volume || '');

      fetchNextBookIdForBook(targetBookId).then((nextId) => {
        if (cancelled || currentBookIdRef.current !== targetBookId) return;
        setNextBookId(nextId);
        nextBookIdRef.current = nextId;
        retainBookCaches([targetBookId, nextId ? String(nextId) : null]);
      });

      if (lastPage > 0) {
        const safePage = Math.min(lastPage, sorted.length > 0 ? sorted.length : lastPage);
        const targetIdx = safePage - 1;
        setSliderValue(safePage);
        setCurrentPageIndex(Math.max(0, targetIdx));
      }

      setLoading(false);
    }).catch((err) => {
      if (cancelled || currentBookIdRef.current !== targetBookId) return;
      console.error('Failed to load book data', err);
      const message = axios.isAxiosError(err)
        ? err.response?.data?.error || err.message || tRef.current('reader.error.loadFailed')
        : tRef.current('reader.error.loadFailed');
      setLoadError(message);
      setLoading(false);
    });

    return () => {
      cancelled = true;
    };
  }, [
    bookId,
    cachedImageUrlsForBook,
    currentBookIdRef,
    fetchBookInfoForBook,
    fetchNextBookIdForBook,
    fetchPagesForBook,
    retainBookCaches,
    setCachedPageImageUrls,
    setCurrentPageIndex,
    setSliderValue,
    tRef,
  ]);

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
