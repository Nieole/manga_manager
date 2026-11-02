import { useCallback, useEffect, useRef, useState } from 'react';
import { useLibraryInvalidation } from '../../../session/ApplicationSessionProvider';
import { apiClient } from '../../../api/client';
import { getApiErrorMessage } from '../../../api/client';


interface UseSeriesMetadataReviewParams {
  seriesId: string | undefined;
  reload: (requireFresh?: boolean) => Promise<void>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesMetadataReview({ seriesId, reload, showToast, t }: UseSeriesMetadataReviewParams) {
  const invalidateLibrary = useLibraryInvalidation();
  const activeScope = useRef({ seriesId, pending: false });
  if (activeScope.current.seriesId !== seriesId) activeScope.current = { seriesId, pending: false };
  const scope = activeScope.current;
  useEffect(() => {
    activeScope.current = scope;
    return () => { if (activeScope.current === scope) activeScope.current = { seriesId: undefined, pending: false }; };
  }, [scope]);
  const [busy, setBusy] = useState<{ scope: typeof scope; reviewId: number } | null>(null);
  const busyMetadataReviewId = busy?.scope === scope ? busy.reviewId : null;

  const apply = useCallback(
    async (reviewId: number) => {
      if (!seriesId || activeScope.current !== scope || scope.pending) return;
      scope.pending = true;
      setBusy({ scope, reviewId });
      try {
        const response = await apiClient.post(`/api/metadata/reviews/${reviewId}/apply`);
        try {
          if (activeScope.current !== scope) return;
          try {
            await reload(true);
          } catch {
            if (activeScope.current === scope) showToast(t(response.data.applied ? 'series.toast.savedRefreshFailed' : 'series.content.loadFailed'), 'error');
            return;
          }
          if (activeScope.current === scope) showToast(t('series.toast.metadataReviewApplied'), 'success');
        } finally {
          // A known commit outlives this view; publish only after our refresh settles.
          if (response.data.applied) invalidateLibrary({ areas: ['catalog', 'collections', 'organize', 'stats', 'reading-lists', 'reviews'] });
        }
      } catch (err) {
        if (activeScope.current !== scope) return;
        showToast(`${t('series.toast.metadataReviewApplyFailed')}: ${getApiErrorMessage(err, t('series.toast.metadataReviewApplyFailed'))}`, 'error');
      } finally {
        scope.pending = false;
        if (activeScope.current === scope) setBusy(null);
      }
    },
    [seriesId, scope, reload, showToast, t, invalidateLibrary],
  );

  const reject = useCallback(
    async (reviewId: number) => {
      if (!seriesId || activeScope.current !== scope || scope.pending) return;
      scope.pending = true;
      setBusy({ scope, reviewId });
      try {
        await apiClient.post(`/api/metadata/reviews/${reviewId}/reject`);
        try {
          if (activeScope.current !== scope) return;
          try {
            await reload(true);
          } catch {
            if (activeScope.current === scope) showToast(t('series.toast.metadataReviewRejectedRefreshFailed'), 'error');
            return;
          }
          if (activeScope.current === scope) showToast(t('series.toast.metadataReviewRejected'), 'success');
        } finally {
          invalidateLibrary({ areas: ['reviews'] });
        }
      } catch (err) {
        if (activeScope.current !== scope) return;
        showToast(`${t('series.toast.metadataReviewRejectFailed')}: ${getApiErrorMessage(err, t('series.toast.metadataReviewRejectFailed'))}`, 'error');
      } finally {
        scope.pending = false;
        if (activeScope.current === scope) setBusy(null);
      }
    },
    [seriesId, scope, reload, showToast, t, invalidateLibrary],
  );

  return { busyMetadataReviewId, apply, reject };
}
