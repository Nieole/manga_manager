import { useCallback, useState } from 'react';
import axios from 'axios';

interface UseSeriesProgressParams {
  reload: () => Promise<void>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesProgress({ reload, showToast, t }: UseSeriesProgressParams) {
  const [busy, setBusy] = useState(false);

  const bulkUpdate = useCallback(
    async (isRead: boolean, bookIds: number[]) => {
      if (bookIds.length === 0) return;
      setBusy(true);
      try {
        await axios.post('/api/books/bulk-progress', {
          book_ids: bookIds,
          is_read: isRead,
        });
        await reload();
        showToast(t('series.toast.bulkProgressSuccess'), 'success');
      } catch (err) {
        console.error('Bulk progress update failed', err);
        showToast(t('series.toast.actionFailed'), 'error');
      } finally {
        setBusy(false);
      }
    },
    [reload, showToast, t],
  );

  const quickToggleBook = useCallback(
    async (bookId: number, makeRead: boolean) => {
      try {
        await axios.post('/api/books/bulk-progress', {
          book_ids: [bookId],
          is_read: makeRead,
        });
        await reload();
        showToast(t('series.toast.statusUpdated'), 'success');
      } catch (err) {
        console.error('Quick toggle failed', err);
        showToast(t('series.toast.actionFailed'), 'error');
      }
    },
    [reload, showToast, t],
  );

  return { busy, bulkUpdate, quickToggleBook };
}
