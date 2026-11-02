import { useCallback, useEffect, useRef, useState } from 'react';
import { useReadStateMutation } from '../../../reading/useReadStateMutation';

interface UseSeriesProgressParams {
  seriesId: string | undefined;
  reload: (requireFresh?: boolean) => Promise<void>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesProgress({ seriesId, reload, showToast, t }: UseSeriesProgressParams) {
  const active = useRef({ seriesId });
  if (active.current.seriesId !== seriesId) active.current = { seriesId };
  const scope = active.current;
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    active.current = scope;
    setBusy(false);
    return () => { if (active.current === scope) active.current = { seriesId: undefined }; };
  }, [scope]);
  const { markBooks } = useReadStateMutation();

  const update = useCallback(async (isRead: boolean, bookIds: number[], success: string) => {
    if (!bookIds.length) return;
    setBusy(true);
    try {
      let refreshFailed = false;
      const outcome = await markBooks(bookIds, isRead, async () => {
        if (active.current !== scope) return;
        try { await reload(true); } catch { refreshFailed = true; }
      });
      if (active.current !== scope) return;
      if (outcome.incomplete) showToast(`${t(success)} / ${t('series.toast.actionFailed')}`, 'error');
      else if (refreshFailed || outcome.warnings) showToast(`${t(success)} / ${t('series.content.loadFailed')}`, 'error');
      else showToast(t(success), 'success');
    } catch (err) {
      console.error('Progress update failed', err);
      if (active.current === scope) showToast(t('series.toast.actionFailed'), 'error');
    } finally {
      if (active.current === scope) setBusy(false);
    }
  }, [markBooks, reload, scope, showToast, t]);

  return {
    busy,
    bulkUpdate: useCallback((isRead: boolean, ids: number[]) => update(isRead, ids, 'series.toast.bulkProgressSuccess'), [update]),
    quickToggleBook: useCallback((id: number, isRead: boolean) => update(isRead, [id], 'series.toast.statusUpdated'), [update]),
  };
}
