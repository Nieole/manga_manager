import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient, getApiErrorMessage } from '../../../api/client';
import { type AxiosResponse } from 'axios';
import {
  type Series,
  type SeriesSearchResponse,
} from '../types';
import { supportsCursorPagination, type AdvancedFilters } from './useLibraryFilters';

interface UseLibrarySeriesParams {
  libId: string | undefined;
  page: number;
  pageSize: number;
  activeTag: string | null;
  activeAuthor: string | null;
  activeStatus: string | null;
  activeLetter: string | null;
  advanced: AdvancedFilters;
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
  // error 为最近一次取列表失败的可读消息（成功后清空）；供页面渲染错误条 + 重试入口。
  error: string | null;
  pageCursorMap: Record<number, string>;
  resetPagination: () => void;
  // 数据变更后的静默重取，覆盖范围与屏幕上显示的范围一致：无限滚动是已累积的全部页，分页模式是当前页。
  refreshLoadedSeries: () => void;
  retry: () => void;
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
  advanced,
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
  const [error, setError] = useState<string | null>(null);
  const [pageCursorMap, setPageCursorMap] = useState<Record<number, string>>({});
  const lastLoadedPageRef = useRef(1);
  const latestRequestIDRef = useRef(0);

  // buildSearchQuery 是 /api/series/search 查询串的唯一拼装口：翻页与刷新必须带同一套筛选条件，
  // 各拼各的就会出现「刷新回来的那一页不属于当前筛选」。cursor 为空即走 offset 分页。
  const buildSearchQuery = useCallback(
    (pageNumber: number, cursor: string) => {
      const params = new URLSearchParams();
      params.append('libraryId', libId ?? '');
      params.append('limit', pageSize.toString());
      params.append('page', pageNumber.toString());
      if (cursor) params.append('cursor', cursor);
      if (activeTag) params.append('tags', activeTag);
      if (activeAuthor) params.append('authors', activeAuthor);
      if (activeStatus) params.append('status', activeStatus);
      if (activeLetter) params.append('letter', activeLetter);
      if (advanced.readState) params.append('readState', advanced.readState);
      if (advanced.minRating !== null) params.append('minRating', String(advanced.minRating));
      if (advanced.maxRating !== null) params.append('maxRating', String(advanced.maxRating));
      if (advanced.minProgress !== null) params.append('minProgress', String(advanced.minProgress));
      if (advanced.maxProgress !== null) params.append('maxProgress', String(advanced.maxProgress));
      if (advanced.addedWithinDays !== null) params.append('addedWithinDays', String(advanced.addedWithinDays));
      if (sortByField && sortDir) params.append('sortBy', `${sortByField}_${sortDir}`);
      if (keyword) params.append('q', keyword);
      return params.toString();
    },
    [
      libId,
      pageSize,
      activeTag,
      activeAuthor,
      activeStatus,
      activeLetter,
      advanced,
      sortByField,
      sortDir,
      keyword,
    ],
  );

  const fetchPage = useCallback(
    (pageNumber: number, silent = false) => {
      if (!libId) return;
      if (!silent) setLoading(true);
      setError(null);
      const cursor = supportsCursorPagination(sortByField) && pageNumber > 1 ? pageCursorMap[pageNumber] : '';

      const requestID = latestRequestIDRef.current + 1;
      latestRequestIDRef.current = requestID;
      requestSeriesSearch(buildSearchQuery(pageNumber, cursor || ''))
        .then((res) => {
          if (requestID !== latestRequestIDRef.current) return;
          const items = res.data.items || [];
          const total = res.data.total || 0;
          // 无限滚动往前滚（page>1）时按 id 合并：更新已存在项的最新数据、追加新增项，
          // 顺序保持不变，于是滚动加载会累积而不是整屏替换。
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
          setError(getApiErrorMessage(err, ''));
        })
        .finally(() => {
          if (requestID === latestRequestIDRef.current) setLoading(false);
        });
    },
    [libId, sortByField, pageCursorMap, appendMode, buildSearchQuery],
  );

  /**
   * refreshLoadedPages 重取第 1..lastPage 页并按 id 就地更新已累积的列表：不追加、不删除，
   * 条数与顺序不变。走 offset 分页（不带 cursor），因而既不依赖也不改写 pageCursorMap，
   * 向前滚动的游标链保持原样。整批共用一个世代号，翻页/换筛选发起的新请求会整批作废它。
   */
  const refreshLoadedPages = useCallback(
    (lastPage: number) => {
      if (!libId) return;
      setError(null);
      const requestID = latestRequestIDRef.current + 1;
      latestRequestIDRef.current = requestID;
      const pages = Array.from({ length: lastPage }, (_, index) => index + 1);
      Promise.all(pages.map((pageNumber) => requestSeriesSearch(buildSearchQuery(pageNumber, ''))))
        .then((responses) => {
          if (requestID !== latestRequestIDRef.current) return;
          const incoming = new Map<number, Series>();
          for (const res of responses) {
            for (const item of res.data.items || []) incoming.set(item.id, item);
          }
          setAllSeries((prev) => prev.map((s) => incoming.get(s.id) ?? s));
          // 第 1 页是 offset 请求，带真实 total。
          setTotalSeries(responses[0]?.data.total || 0);
        })
        .catch((err) => {
          if (requestID !== latestRequestIDRef.current) return;
          console.error('Failed to refresh loaded series pages', err);
          setError(getApiErrorMessage(err, ''));
        });
    },
    [libId, buildSearchQuery],
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
    advanced,
    sortByField,
    sortDir,
    refreshTrigger,
    keyword,
  ]);

  const resetPagination = useCallback(() => {
    setPageCursorMap({});
    lastLoadedPageRef.current = 1;
  }, []);

  // 无限滚动下屏幕上是第 1..page 页的累积结果，只重取当前页会让前几屏停在旧数据上；
  // 分页模式下屏幕上就只有当前页，重取当前页即可（且必须整页替换，才能反映新增与删除）。
  const refreshLoadedSeries = useCallback(() => {
    if (!libId) return;
    if (appendMode && page > 1) refreshLoadedPages(page);
    else fetchPage(page, true);
  }, [libId, appendMode, page, refreshLoadedPages, fetchPage]);

  // retry：错误条的重试入口，非静默重取当前页（显示 loading 并清除错误）。
  const retry = useCallback(() => {
    if (libId) fetchPage(page);
  }, [libId, page, fetchPage]);

  const patchSeries = useCallback((id: number, partial: Partial<Series>) => {
    setAllSeries((prev) => prev.map((s) => (s.id === id ? { ...s, ...partial } : s)));
  }, []);

  return {
    allSeries,
    totalSeries,
    loading,
    error,
    pageCursorMap,
    resetPagination,
    refreshLoadedSeries,
    retry,
    patchSeries,
  };
}
