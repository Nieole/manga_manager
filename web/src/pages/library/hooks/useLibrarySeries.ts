/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient } from '../../../api/client';
import { type AxiosResponse } from 'axios';
import {
  type Series,
  type SeriesSearchResponse,
} from '../types';
import { supportsCursorPagination } from './useLibraryFilters';

interface UseLibrarySeriesParams {
  libId: string | undefined;
  page: number;
  pageSize: number;
  activeTag: string | null;
  activeAuthor: string | null;
  activeStatus: string | null;
  activeLetter: string | null;
  sortByField: string;
  sortDir: string;
  refreshTrigger: number;
  enabled: boolean;
  keyword?: string;
  // 无限滚动模式：翻到第 2 页及以后时把新数据追加到已有列表（按 id 去重），
  // 而非整页替换。分页模式（false）保持“每页替换”语义。
  appendMode?: boolean;
}

interface UseLibrarySeriesResult {
  allSeries: Series[];
  totalSeries: number;
  loading: boolean;
  pageCursorMap: Record<number, string>;
  resetPagination: () => void;
  refetchCurrentPage: () => void;
  patchSeries: (id: number, partial: Partial<Series>) => void;
}

const inflightSeriesSearchRequests = new Map<string, Promise<AxiosResponse<SeriesSearchResponse>>>();

function requestSeriesSearch(query: string) {
  const existing = inflightSeriesSearchRequests.get(query);
  if (existing) return existing;
  const request = apiClient
    .get<SeriesSearchResponse>(`/api/series/search?${query}`)
    .finally(() => {
      window.setTimeout(() => {
        inflightSeriesSearchRequests.delete(query);
      }, 100);
    });
  inflightSeriesSearchRequests.set(query, request);
  return request;
}

/**
 * useLibrarySeries：负责 /api/series/search 的 paged + cursor 调用、
 * 加载/总数状态、分页缓存。供 LibraryGrid / LibraryPagination 共用。
 */
export function useLibrarySeries({
  libId,
  page,
  pageSize,
  activeTag,
  activeAuthor,
  activeStatus,
  activeLetter,
  sortByField,
  sortDir,
  refreshTrigger,
  enabled,
  keyword = '',
  appendMode = false,
}: UseLibrarySeriesParams): UseLibrarySeriesResult {
  const [allSeries, setAllSeries] = useState<Series[]>([]);
  const [totalSeries, setTotalSeries] = useState(0);
  const [loading, setLoading] = useState(false);
  const [pageCursorMap, setPageCursorMap] = useState<Record<number, string>>({});
  const lastLoadedPageRef = useRef(1);
  const latestRequestIDRef = useRef(0);

  const fetchPage = useCallback(
    (pageNumber: number, silent = false) => {
      if (!libId) return;
      if (!silent) setLoading(true);
      const params = new URLSearchParams();
      params.append('libraryId', libId);
      params.append('limit', pageSize.toString());
      params.append('page', pageNumber.toString());
      const cursor = supportsCursorPagination(sortByField) && pageNumber > 1 ? pageCursorMap[pageNumber] : '';
      if (cursor) params.append('cursor', cursor);
      if (activeTag) params.append('tags', activeTag);
      if (activeAuthor) params.append('authors', activeAuthor);
      if (activeStatus) params.append('status', activeStatus);
      if (activeLetter) params.append('letter', activeLetter);
      if (sortByField && sortDir) params.append('sortBy', `${sortByField}_${sortDir}`);
      if (keyword) params.append('q', keyword);

      const requestID = latestRequestIDRef.current + 1;
      latestRequestIDRef.current = requestID;
      requestSeriesSearch(params.toString())
        .then((res) => {
          if (requestID !== latestRequestIDRef.current) return;
          const items = res.data.items || [];
          const total = res.data.total || 0;
          // 无限滚动翻页（page>1）时按 id 合并：更新已存在项的最新数据、追加新增项，
          // 顺序保持不变。这样滚动加载会累积，而收藏/刮削后对当前页的静默刷新也能生效。
          // 筛选/排序变化会把 page 重置为 1，届时走替换分支，自然清空累积列表。
          if (appendMode && pageNumber > 1) {
            setAllSeries((prev) => {
              if (prev.length === 0) return items;
              const incoming = new Map(items.map((item) => [item.id, item]));
              const merged = prev.map((s) => incoming.get(s.id) ?? s);
              const seen = new Set(prev.map((s) => s.id));
              for (const item of items) {
                if (!seen.has(item.id)) merged.push(item);
              }
              return merged;
            });
          } else {
            setAllSeries(items);
          }
          // 游标分页（cursor 翻页）的后端响应不做 COUNT，total 恒为 0；此时不能用它覆盖
          // 第 1 页已取得的真实总数，否则 totalSeries 归零会让分页控件（totalSeries > 0）消失。
          // 仅在非游标请求（带真实 total）时才更新总数。
          if (cursor) {
            if (total > 0) setTotalSeries(total);
          } else {
            setTotalSeries(total);
          }
          if (res.data.next_cursor && supportsCursorPagination(sortByField)) {
            setPageCursorMap((prev) => ({ ...prev, [pageNumber + 1]: res.data.next_cursor as string }));
          }
          lastLoadedPageRef.current = pageNumber;
        })
        .catch((err) => {
          if (requestID !== latestRequestIDRef.current) return;
          console.error('Failed to fetch series page', err);
        })
        .finally(() => {
          if (requestID === latestRequestIDRef.current) setLoading(false);
        });
    },
    [
      libId,
      pageSize,
      activeTag,
      activeAuthor,
      activeStatus,
      activeLetter,
      sortByField,
      sortDir,
      keyword,
      pageCursorMap,
      appendMode,
    ],
  );

  // refresh on refreshTrigger / filter / page changes
  useEffect(() => {
    if (!enabled || !libId) return;
    fetchPage(page);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    enabled,
    libId,
    page,
    pageSize,
    activeTag,
    activeAuthor,
    activeStatus,
    activeLetter,
    sortByField,
    sortDir,
    refreshTrigger,
    keyword,
  ]);

  const resetPagination = useCallback(() => {
    setPageCursorMap({});
    lastLoadedPageRef.current = 1;
  }, []);

  const refetchCurrentPage = useCallback(() => {
    if (libId) fetchPage(page, true);
  }, [libId, page, fetchPage]);

  const patchSeries = useCallback((id: number, partial: Partial<Series>) => {
    setAllSeries((prev) => prev.map((s) => (s.id === id ? { ...s, ...partial } : s)));
  }, []);

  return {
    allSeries,
    totalSeries,
    loading,
    pageCursorMap,
    resetPagination,
    refetchCurrentPage,
    patchSeries,
  };
}
