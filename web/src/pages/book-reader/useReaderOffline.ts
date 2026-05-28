import { useCallback, useEffect, useState } from 'react';
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
    // eslint-disable-next-line react-hooks/set-state-in-effect
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
