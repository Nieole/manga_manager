import { useCallback, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import axios from 'axios';

import type { Series } from '../types';

function getApiErrorMessage(error: unknown, fallback: string) {
  if (axios.isAxiosError(error)) return error.response?.data?.error || error.message || fallback;
  if (error instanceof Error) return error.message;
  return fallback;
}

interface UseLibraryCardActionsParams {
  isSelectionMode: boolean;
  toggleSelectSeries: (id: number) => void;
  patchSeries: (id: number, patch: Partial<Series>) => void;
  refetchCurrentPage: () => void;
  showError: (key: string) => void;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useLibraryCardActions({
  isSelectionMode,
  toggleSelectSeries,
  patchSeries,
  refetchCurrentPage,
  showError,
  showToast,
  t,
}: UseLibraryCardActionsParams) {
  const navigate = useNavigate();
  const [rescanningId, setRescanningId] = useState<number | null>(null);

  const handleCardClick = useCallback(
    (series: Series) => {
      if (isSelectionMode) {
        toggleSelectSeries(series.id);
      } else {
        navigate(`/series/${series.id}`);
      }
    },
    [isSelectionMode, navigate, toggleSelectSeries],
  );

  const handleToggleFavorite = useCallback(
    async (event: React.MouseEvent, series: Series) => {
      event.preventDefault();
      event.stopPropagation();
      try {
        await axios.post('/api/series/bulk-update', { series_ids: [series.id], is_favorite: !series.is_favorite });
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
        await axios.post(`/api/series/${series.id}/rescan?force=true`);
        showToast(t('home.seriesRescanQueued'), 'success');
        window.setTimeout(refetchCurrentPage, 3000);
      } catch (err) {
        showToast(`${t('home.seriesRescanFailed')}: ${getApiErrorMessage(err, t('home.seriesRescanFailed'))}`, 'error');
      } finally {
        setRescanningId(null);
      }
    },
    [refetchCurrentPage, showToast, t],
  );

  return {
    rescanningId,
    handleCardClick,
    handleToggleFavorite,
    handleRescanSeries,
  };
}
