/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import axios from 'axios';
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
  const [bookmarks, setBookmarks] = useState<ReadingBookmark[]>([]);
  const [bookmarkNote, setBookmarkNote] = useState('');
  const [savingBookmark, setSavingBookmark] = useState(false);

  const currentBookmark = useMemo(
    () => bookmarks.find((item) => item.page === currentPageNumber) || null,
    [bookmarks, currentPageNumber],
  );

  const loadBookmarks = useCallback(() => {
    if (!bookId) return Promise.resolve();

    return axios.get<ReadingBookmark[]>(`/api/books/${bookId}/bookmarks`)
      .then((res) => {
        if (bookId === currentBookIdRef.current) {
          setBookmarks(res.data || []);
        }
      })
      .catch((err) => {
        console.error('Failed to load reading bookmarks', err);
        if (bookId === currentBookIdRef.current) {
          setBookmarks([]);
        }
      });
  }, [bookId, currentBookIdRef]);

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
    if (!bookId || activePageCount === 0) return;

    const targetBookId = bookId;
    setSavingBookmark(true);
    axios.post<ReadingBookmark>(`/api/books/${targetBookId}/bookmarks`, {
      page: currentPageNumber,
      note: bookmarkNote,
    }).then((res) => {
      if (targetBookId !== currentBookIdRef.current) return;
      setBookmarks((prev) => {
        const next = prev.filter((item) => item.id !== res.data.id && item.page !== res.data.page);
        next.push(res.data);
        return next.sort((a, b) => a.page - b.page);
      });
      setBookmarkNote('');
    }).catch((err) => {
      console.error('Failed to save reading bookmark', err);
    }).finally(() => {
      setSavingBookmark(false);
    });
  }, [activePageCount, bookId, bookmarkNote, currentBookIdRef, currentPageNumber]);

  const deleteBookmark = useCallback((bookmark: ReadingBookmark) => {
    if (!bookId) return;

    const targetBookId = bookId;
    axios.delete(`/api/books/${targetBookId}/bookmarks/${bookmark.id}`)
      .then(() => {
        if (targetBookId === currentBookIdRef.current) {
          setBookmarks((prev) => prev.filter((item) => item.id !== bookmark.id));
        }
      })
      .catch((err) => console.error('Failed to delete reading bookmark', err));
  }, [bookId, currentBookIdRef]);

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
