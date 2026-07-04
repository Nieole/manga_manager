/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useCallback, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiClient } from '../../../api/client';
import { getApiErrorMessage } from '../../../api/client';

import type { Series } from '../types';


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
