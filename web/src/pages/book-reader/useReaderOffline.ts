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
  const [offlineCaching, setOfflineCaching] = useState(false);
  const [offlineDeleting, setOfflineDeleting] = useState(false);
  const [offlineCachedPages, setOfflineCachedPages] = useState(0);
  const [offlineCacheError, setOfflineCacheError] = useState<string | null>(null);
  const [offlineQueuedPage, setOfflineQueuedPage] = useState<number | null>(null);
  const [isOnline, setIsOnline] = useState(() => typeof navigator === 'undefined' ? true : navigator.onLine);

  const refreshOfflineStatus = useCallback(() => {
    if (!bookId || !offlineSupported) {
      setOfflineStatus(null);
      setOfflineQueuedPage(null);
      return;
    }

    getOfflineBookStatus(bookId)
      .then(setOfflineStatus)
      .catch(() => setOfflineStatus(null));
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

    setOfflineCaching(true);
    setOfflineCacheError(null);
    setOfflineCachedPages(0);
    const imageProfile = [
      readerImageFormat === 'original' ? t('reader.networkOriginal') : `${readerImageFormat.toUpperCase()} ${readerImageQuality}`,
      imageFilter !== 'none' ? imageFilter : '',
      autoCrop ? 'crop' : '',
    ].filter(Boolean).join(' · ');

    cacheBookForOffline({
      bookId,
      title: bookTitle || t('reader.offline.untitled'),
      pages,
      imageProfile,
      imageUrlForPage: (page) => getImageUrlForBook(bookId, page.number),
      onProgress: (cached) => setOfflineCachedPages(cached),
    }).then((status) => {
      if (activeBookIdRef.current !== bookId) return;
      // status 为 null 是「这次下载在收尾前被作废了」（用户删了这本书、或换了用户）：
      // 照样落到 setOfflineStatus，界面回到「未缓存」，被删掉的书不会复活成已缓存。
      setOfflineStatus(status);
    }).catch((err) => {
      const message = err instanceof Error ? err.message : t('reader.offline.cacheFailed');
      setOfflineCacheError(message);
    }).finally(() => {
      setOfflineCaching(false);
      setOfflineCachedPages(0);
    });
  }, [autoCrop, bookId, bookTitle, getImageUrlForBook, imageFilter, pages, readerImageFormat, readerImageQuality, t]);

  const deleteBookOffline = useCallback(() => {
    if (!bookId) return;

    setOfflineDeleting(true);
    setOfflineCacheError(null);
    deleteOfflineBook(bookId)
      .then(() => setOfflineStatus(null))
      .catch((err) => {
        const message = err instanceof Error ? err.message : t('reader.offline.deleteFailed');
        setOfflineCacheError(message);
      })
      .finally(() => setOfflineDeleting(false));
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
