import { useCallback, useState } from 'react';
import { apiClient } from '../../../api/client';
import { getApiErrorMessage } from '../../../api/client';

import type { Series } from '../types';


interface UseLibraryCardActionsParams {
  toggleSelectSeries: (id: number) => void;
  patchSeries: (id: number, patch: Partial<Series>) => void;
  refreshLoadedSeries: () => void;
  showError: (key: string) => void;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useLibraryCardActions({
  toggleSelectSeries,
  patchSeries,
  refreshLoadedSeries,
  showError,
  showToast,
  t,
}: UseLibraryCardActionsParams) {
  const [rescanningId, setRescanningId] = useState<number | null>(null);

  // 只管多选：进详情由卡片自身的 <Link> 导航，这里再 navigate 一次会把历史栈压成两条。
  const handleCardClick = useCallback(
    (series: Series) => {
      toggleSelectSeries(series.id);
    },
    [toggleSelectSeries],
  );

  const handleToggleFavorite = useCallback(
    async (event: React.MouseEvent, series: Series) => {
      event.preventDefault();
      event.stopPropagation();
      try {
        await apiClient.post('/api/series/bulk-update', { series_ids: [series.id], is_favorite: !series.is_favorite });
        patchSeries(series.id, { is_favorite: !series.is_favorite });
      } catch (err) {
        console.error('Toggle favorite failed', err);
        showError('home.bulkFavoriteFailed');
      }
    },
    [patchSeries, showError],
  );

  const handleRescanSeries = useCallback(
    async (event: React.MouseEvent, series: Series) => {
      event.preventDefault();
      event.stopPropagation();
      setRescanningId(series.id);
      try {
        await apiClient.post(`/api/series/${series.id}/rescan?force=true`);
        showToast(t('home.seriesRescanQueued'), 'success');
        window.setTimeout(refreshLoadedSeries, 3000);
      } catch (err) {
        showToast(`${t('home.seriesRescanFailed')}: ${getApiErrorMessage(err, t('home.seriesRescanFailed'))}`, 'error');
      } finally {
        setRescanningId(null);
      }
    },
    [refreshLoadedSeries, showToast, t],
  );

  return {
    rescanningId,
    handleCardClick,
    handleToggleFavorite,
    handleRescanSeries,
  };
}
