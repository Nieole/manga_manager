import { useCallback, useEffect, useMemo, useRef, useLayoutEffect, useState } from 'react';
import { useSessionActive } from '../../session/ApplicationSessionProvider';
import { apiClient } from '../../api/client';
import type { ReadingBookmark } from './types';

interface UseReaderBookmarksOptions {
  bookId?: string;
  currentBookIdRef: {
    current: string | null;
  };
  activePageCount: number;
  currentPageNumber: number;
}

export function useReaderBookmarks({
  bookId,
  currentBookIdRef,
  activePageCount,
  currentPageNumber,
}: UseReaderBookmarksOptions) {
  const isSessionActive = useSessionActive();
  const mounted = useRef(false);
  useLayoutEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const bookSession = useMemo(() => ({ bookId }), [bookId]);
  const currentSession = useRef(bookSession);
  useLayoutEffect(() => { currentSession.current = bookSession; }, [bookSession]);
  const isCurrent = useCallback((target: string) => mounted.current && currentSession.current === bookSession && isSessionActive() && target === currentBookIdRef.current, [bookSession, currentBookIdRef, isSessionActive]);
  const [bookmarks, setBookmarks] = useState<ReadingBookmark[]>([]);
  const [bookmarkNote, setBookmarkNote] = useState('');
  const [savingBookmark, setSavingBookmark] = useState(false);

  const currentBookmark = useMemo(
    () => bookmarks.find((item) => item.page === currentPageNumber) || null,
    [bookmarks, currentPageNumber],
  );

  const loadBookmarks = useCallback(() => {
    if (!bookId) return Promise.resolve();

    return apiClient.get<ReadingBookmark[]>(`/api/books/${bookId}/bookmarks`)
      .then((res) => {
        if (isCurrent(bookId)) {
          setBookmarks(res.data || []);
        }
      })
      .catch((err) => {
        console.error('Failed to load reading bookmarks', err);
        if (isCurrent(bookId)) {
          setBookmarks([]);
        }
      });
  }, [bookId, isCurrent]);

  useEffect(() => {
     
    setBookmarks([]);
    setBookmarkNote('');
    setSavingBookmark(false);
  }, [bookId]);

  useEffect(() => {
    void loadBookmarks();
  }, [loadBookmarks]);

  useEffect(() => {
     
    setBookmarkNote(currentBookmark?.note || '');
  }, [currentBookmark]);

  const saveBookmark = useCallback(() => {
    if (!bookId || !isCurrent(bookId) || activePageCount === 0) return;

    const targetBookId = bookId;
    setSavingBookmark(true);
    apiClient.post<ReadingBookmark>(`/api/books/${targetBookId}/bookmarks`, {
      page: currentPageNumber,
      note: bookmarkNote,
    }).then((res) => {
      if (!isCurrent(targetBookId)) return;
      setBookmarks((prev) => {
        const next = prev.filter((item) => item.id !== res.data.id && item.page !== res.data.page);
        next.push(res.data);
        return next.sort((a, b) => a.page - b.page);
      });
      setBookmarkNote('');
    }).catch((err) => {
      console.error('Failed to save reading bookmark', err);
    }).finally(() => {
      if (isCurrent(targetBookId)) setSavingBookmark(false);
    });
  }, [activePageCount, bookId, bookmarkNote, isCurrent, currentPageNumber]);

  const deleteBookmark = useCallback((bookmark: ReadingBookmark) => {
    if (!bookId || !isCurrent(bookId)) return;

    const targetBookId = bookId;
    apiClient.delete(`/api/books/${targetBookId}/bookmarks/${bookmark.id}`)
      .then(() => {
        if (isCurrent(targetBookId)) {
          setBookmarks((prev) => prev.filter((item) => item.id !== bookmark.id));
        }
      })
      .catch((err) => console.error('Failed to delete reading bookmark', err));
  }, [bookId, isCurrent]);

  const resetBookmarks = useCallback(() => {
    setBookmarks([]);
    setBookmarkNote('');
    setSavingBookmark(false);
  }, []);

  return {
    bookmarks,
    bookmarkNote,
    setBookmarkNote,
    savingBookmark,
    currentBookmark,
    saveBookmark,
    deleteBookmark,
    resetBookmarks,
  };
}
