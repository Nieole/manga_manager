import { useCallback, useState } from 'react';
import axios from 'axios';

function getApiErrorMessage(error: unknown, fallback: string) {
  if (axios.isAxiosError(error)) return error.response?.data?.error || error.message || fallback;
  if (error instanceof Error) return error.message;
  return fallback;
}

interface UseSeriesMetadataReviewParams {
  reload: () => Promise<void>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesMetadataReview({ reload, showToast, t }: UseSeriesMetadataReviewParams) {
  const [busyMetadataReviewId, setBusyMetadataReviewId] = useState<number | null>(null);

  const apply = useCallback(
    async (reviewId: number) => {
      setBusyMetadataReviewId(reviewId);
      try {
        await axios.post(`/api/metadata/reviews/${reviewId}/apply`);
        await reload();
        showToast(t('series.toast.metadataReviewApplied'), 'success');
      } catch (err) {
        showToast(`${t('series.toast.metadataReviewApplyFailed')}: ${getApiErrorMessage(err, t('series.toast.metadataReviewApplyFailed'))}`, 'error');
      } finally {
        setBusyMetadataReviewId(null);
      }
    },
    [reload, showToast, t],
  );

  const reject = useCallback(
    async (reviewId: number) => {
      setBusyMetadataReviewId(reviewId);
      try {
        await axios.post(`/api/metadata/reviews/${reviewId}/reject`);
        await reload();
        showToast(t('series.toast.metadataReviewRejected'), 'success');
      } catch (err) {
        showToast(`${t('series.toast.metadataReviewRejectFailed')}: ${getApiErrorMessage(err, t('series.toast.metadataReviewRejectFailed'))}`, 'error');
      } finally {
        setBusyMetadataReviewId(null);
      }
    },
    [reload, showToast, t],
  );

  return { busyMetadataReviewId, apply, reject };
}
