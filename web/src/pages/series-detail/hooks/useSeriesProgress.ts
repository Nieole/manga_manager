import { useCallback, useState } from 'react';
import { useReadStateMutation } from '../../../reading/useReadStateMutation';

interface UseSeriesProgressParams {
  reload: () => Promise<void>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesProgress({ reload, showToast, t }: UseSeriesProgressParams) {
  const [busy, setBusy] = useState(false);
  const { markBooks } = useReadStateMutation();

  const bulkUpdate = useCallback(
    async (isRead: boolean, bookIds: number[]) => {
      if (bookIds.length === 0) return;
      setBusy(true);
      try {
        await markBooks(bookIds, isRead);
        await reload();
        showToast(t('series.toast.bulkProgressSuccess'), 'success');
      } catch (err) {
        console.error('Bulk progress update failed', err);
        await reload().catch((refreshError) => console.error('Failed to refresh partial progress', refreshError));
        showToast(t('series.toast.actionFailed'), 'error');
      } finally {
        setBusy(false);
      }
    },
    [markBooks, reload, showToast, t],
  );

  const quickToggleBook = useCallback(
    async (bookId: number, makeRead: boolean) => {
      try {
        await markBooks([bookId], makeRead);
        await reload();
        showToast(t('series.toast.statusUpdated'), 'success');
      } catch (err) {
        console.error('Quick toggle failed', err);
        await reload().catch((refreshError) => console.error('Failed to refresh partial progress', refreshError));
        showToast(t('series.toast.actionFailed'), 'error');
      }
    },
    [markBooks, reload, showToast, t],
  );

  return { busy, bulkUpdate, quickToggleBook };
}
