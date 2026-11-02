import { useCallback, useRef } from 'react';
import { useLibraryInvalidation } from '../session/ApplicationSessionProvider';
import { isAxiosError, apiClient } from '../api/client';

interface MarkResponse { committed_book_ids?: number[]; updated?: number; warnings?: number }
export interface MarkOutcome { committed: boolean; incomplete: boolean; warnings: boolean }

interface Operation { operation_id: string; occurred_at: string; isRead: boolean }

/** Retain an uncertain operation until its request succeeds, so a retry preserves the original fact. */
export function useReadStateMutation() {
  const invalidateLibrary = useLibraryInvalidation();
  const pending = useRef(new Map<string, Operation>());
  const mark = useCallback(async (kind: 'books' | 'series', ids: number[], isRead: boolean, refreshLocal?: () => Promise<void>) => {
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
    let data: MarkResponse | undefined;
    let incomplete = false;
    try {
      const response = await apiClient.post<MarkResponse>(`/api/${kind}/bulk-progress`, {
        [kind === 'books' ? 'book_ids' : 'series_ids']: ids,
        is_read: isRead,
        operation_id: operation.operation_id,
        occurred_at: operation.occurred_at,
      });
      data = response.data;
    } catch (err) {
      const response = isAxiosError<MarkResponse>(err) ? err.response?.data : undefined;
      if (!response?.committed_book_ids?.length) throw err;
      data = response;
      incomplete = true;
    }
    const committed = Array.isArray(data?.committed_book_ids)
      ? data.committed_book_ids.length > 0 : (data?.updated ?? 0) > 0;
    // A partial/uncertain request must replay the same fact, including books already committed.
    if (!incomplete && pending.current.get(key) === operation) pending.current.delete(key);
    try {
      // The action's authoritative refresh must settle before its own shared invalidation.
      if (committed || !incomplete) await refreshLocal?.();
    } finally {
      if (committed) invalidateLibrary({ areas: ['catalog', 'collections', 'organize', 'stats', 'reading-lists'] });
    }
    return { committed, incomplete, warnings: (data?.warnings ?? 0) > 0 };
  }, [invalidateLibrary]);
  return {
    markBooks: useCallback((ids: number[], isRead: boolean, refreshLocal?: () => Promise<void>) => mark('books', ids, isRead, refreshLocal), [mark]),
    markSeries: useCallback((ids: number[], isRead: boolean, refreshLocal?: () => Promise<void>) => mark('series', ids, isRead, refreshLocal), [mark]),
  };
}
