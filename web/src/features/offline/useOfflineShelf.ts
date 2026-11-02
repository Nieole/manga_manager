import { useCallback, useEffect, useState } from 'react';
import {
  clearQueuedOfflineProgress,
  deleteAllOfflineBooks,
  deleteOfflineBook,
  deleteQueuedOfflineProgress,
  getOfflineReaderStorageStats,
  listQueuedOfflineProgress,
  listOfflineBooks,
  supportsOfflineReaderCache,
  syncQueuedOfflineProgress,
  type OfflineBookStatus,
  type OfflineProgressSyncResult,
  type OfflineReaderStorageStats,
  type QueuedProgress,
} from './offlineReader';

export function useOfflineShelf() {
  const [books, setBooks] = useState<OfflineBookStatus[]>([]);
  const [queuedProgress, setQueuedProgress] = useState<QueuedProgress[]>([]);
  const [stats, setStats] = useState<OfflineReaderStorageStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deletingProgressId, setDeletingProgressId] = useState<string | null>(null);
  const [clearingAll, setClearingAll] = useState(false);
  const [clearingProgress, setClearingProgress] = useState(false);
  const [syncingProgress, setSyncingProgress] = useState(false);
  const [syncResult, setSyncResult] = useState<OfflineProgressSyncResult | null>(null);
  const [isOnline, setIsOnline] = useState(() => typeof navigator === 'undefined' ? true : navigator.onLine);
  const offlineSupported = supportsOfflineReaderCache();

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [nextBooks, nextStats] = await Promise.all([
        listOfflineBooks(),
        getOfflineReaderStorageStats(),
      ]);
      setBooks(nextBooks);
      setStats(nextStats);
      setQueuedProgress(listQueuedOfflineProgress());
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    const handleOnline = () => setIsOnline(true);
    const handleOffline = () => setIsOnline(false);
    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);
    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, []);

  const handleDeleteBook = useCallback(async (bookId: string) => {
    setDeletingId(bookId);
    try {
      await deleteOfflineBook(bookId);
      await refresh();
    } finally {
      setDeletingId(null);
    }
  }, [refresh]);

  const handleClearAll = useCallback(async () => {
    setClearingAll(true);
    try {
      await deleteAllOfflineBooks();
      await refresh();
    } finally {
      setClearingAll(false);
    }
  }, [refresh]);

  const handleSyncProgress = useCallback(async () => {
    setSyncingProgress(true);
    try {
      const result = await syncQueuedOfflineProgress();
      setSyncResult(result);
      await refresh();
    } finally {
      setSyncingProgress(false);
    }
  }, [refresh]);

  const handleDeleteQueuedProgress = useCallback(async (bookId: string) => {
    setDeletingProgressId(bookId);
    try {
      deleteQueuedOfflineProgress(bookId);
      setSyncResult(null);
      await refresh();
    } finally {
      setDeletingProgressId(null);
    }
  }, [refresh]);

  const handleClearQueuedProgress = useCallback(async () => {
    setClearingProgress(true);
    try {
      clearQueuedOfflineProgress();
      setSyncResult(null);
      await refresh();
    } finally {
      setClearingProgress(false);
    }
  }, [refresh]);

  return {
    books,
    queuedProgress,
    stats,
    loading,
    deletingId,
    deletingProgressId,
    clearingAll,
    clearingProgress,
    syncingProgress,
    syncResult,
    isOnline,
    offlineSupported,
    refresh,
    handleDeleteBook,
    handleClearAll,
    handleSyncProgress,
    handleDeleteQueuedProgress,
    handleClearQueuedProgress,
  };
}
