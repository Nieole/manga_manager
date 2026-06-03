import { useCallback, useEffect, useRef, useState } from 'react';
import axios, { type AxiosResponse } from 'axios';
import { recordSeriesListRenderMetric } from '../../../utils/frontendPerformance';
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
  serializedFilters: string;
  refreshTrigger: number;
  enabled: boolean;
  keyword?: string;
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
  const request = axios
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
  serializedFilters,
  refreshTrigger,
  enabled,
  keyword = '',
}: UseLibrarySeriesParams): UseLibrarySeriesResult {
  const [allSeries, setAllSeries] = useState<Series[]>([]);
  const [totalSeries, setTotalSeries] = useState(0);
  const [loading, setLoading] = useState(false);
  const [pageCursorMap, setPageCursorMap] = useState<Record<number, string>>({});
  const lastLoadedPageRef = useRef(1);

  const pendingRenderMetric = useRef<{
    requestStartedAt: number;
    responseReceivedAt: number;
    libraryId: string;
    pageNumber: number;
    currentPageSize: number;
    currentSortBy: string;
    currentSortDir: string;
    filters: string;
    itemCount: number;
    totalCount: number;
  } | null>(null);

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

      const requestStartedAt = performance.now();
      requestSeriesSearch(params.toString())
        .then((res) => {
          const items = res.data.items || [];
          const total = res.data.total || 0;
          setAllSeries(items);
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

          pendingRenderMetric.current = {
            requestStartedAt,
            responseReceivedAt: performance.now(),
            libraryId: libId,
            pageNumber,
            currentPageSize: pageSize,
            currentSortBy: sortByField,
            currentSortDir: sortDir,
            filters: serializedFilters,
            itemCount: items.length,
            totalCount: total,
          };
        })
        .catch((err) => {
          console.error('Failed to fetch series page', err);
        })
        .finally(() => {
          if (!silent) setLoading(false);
        });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [
      libId,
      pageSize,
      activeTag,
      activeAuthor,
      activeStatus,
      activeLetter,
      sortByField,
      sortDir,
      serializedFilters,
      keyword,
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

  // 渲染计时埋点：在数据落到 DOM 之后报告
  useEffect(() => {
    if (!pendingRenderMetric.current) return;
    const metric = pendingRenderMetric.current;
    pendingRenderMetric.current = null;
    const finalize = () => {
      const renderedAt = performance.now();
      recordSeriesListRenderMetric({
        path: typeof window !== 'undefined' ? window.location.pathname : '',
        library_id: metric.libraryId,
        page: metric.pageNumber,
        page_size: metric.currentPageSize,
        sort: `${metric.currentSortBy}_${metric.currentSortDir}`,
        filters: metric.filters || 'none',
        item_count: metric.itemCount,
        total_count: metric.totalCount,
        request_ms: Math.max(0, Math.round(metric.responseReceivedAt - metric.requestStartedAt)),
        render_ms: Math.max(0, Math.round(renderedAt - metric.responseReceivedAt)),
        total_ms: Math.max(0, Math.round(renderedAt - metric.requestStartedAt)),
        measured_at: new Date().toISOString(),
      });
    };
    if (typeof window === 'undefined' || typeof requestAnimationFrame !== 'function') {
      finalize();
      return;
    }
    const frame = requestAnimationFrame(finalize);
    return () => cancelAnimationFrame(frame);
  }, [allSeries]);

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
