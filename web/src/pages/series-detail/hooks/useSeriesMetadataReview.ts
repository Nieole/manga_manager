import { useCallback, useState } from 'react';
import { useLibraryInvalidation } from '../../../session/ApplicationSessionProvider';
import { apiClient } from '../../../api/client';
import { getApiErrorMessage } from '../../../api/client';


interface UseSeriesMetadataReviewParams {
  reload: () => Promise<void>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesMetadataReview({ reload, showToast, t }: UseSeriesMetadataReviewParams) {
  const invalidateLibrary = useLibraryInvalidation();
  const [busyMetadataReviewId, setBusyMetadataReviewId] = useState<number | null>(null);

  const apply = useCallback(
    async (reviewId: number) => {
      setBusyMetadataReviewId(reviewId);
      try {
        const response = await apiClient.post(`/api/metadata/reviews/${reviewId}/apply`);
        if (response.data.applied) invalidateLibrary({ areas: ['catalog', 'collections', 'organize', 'stats', 'reading-lists', 'reviews'] });
        await reload();
        showToast(t('series.toast.metadataReviewApplied'), 'success');
      } catch (err) {
        showToast(`${t('series.toast.metadataReviewApplyFailed')}: ${getApiErrorMessage(err, t('series.toast.metadataReviewApplyFailed'))}`, 'error');
      } finally {
        setBusyMetadataReviewId(null);
      }
    },
    [reload, showToast, t, invalidateLibrary],
  );

  const reject = useCallback(
    async (reviewId: number) => {
      setBusyMetadataReviewId(reviewId);
      try {
        await apiClient.post(`/api/metadata/reviews/${reviewId}/reject`);
        invalidateLibrary({ areas: ['reviews'] });
        await reload();
        showToast(t('series.toast.metadataReviewRejected'), 'success');
      } catch (err) {
        showToast(`${t('series.toast.metadataReviewRejectFailed')}: ${getApiErrorMessage(err, t('series.toast.metadataReviewRejectFailed'))}`, 'error');
      } finally {
        setBusyMetadataReviewId(null);
      }
    },
    [reload, showToast, t, invalidateLibrary],
  );

  return { busyMetadataReviewId, apply, reject };
}
