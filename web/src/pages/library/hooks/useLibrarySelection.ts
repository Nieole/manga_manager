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
  /** 有序数组，供批量 API 请求体与计数展示。 */
  selectedSeries: number[];
  /** O(1) 成员判定，供 LibraryGrid 逐卡判断是否选中（取代 number[].includes 的 O(n²) 渲染）。 */
  selectedSet: Set<number>;
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
  // 内部以 Set 承载选择态：成员判定 O(1)，大选择集下逐卡判断不再是 O(n²)。selectedSeries 数组按需派生。
  const [selectedSet, setSelectedSet] = useState<Set<number>>(() => new Set());
  const [bulkProgressUpdating, setBulkProgressUpdating] = useState<'read' | 'unread' | null>(null);

  const selectedSeries = useMemo(() => Array.from(selectedSet), [selectedSet]);
  const currentPageSeriesIds = useMemo(() => allSeries.map((s) => s.id), [allSeries]);
  const currentPageSelectedCount = useMemo(
    () => currentPageSeriesIds.reduce((n, id) => (selectedSet.has(id) ? n + 1 : n), 0),
    [currentPageSeriesIds, selectedSet],
  );
  const allCurrentPageSelected =
    currentPageSeriesIds.length > 0 && currentPageSelectedCount === currentPageSeriesIds.length;

  const toggleSelectionMode = useCallback(() => {
    setIsSelectionMode((prev) => {
      if (prev) setSelectedSet(new Set());
      return !prev;
    });
  }, []);

  const toggleSelectSeries = useCallback((id: number) => {
    setSelectedSet((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const toggleSelectCurrentPage = useCallback(() => {
    setSelectedSet((prev) => {
      const allSelected =
        currentPageSeriesIds.length > 0 && currentPageSeriesIds.every((id) => prev.has(id));
      const next = new Set(prev);
      if (allSelected) {
        currentPageSeriesIds.forEach((id) => next.delete(id));
      } else {
        currentPageSeriesIds.forEach((id) => next.add(id));
      }
      return next;
    });
  }, [currentPageSeriesIds]);

  const clearSelection = useCallback(() => {
    setSelectedSet(new Set());
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
    selectedSet,
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
