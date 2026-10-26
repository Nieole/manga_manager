import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient, isAxiosError } from '../../api/client';

export type ProgressSyncStatus = 'idle' | 'syncing' | 'synced' | 'offline-queued';

interface UseReaderProgressIndicatorOptions {
  bookId?: string;
  pagesBookIdRef: { current: string | null };
  loading: boolean;
  isOnline: boolean;
  offlineQueuedPage: number | null;
  queueOfflineReaderProgress: (page: number, occurredAt?: string) => void;
}

export interface UseReaderProgressIndicatorResult {
  status: ProgressSyncStatus;
  updateProgress: (pageNumber: number) => void;
}

const SYNCED_FLASH_MS = 1500;

export function useReaderProgressIndicator({
  bookId,
  pagesBookIdRef,
  loading,
  isOnline,
  offlineQueuedPage,
  queueOfflineReaderProgress,
}: UseReaderProgressIndicatorOptions): UseReaderProgressIndicatorResult {
  const [status, setStatus] = useState<ProgressSyncStatus>('idle');
  const viewGeneration = useRef(0);
  const inFlightRef = useRef(0);
  const syncedTimerRef = useRef<number | null>(null);

  useEffect(() => {
    if (offlineQueuedPage != null) {
       
      setStatus('offline-queued');
    }
  }, [offlineQueuedPage]);

  useEffect(() => {
    if (!isOnline) {
       
      setStatus((prev) => (prev === 'syncing' ? 'offline-queued' : prev));
    }
  }, [isOnline]);

  useEffect(() => {
     
    viewGeneration.current += 1;
    setStatus('idle');
    inFlightRef.current = 0;
    if (syncedTimerRef.current != null) {
      window.clearTimeout(syncedTimerRef.current);
      syncedTimerRef.current = null;
    }
    return () => {
      viewGeneration.current += 1;
      if (syncedTimerRef.current != null) {
        window.clearTimeout(syncedTimerRef.current);
        syncedTimerRef.current = null;
      }
    };
  }, [bookId]);

  const updateProgress = useCallback((pageNumber: number) => {
    if (!bookId || loading) return;
    if (bookId !== pagesBookIdRef.current) return;
    if (pageNumber <= 0) return;

    const occurredAt = new Date().toISOString();

    if (!isOnline) {
      queueOfflineReaderProgress(pageNumber, occurredAt);
      setStatus('offline-queued');
      return;
    }

    const generation = viewGeneration.current;
    inFlightRef.current += 1;
    setStatus('syncing');

    apiClient.post(`/api/books/${bookId}/progress`, { page: pageNumber, updated_at: occurredAt })
      .then(() => {
        if (generation !== viewGeneration.current) return;
        inFlightRef.current = Math.max(0, inFlightRef.current - 1);
        if (inFlightRef.current === 0) {
          setStatus('synced');
          if (syncedTimerRef.current != null) {
            window.clearTimeout(syncedTimerRef.current);
          }
          syncedTimerRef.current = window.setTimeout(() => {
            setStatus((prev) => (prev === 'synced' ? 'idle' : prev));
            syncedTimerRef.current = null;
          }, SYNCED_FLASH_MS);
        }
      })
      .catch((err) => {
        const networkFailure = !isAxiosError(err) || !err.response;
        if (networkFailure) queueOfflineReaderProgress(pageNumber, occurredAt);
        if (generation !== viewGeneration.current) return;
        inFlightRef.current = Math.max(0, inFlightRef.current - 1);
        if (networkFailure) {
          setStatus('offline-queued');
        } else if (inFlightRef.current === 0) {
          setStatus('idle');
        }
        console.error('Failed to update read progress', err);
      });
  }, [bookId, isOnline, loading, pagesBookIdRef, queueOfflineReaderProgress]);

  return { status, updateProgress };
}
