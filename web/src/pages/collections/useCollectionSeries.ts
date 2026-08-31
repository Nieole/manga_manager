/**
 * 本文件是合集页右栏成员的取数逻辑：智能书架按 offset 逐页追加，手工合集一次取完。
 * 与资料库列表同构——每次请求带世代号，只有最新一次的响应能落到界面上。
 * 维护时应保证 total 始终是命中总数而非当前已加载条数，右栏计数才能与左栏对上。
 */

import { useCallback, useRef, useState } from 'react';
import { apiClient } from '../../api/client';
import type {
  Collection,
  CollectionSeriesItem,
  SmartCollectionSeriesItem,
  SmartCollectionSeriesResponse,
  SmartFilter,
} from './types';

// 智能书架没回显每页条数时的兜底，与后端 defaultSmartCollectionPageLimit 一致。
const FALLBACK_PAGE_SIZE = 30;

function toSeriesItem(item: SmartCollectionSeriesItem): CollectionSeriesItem {
  return {
    series_id: item.id,
    series_name: item.title?.Valid ? item.title.String : item.name,
    cover_path: item.cover_path || { String: '', Valid: false },
    book_count: item.actual_book_count ?? item.book_count ?? 0,
  };
}

// 追加页按 series_id 合并：翻页期间成员集合可能已变，重复项只保留一份。
function mergeByID(prev: CollectionSeriesItem[], page: CollectionSeriesItem[]): CollectionSeriesItem[] {
  if (prev.length === 0) return page;
  const seen = new Set(prev.map((item) => item.series_id));
  return prev.concat(page.filter((item) => !seen.has(item.series_id)));
}

export interface UseCollectionSeriesResult {
  items: CollectionSeriesItem[];
  // 命中总数：智能书架取响应里的 total，手工合集就是取回的条数。
  total: number;
  hasMore: boolean;
  loading: boolean;
  open: (c: Collection) => void;
  loadMore: () => void;
  clear: () => void;
  removeSeries: (seriesID: number) => void;
}

/**
 * useCollectionSeries 供合集页右栏使用。
 * onSmartFilterLoaded 把响应里的筛选定义回灌给选中项，让右栏读到的定义与左栏列表保持一致。
 */
export function useCollectionSeries(
  onSmartFilterLoaded: (viewID: string, filter: SmartFilter) => void,
): UseCollectionSeriesResult {
  const [items, setItems] = useState<CollectionSeriesItem[]>([]);
  const [total, setTotal] = useState(0);
  const [paged, setPaged] = useState(false);
  const [loading, setLoading] = useState(false);
  const itemsRef = useRef<CollectionSeriesItem[]>([]);
  const requestIDRef = useRef(0);
  // 下一页的起点跟着响应走，而不是跟着渲染闭包，连点两次也不会重取同一页。
  const cursorRef = useRef<{ collection: Collection; nextOffset: number; pageSize: number } | null>(null);

  const applyItems = useCallback((next: CollectionSeriesItem[]) => {
    itemsRef.current = next;
    setItems(next);
  }, []);

  const requestSmartPage = useCallback((c: Collection, offset: number, pageSize: number) => {
    const requestID = requestIDRef.current + 1;
    requestIDRef.current = requestID;
    setLoading(true);
    // 第一页不指定 limit，交给后端用这个书架配置的每页大小；往后翻沿用它回显的值。
    const config = offset > 0 ? { params: { limit: pageSize, offset } } : undefined;
    apiClient
      .get<SmartCollectionSeriesResponse>(`/api/collection-views/smart/${c.numeric_id}/series`, config)
      .then((res) => {
        // 世代号对不上就整份丢弃：慢网下先发的响应后到，不能盖掉用户已经切过去的合集。
        if (requestID !== requestIDRef.current) return;
        const payload = res.data;
        const page = (payload.items || []).map(toSeriesItem);
        const merged = offset > 0 ? mergeByID(itemsRef.current, page) : page;
        applyItems(merged);
        // 追加请求一条新成员都没带回来时，把总数收敛到实际条数，免得「加载更多」永远消不掉。
        setTotal(offset > 0 && page.length === 0 ? merged.length : payload.total ?? merged.length);
        const effectivePageSize = payload.limit && payload.limit > 0 ? payload.limit : pageSize;
        cursorRef.current = { collection: c, nextOffset: offset + page.length, pageSize: effectivePageSize };
        if (payload.filter) onSmartFilterLoaded(c.view_id, payload.filter);
      })
      .catch(() => {
        if (requestID === requestIDRef.current) cursorRef.current = { collection: c, nextOffset: offset, pageSize };
      })
      .finally(() => {
        if (requestID === requestIDRef.current) setLoading(false);
      });
  }, [applyItems, onSmartFilterLoaded]);

  const requestManualSeries = useCallback((c: Collection) => {
    const requestID = requestIDRef.current + 1;
    requestIDRef.current = requestID;
    setLoading(true);
    apiClient
      .get<CollectionSeriesItem[]>(`/api/collections/${c.numeric_id}/series`)
      .then((res) => {
        if (requestID !== requestIDRef.current) return;
        const rows = res.data || [];
        applyItems(rows);
        setTotal(rows.length);
      })
      .catch(() => undefined)
      .finally(() => {
        if (requestID === requestIDRef.current) setLoading(false);
      });
  }, [applyItems]);

  const open = useCallback((c: Collection) => {
    const smart = c.kind === 'smart';
    applyItems([]);
    // 先用左栏那个准确的计数占位，第一页回来前右栏也不会报错数。
    setTotal(c.series_count ?? 0);
    setPaged(smart);
    cursorRef.current = null;
    if (smart) requestSmartPage(c, 0, FALLBACK_PAGE_SIZE);
    else requestManualSeries(c);
  }, [applyItems, requestSmartPage, requestManualSeries]);

  const loadMore = useCallback(() => {
    const cursor = cursorRef.current;
    if (!cursor || cursor.nextOffset <= 0) return;
    requestSmartPage(cursor.collection, cursor.nextOffset, cursor.pageSize);
  }, [requestSmartPage]);

  const clear = useCallback(() => {
    requestIDRef.current += 1;
    cursorRef.current = null;
    applyItems([]);
    setTotal(0);
    setPaged(false);
    setLoading(false);
  }, [applyItems]);

  const removeSeries = useCallback((seriesID: number) => {
    const next = itemsRef.current.filter((item) => item.series_id !== seriesID);
    if (next.length === itemsRef.current.length) return;
    applyItems(next);
    setTotal((prev) => Math.max(0, prev - 1));
  }, [applyItems]);

  return {
    items,
    total,
    // 取数期间不给「加载更多」，既避免与首页请求打架，也免得重复取同一页。
    hasMore: paged && !loading && items.length < total,
    loading,
    open,
    loadMore,
    clear,
    removeSeries,
  };
}
