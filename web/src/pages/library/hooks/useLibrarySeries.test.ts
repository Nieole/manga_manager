/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「刷新覆盖的范围要和屏幕上真正显示的范围一致」。无限滚动把多屏累积在一个列表里，
 * 批量操作只改数据不改页码；刷新若只覆盖当前那一屏，用户往回看到的前几屏会停在旧进度上，
 * 看起来像操作失败，于是重复点。分页模式相反：一次只显示一页，刷新必须只重取当前页。
 */

import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { apiClient } from '../../../api/client';
import type { Series } from '../types';
import { EMPTY_ADVANCED_FILTERS } from './libraryFilterParams';
import { useLibrarySeries } from './useLibrarySeries';

const PAGE_SIZE = 3;
const BOOKS_PER_SERIES = 5;

// 假后端持有的整库数据：批量操作直接改这里，模拟「服务端已经写好、只等前端来取」。
let serverSeries: Series[] = [];
// 假后端每页给哪几条；默认按 limit 等分，去重用例改成相邻两屏重叠。
let pageItemsFor: (page: number, limit: number) => Series[];
const requests: string[] = [];

function seriesOf(id: number, readCount = 0): Series {
  return {
    id,
    name: `系列${id}`,
    volume_count: BOOKS_PER_SERIES,
    actual_book_count: BOOKS_PER_SERIES,
    read_count: readCount,
    total_pages: { Float64: BOOKS_PER_SERIES, Valid: true },
    is_favorite: false,
  };
}

/** 服务端侧的批量标记已读：整本读完即 read_count 追平 actual_book_count。 */
function markReadOnServer(ids: number[]) {
  serverSeries = serverSeries.map((s) => (ids.includes(s.id) ? { ...s, read_count: BOOKS_PER_SERIES } : s));
}

function readCountOf(list: Series[], id: number) {
  return list.find((s) => s.id === id)?.read_count;
}

const idsOf = (list: Series[]) => list.map((s) => s.id);
const pagesRequested = () => requests.map((q) => new URLSearchParams(q).get('page'));

/**
 * 复刻 /api/series/search 的分页形状：offset 请求带真实 total，游标请求 total 恒为 0
 * （后端不做 COUNT）；游标 cur-N 表示「第 N 页的起点」，与真实游标链一致。
 */
function mockSeriesSearch() {
  vi.spyOn(apiClient, 'get').mockImplementation((async (url: string) => {
    const query = url.replace('/api/series/search?', '');
    requests.push(query);
    const params = new URLSearchParams(query);
    const limit = Number(params.get('limit') || PAGE_SIZE);
    const cursor = params.get('cursor');
    const page = cursor ? Number(cursor.replace('cur-', '')) : Number(params.get('page') || '1');
    return {
      data: {
        items: pageItemsFor(page, limit).map((s) => ({ ...s })),
        total: cursor ? 0 : serverSeries.length,
        next_cursor: `cur-${page + 1}`,
      },
    };
  }) as never);
}

interface HarnessProps {
  libId: string;
  page: number;
  appendMode: boolean;
}

function renderSeries(props: HarnessProps) {
  return renderHook(
    ({ libId, page, appendMode }: HarnessProps) =>
      useLibrarySeries({
        libId,
        page,
        pageSize: PAGE_SIZE,
        activeTag: null,
        activeAuthor: null,
        activeStatus: null,
        activeLetter: null,
        advanced: EMPTY_ADVANCED_FILTERS,
        // name 支持游标分页，无限滚动的翻页会带上 cursor——正是前几屏难以重取的那条链路。
        sortByField: 'name',
        sortDir: 'asc',
        refreshTrigger: 0,
        enabled: true,
        appendMode,
      }),
    { initialProps: props },
  );
}

// 推进请求 promise 与在途去重表的 100ms 清理窗口，直到列表静止。
async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(200);
  });
}

// vitest 没开 globals，testing-library 的自动清理不会生效。
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

beforeEach(() => {
  vi.useFakeTimers();
  requests.length = 0;
  serverSeries = Array.from({ length: 9 }, (_, i) => seriesOf(i + 1));
  pageItemsFor = (page, limit) => serverSeries.slice((page - 1) * limit, page * limit);
  mockSeriesSearch();
});

describe('资料库列表刷新范围', () => {
  it('无限滚动滚过 3 屏后批量标记已读，第 1 屏的卡片也跟着更新', async () => {
    const view = renderSeries({ libId: '8001', page: 1, appendMode: true });
    await settle();
    view.rerender({ libId: '8001', page: 2, appendMode: true });
    await settle();
    view.rerender({ libId: '8001', page: 3, appendMode: true });
    await settle();
    expect(idsOf(view.result.current.allSeries)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9]);

    // 勾选第 1 屏的系列 1、2 与第 3 屏的系列 9，服务端已改完，前端来刷新。
    markReadOnServer([1, 2, 9]);
    act(() => view.result.current.refreshLoadedSeries());
    await settle();

    expect(readCountOf(view.result.current.allSeries, 1)).toBe(BOOKS_PER_SERIES);
    expect(readCountOf(view.result.current.allSeries, 2)).toBe(BOOKS_PER_SERIES);
    expect(readCountOf(view.result.current.allSeries, 9)).toBe(BOOKS_PER_SERIES);
    // 刷新不改变已累积的条数与顺序。
    expect(idsOf(view.result.current.allSeries)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9]);
  });

  it('无限滚动的刷新不动向前滚动的游标链', async () => {
    const view = renderSeries({ libId: '8003', page: 1, appendMode: true });
    await settle();
    view.rerender({ libId: '8003', page: 2, appendMode: true });
    await settle();
    view.rerender({ libId: '8003', page: 3, appendMode: true });
    await settle();

    act(() => view.result.current.refreshLoadedSeries());
    await settle();

    expect(view.result.current.pageCursorMap[4]).toBe('cur-4');
    requests.length = 0;
    view.rerender({ libId: '8003', page: 4, appendMode: true });
    await settle();
    expect(requests.some((q) => q.includes('cursor=cur-4'))).toBe(true);
  });

  it('分页模式的刷新仍然只重取当前页', async () => {
    const view = renderSeries({ libId: '8002', page: 3, appendMode: false });
    await settle();
    expect(idsOf(view.result.current.allSeries)).toEqual([7, 8, 9]);

    requests.length = 0;
    markReadOnServer([7]);
    act(() => view.result.current.refreshLoadedSeries());
    await settle();

    expect(pagesRequested()).toEqual(['3']);
    // 分页模式一次只显示一页：刷新后仍然只有当前页，且吃到最新数据。
    expect(idsOf(view.result.current.allSeries)).toEqual([7, 8, 9]);
    expect(readCountOf(view.result.current.allSeries, 7)).toBe(BOOKS_PER_SERIES);
  });

  it('无限滚动翻页时按 id 去重累积，已存在项吃到最新数据', async () => {
    // 相邻两屏重叠一条（翻页期间有新数据插入就会这样）：累积结果不得出现重复。
    pageItemsFor = (page, limit) => serverSeries.slice((page - 1) * (limit - 1), (page - 1) * (limit - 1) + limit);

    const view = renderSeries({ libId: '8004', page: 1, appendMode: true });
    await settle();
    expect(idsOf(view.result.current.allSeries)).toEqual([1, 2, 3]);

    markReadOnServer([3]);
    view.rerender({ libId: '8004', page: 2, appendMode: true });
    await settle();

    expect(idsOf(view.result.current.allSeries)).toEqual([1, 2, 3, 4, 5]);
    expect(readCountOf(view.result.current.allSeries, 3)).toBe(BOOKS_PER_SERIES);
  });
});
