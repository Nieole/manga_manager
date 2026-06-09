/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

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
