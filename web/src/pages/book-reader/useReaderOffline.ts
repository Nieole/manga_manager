import { useCallback, useEffect, useRef, useState } from 'react';
import {
  cacheBookForOffline,
  deleteOfflineBook,
  getOfflineBookStatus,
  getQueuedOfflineProgress,
  queueOfflineProgress,
  supportsOfflineReaderCache,
  syncQueuedOfflineProgress,
  type OfflineBookStatus,
} from './offlineReader';
import type { ImageFilter, Page, ReaderImageFormat } from './types';

interface UseReaderOfflineOptions {
  bookId?: string;
  bookTitle: string;
  pages: Page[];
  imageFilter: ImageFilter;
  autoCrop: boolean;
  readerImageFormat: ReaderImageFormat;
  readerImageQuality: number;
  getImageUrlForBook: (bookId: string, pageNumber: number) => string;
  t: (key: string) => string;
}

// dropKey 摘掉一格，键不在就原样返回——省一次无谓的重渲染。
function dropKey<T>(table: Record<string, T>, key: string): Record<string, T> {
  if (!(key in table)) return table;
  const next = { ...table };
  delete next[key];
  return next;
}

export function useReaderOffline({
  bookId,
  bookTitle,
  pages,
  imageFilter,
  autoCrop,
  readerImageFormat,
  readerImageQuality,
  getImageUrlForBook,
  t,
}: UseReaderOfflineOptions) {
  const offlineSupported = supportsOfflineReaderCache();
  // 下载是异步的，期间用户可能已经翻到别的书。收尾时拿它比一比，
  // 免得把上一本书的状态盖到当前这本上。
  const activeBookIdRef = useRef(bookId);
  const [offlineStatus, setOfflineStatus] = useState<OfflineBookStatus | null>(null);
  // 下载与删除按 bookId 分格存，而不是各存一份单值。
  //
  // cacheBookForOffline 在用户翻到下一本之后仍会跑完，这是有意的——用户点「缓存本书」要的
  // 就是它下完，而翻到末页会自动换书。既然下载跨书存活，「哪本在下、下到第几页、哪本失败了」
  // 就得跟着 bookId 走：单值状态会把上一本的进度与错误挂到新书的面板上，还把两个按钮一起
  // 锁死；而换书时把它清掉是另一种撒谎——后台明明还在下，切回来却写着「没在下载」。
  const [offlineCachingPages, setOfflineCachingPages] = useState<Record<string, number>>({});
  const [offlineDeletingBooks, setOfflineDeletingBooks] = useState<Record<string, true>>({});
  const [offlineCacheErrors, setOfflineCacheErrors] = useState<Record<string, string>>({});
  const [offlineQueuedPage, setOfflineQueuedPage] = useState<number | null>(null);
  const [isOnline, setIsOnline] = useState(() => typeof navigator === 'undefined' ? true : navigator.onLine);

  const offlineCaching = bookId ? bookId in offlineCachingPages : false;
  const offlineDeleting = bookId ? bookId in offlineDeletingBooks : false;
  const offlineCachedPages = bookId ? offlineCachingPages[bookId] ?? 0 : 0;
  const offlineCacheError = bookId ? offlineCacheErrors[bookId] ?? null : null;

  const refreshOfflineStatus = useCallback(() => {
    if (!bookId || !offlineSupported) {
      setOfflineStatus(null);
      setOfflineQueuedPage(null);
      return;
    }

    // 读回状态同样横跨一段 await：A→B→A 快切时，先发的那次可能后回来，
    // 落到 setOfflineStatus 就成了拿别的书的状态覆盖当前这本。
    getOfflineBookStatus(bookId)
      .then((status) => {
        if (activeBookIdRef.current !== bookId) return;
        setOfflineStatus(status);
      })
      .catch(() => {
        if (activeBookIdRef.current !== bookId) return;
        setOfflineStatus(null);
      });
    setOfflineQueuedPage(getQueuedOfflineProgress(bookId)?.page ?? null);
  }, [bookId, offlineSupported]);

  useEffect(() => {
    activeBookIdRef.current = bookId;
  }, [bookId]);

  useEffect(() => {
     
    refreshOfflineStatus();
  }, [refreshOfflineStatus]);

  useEffect(() => {
    if (typeof window === 'undefined' || typeof navigator === 'undefined') return undefined;

    const handleOnline = () => {
      setIsOnline(true);
      syncQueuedOfflineProgress()
        .catch((err) => console.error('Failed to sync queued offline progress', err))
        .finally(refreshOfflineStatus);
    };
    const handleOffline = () => setIsOnline(false);

    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);
    if (navigator.onLine) {
      void syncQueuedOfflineProgress().finally(refreshOfflineStatus);
    }

    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, [refreshOfflineStatus]);

  const queueProgress = useCallback((pageNumber: number) => {
    if (!bookId) return;
    queueOfflineProgress(bookId, pageNumber, bookTitle || undefined);
    setOfflineQueuedPage(pageNumber);
  }, [bookId, bookTitle]);

  const cacheBookOffline = useCallback(() => {
    if (!bookId || pages.length === 0) return;
    // 把这一本固定在闭包里：下面每个回调都可能在用户换书之后才跑，
    // 它们该写的是发起时那本书的那一格，而不是当时正在读的那本。
    const targetBookId = bookId;

    setOfflineCachingPages((prev) => ({ ...prev, [targetBookId]: 0 }));
    setOfflineCacheErrors((prev) => dropKey(prev, targetBookId));
    const imageProfile = [
      readerImageFormat === 'original' ? t('reader.networkOriginal') : `${readerImageFormat.toUpperCase()} ${readerImageQuality}`,
      imageFilter !== 'none' ? imageFilter : '',
      autoCrop ? 'crop' : '',
    ].filter(Boolean).join(' · ');

    cacheBookForOffline({
      bookId: targetBookId,
      title: bookTitle || t('reader.offline.untitled'),
      pages,
      imageProfile,
      imageUrlForPage: (page) => getImageUrlForBook(targetBookId, page.number),
      // 只在这一格还在时才补写：收尾已经把它摘掉的话，进度会把一本下完的书重新挂回「正在缓存」。
      onProgress: (cached) => setOfflineCachingPages((prev) => (
        targetBookId in prev ? { ...prev, [targetBookId]: cached } : prev
      )),
    }).then((status) => {
      if (activeBookIdRef.current !== targetBookId) return;
      // status 为 null 是「这次下载在收尾前被作废了」（用户删了这本书、或换了用户）：
      // 照样落到 setOfflineStatus，界面回到「未缓存」，被删掉的书不会复活成已缓存。
      setOfflineStatus(status);
    }).catch((err) => {
      const message = err instanceof Error ? err.message : t('reader.offline.cacheFailed');
      setOfflineCacheErrors((prev) => ({ ...prev, [targetBookId]: message }));
    }).finally(() => {
      setOfflineCachingPages((prev) => dropKey(prev, targetBookId));
    });
  }, [autoCrop, bookId, bookTitle, getImageUrlForBook, imageFilter, pages, readerImageFormat, readerImageQuality, t]);

  const deleteBookOffline = useCallback(() => {
    if (!bookId) return;
    const targetBookId = bookId;

    setOfflineDeletingBooks((prev) => ({ ...prev, [targetBookId]: true }));
    setOfflineCacheErrors((prev) => dropKey(prev, targetBookId));
    deleteOfflineBook(targetBookId)
      .then(() => {
        if (activeBookIdRef.current !== targetBookId) return;
        setOfflineStatus(null);
      })
      .catch((err) => {
        const message = err instanceof Error ? err.message : t('reader.offline.deleteFailed');
        setOfflineCacheErrors((prev) => ({ ...prev, [targetBookId]: message }));
      })
      .finally(() => setOfflineDeletingBooks((prev) => dropKey(prev, targetBookId)));
  }, [bookId, t]);

  return {
    isOnline,
    offlineSupported,
    offlineStatus,
    offlineCaching,
    offlineDeleting,
    offlineCachedPages,
    offlineQueuedPage,
    offlineCacheError,
    queueProgress,
    cacheBookOffline,
    deleteBookOffline,
    refreshOfflineStatus,
  };
}
