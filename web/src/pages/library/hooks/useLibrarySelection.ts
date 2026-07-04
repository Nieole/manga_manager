/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useCallback, useMemo, useState } from 'react';
import { apiClient } from '../../../api/client';
import type { Series } from '../types';

interface UseLibrarySelectionParams {
  allSeries: Series[];
  /** 操作完成后请求重新拉当前页 */
  onChanged: () => void;
  /** 错误时提示 */
  onError: (msg: string) => void;
}

interface UseLibrarySelectionResult {
  isSelectionMode: boolean;
  selectedSeries: number[];
  bulkProgressUpdating: 'read' | 'unread' | null;
  currentPageSelectedCount: number;
  allCurrentPageSelected: boolean;
  toggleSelectionMode: () => void;
  toggleSelectSeries: (id: number) => void;
  toggleSelectCurrentPage: () => void;
  clearSelection: () => void;
  bulkFavorite: (isFav: boolean) => Promise<void>;
  bulkProgress: (isRead: boolean) => Promise<void>;
}

/**
 * useLibrarySelection：批量选择 + 批量动作（收藏 / 已读 / 未读 / 加合集由调用方做）。
 * 加合集由 LibraryHeader 直接打开 modal，不进这里。
 */
export function useLibrarySelection({
  allSeries,
  onChanged,
  onError,
}: UseLibrarySelectionParams): UseLibrarySelectionResult {
  const [isSelectionMode, setIsSelectionMode] = useState(false);
  const [selectedSeries, setSelectedSeries] = useState<number[]>([]);
  const [bulkProgressUpdating, setBulkProgressUpdating] = useState<'read' | 'unread' | null>(null);

  const currentPageSeriesIds = useMemo(() => allSeries.map((s) => s.id), [allSeries]);
  const currentPageSelectedCount = useMemo(
    () => currentPageSeriesIds.filter((id) => selectedSeries.includes(id)).length,
    [currentPageSeriesIds, selectedSeries],
  );
  const allCurrentPageSelected =
    currentPageSeriesIds.length > 0 && currentPageSelectedCount === currentPageSeriesIds.length;

  const toggleSelectionMode = useCallback(() => {
    setIsSelectionMode((prev) => {
      if (prev) setSelectedSeries([]);
      return !prev;
    });
  }, []);

  const toggleSelectSeries = useCallback((id: number) => {
    setSelectedSeries((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]));
  }, []);

  const toggleSelectCurrentPage = useCallback(() => {
    setSelectedSeries((prev) => {
      const allSelected =
        currentPageSeriesIds.length > 0 &&
        currentPageSeriesIds.every((id) => prev.includes(id));
      if (allSelected) {
        return prev.filter((id) => !currentPageSeriesIds.includes(id));
      }
      return Array.from(new Set([...prev, ...currentPageSeriesIds]));
    });
  }, [currentPageSeriesIds]);

  const clearSelection = useCallback(() => {
    setSelectedSeries([]);
    setIsSelectionMode(false);
  }, []);

  const bulkFavorite = useCallback(
    async (isFav: boolean) => {
      if (selectedSeries.length === 0) return;
      try {
        await apiClient.post('/api/series/bulk-update', {
          series_ids: selectedSeries,
          is_favorite: isFav,
        });
        clearSelection();
        onChanged();
      } catch (err) {
        console.error('Bulk favorite failed', err);
        onError('home.bulkFavoriteFailed');
      }
    },
    [selectedSeries, clearSelection, onChanged, onError],
  );

  const bulkProgress = useCallback(
    async (isRead: boolean) => {
      if (selectedSeries.length === 0) return;
      setBulkProgressUpdating(isRead ? 'read' : 'unread');
      try {
        await apiClient.post('/api/series/bulk-progress', {
          series_ids: selectedSeries,
          is_read: isRead,
        });
        clearSelection();
        onChanged();
      } catch (err) {
        console.error('Bulk progress failed', err);
        onError('home.bulkProgressFailed');
      } finally {
        setBulkProgressUpdating(null);
      }
    },
    [selectedSeries, clearSelection, onChanged, onError],
  );

  return {
    isSelectionMode,
    selectedSeries,
    bulkProgressUpdating,
    currentPageSelectedCount,
    allCurrentPageSelected,
    toggleSelectionMode,
    toggleSelectSeries,
    toggleSelectCurrentPage,
    clearSelection,
    bulkFavorite,
    bulkProgress,
  };
}
