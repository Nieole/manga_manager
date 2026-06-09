/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import axios from 'axios';

export type ProgressSyncStatus = 'idle' | 'syncing' | 'synced' | 'offline-queued';

interface UseReaderProgressIndicatorOptions {
  bookId?: string;
  pagesBookIdRef: { current: string | null };
  loading: boolean;
  isOnline: boolean;
  offlineQueuedPage: number | null;
  queueOfflineReaderProgress: (page: number) => void;
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
     
    setStatus('idle');
    inFlightRef.current = 0;
    if (syncedTimerRef.current != null) {
      window.clearTimeout(syncedTimerRef.current);
      syncedTimerRef.current = null;
    }
    return () => {
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

    if (!isOnline) {
      queueOfflineReaderProgress(pageNumber);
      setStatus('offline-queued');
      return;
    }

    inFlightRef.current += 1;
    setStatus('syncing');

    axios.post(`/api/books/${bookId}/progress`, { page: pageNumber })
      .then(() => {
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
        inFlightRef.current = Math.max(0, inFlightRef.current - 1);
        if (!axios.isAxiosError(err) || !err.response) {
          queueOfflineReaderProgress(pageNumber);
          setStatus('offline-queued');
        } else if (inFlightRef.current === 0) {
          setStatus('idle');
        }
        console.error('Failed to update read progress', err);
      });
  }, [bookId, isOnline, loading, pagesBookIdRef, queueOfflineReaderProgress]);

  return { status, updateProgress };
}
