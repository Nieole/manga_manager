import { useCallback, useRef } from 'react';
import { apiClient } from '../api/client';

interface Operation { operation_id: string; occurred_at: string; isRead: boolean }

/** Retain an uncertain operation until its request succeeds, so a retry preserves the original fact. */
export function useReadStateMutation() {
  const pending = useRef(new Map<string, Operation>());
  const mark = useCallback(async (kind: 'books' | 'series', ids: number[], isRead: boolean) => {
    const key = JSON.stringify([kind, [...ids].sort((a, b) => a - b)]);
    let operation = pending.current.get(key);
    if (!operation || operation.isRead !== isRead) {
      operation = {
        isRead,
        operation_id: Array.from(crypto.getRandomValues(new Uint32Array(4))).join('-'),
        occurred_at: new Date().toISOString(),
      };
      pending.current.set(key, operation);
    }
    await apiClient.post(`/api/${kind}/bulk-progress`, {
      [kind === 'books' ? 'book_ids' : 'series_ids']: ids,
      is_read: isRead,
      operation_id: operation.operation_id,
      occurred_at: operation.occurred_at,
    });
    if (pending.current.get(key) === operation) pending.current.delete(key);
  }, []);
  return {
    markBooks: useCallback((ids: number[], isRead: boolean) => mark('books', ids, isRead), [mark]),
    markSeries: useCallback((ids: number[], isRead: boolean) => mark('series', ids, isRead), [mark]),
  };
}
