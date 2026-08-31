/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「页码与游标只在用户改筛选时才重置」。判据认错方向会两头出错：
 * 把水合当用户操作，分享出去的 ?q=foo&page=3 会被冲回第 1 页；把用户操作当水合，
 * 改排序时旧排序算出的游标会被带进新查询，用户看到错位、跳过的结果。
 */

import { useEffect, useState, type ReactNode } from 'react';
import { act, cleanup, renderHook } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useNavigate, useParams } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { apiClient } from '../../../api/client';
import { useLibraryFilters } from './useLibraryFilters';
import { useLibrarySeries } from './useLibrarySeries';
import { useResetPageOnFilterChange } from './useResetPageOnFilterChange';

// 每条用例用不同的 libId：useLibrarySeries 的在途请求去重表是模块级的，
// 换库即换 query 字符串，用例之间不会互相吞掉请求。
const requests: string[] = [];

const lastQuery = () => requests[requests.length - 1] ?? '';
const pageOf = (query: string) => new URLSearchParams(query).get('page');
const requestedPages = () => requests.map(pageOf);

// 复刻后端 /api/series/search 的分页形状：next_cursor 由本次请求的页码推出，
// 于是「翻到第 3 页」拿到的就是第 2 页响应给出的 cur-3，与真实游标链一致。
function mockSeriesSearch() {
  vi.spyOn(apiClient, 'get').mockImplementation((async (url: string) => {
    const query = url.replace('/api/series/search?', '');
    requests.push(query);
    const page = Number(new URLSearchParams(query).get('page') || '1');
    return { data: { items: [], total: 100, next_cursor: `cur-${page + 1}` } };
  }) as never);
}

// 与 library/index.tsx 相同的接线：筛选状态 → 300ms 防抖的关键词 → 主查询 → 页码重置。
// 重置守卫本身用的是页面上真实那一份 useResetPageOnFilterChange。
function useLibraryQueryHarness() {
  const { libId } = useParams<{ libId: string }>();
  const navigate = useNavigate();
  const filters = useLibraryFilters({ libId });
  const [debouncedKeyword, setDebouncedKeyword] = useState('');
  useEffect(() => {
    const id = window.setTimeout(() => setDebouncedKeyword(filters.keyword.trim()), 300);
    return () => window.clearTimeout(id);
  }, [filters.keyword]);
  const series = useLibrarySeries({
    libId,
    page: filters.page,
    pageSize: filters.pageSize,
    activeTag: filters.activeTag,
    activeAuthor: filters.activeAuthor,
    activeStatus: filters.activeStatus,
    activeLetter: filters.activeLetter,
    advanced: filters.advanced,
    sortByField: filters.sortByField,
    sortDir: filters.sortDir,
    refreshTrigger: 0,
    enabled: filters.settingsReady && debouncedKeyword === filters.keyword.trim(),
    keyword: debouncedKeyword,
  });
  useResetPageOnFilterChange(filters, series.resetPagination);
  return { filters, navigate };
}

function renderLibrary(entry: string) {
  return renderHook(useLibraryQueryHarness, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/library/:libId" element={<>{children}</>} />
        </Routes>
      </MemoryRouter>
    ),
  });
}

// 推进防抖(300ms)、设置落盘(400ms)与请求 promise，直到界面静止。
async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(600);
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
  window.localStorage.clear();
  requests.length = 0;
  mockSeriesSearch();
});

describe('资料库页码重置守卫', () => {
  it('带关键词的深链刷新后停在原页，不被冲回第 1 页', async () => {
    const { result } = renderLibrary('/library/7002?q=foo&page=3');
    await settle();

    expect(requestedPages()).toEqual(['3']);
    expect(result.current.filters.page).toBe(3);
  });

  it('不带关键词的深链刷新后停在原页', async () => {
    const { result } = renderLibrary('/library/7003?page=3');
    await settle();

    expect(requestedPages()).toEqual(['3']);
    expect(result.current.filters.page).toBe(3);
  });

  it('首访(本地无存档)时改每页数量也要回到第 1 页', async () => {
    const { result } = renderLibrary('/library/7001');
    await settle();
    act(() => result.current.filters.setPage(3));
    await settle();

    act(() => result.current.filters.setPageSize(50));
    await settle();

    expect(lastQuery()).toContain('limit=50');
    expect(pageOf(lastQuery())).toBe('1');
    expect(result.current.filters.page).toBe(1);
  });

  it('首访(本地无存档)时改排序要清掉旧排序算出的游标', async () => {
    const { result } = renderLibrary('/library/7005');
    await settle();
    act(() => result.current.filters.setPage(2));
    await settle();
    act(() => result.current.filters.setPage(3));
    await settle();
    expect(lastQuery()).toContain('cursor=cur-3');

    act(() => result.current.filters.setSortByField('updated'));
    await settle();

    expect(lastQuery()).toContain('sortBy=updated_asc');
    expect(lastQuery()).not.toContain('cursor=');
    expect(pageOf(lastQuery())).toBe('1');
  });

  it('已有本地存档时改筛选照样回到第 1 页并清游标', async () => {
    window.localStorage.setItem(
      'library:7004:settings',
      JSON.stringify({ sortByField: 'name', sortDir: 'asc', pageSize: 30, page: 1 }),
    );
    const { result } = renderLibrary('/library/7004');
    await settle();
    act(() => result.current.filters.setPage(3));
    await settle();

    act(() => result.current.filters.setActiveStatus('ongoing'));
    await settle();

    expect(lastQuery()).toContain('status=ongoing');
    expect(lastQuery()).not.toContain('cursor=');
    expect(pageOf(lastQuery())).toBe('1');
  });

  it('切到另一个库时恢复该库存档里的页码', async () => {
    window.localStorage.setItem('library:7007:settings', JSON.stringify({ page: 4 }));
    const { result } = renderLibrary('/library/7006');
    await settle();
    act(() => result.current.filters.setPage(2));
    await settle();

    act(() => result.current.navigate('/library/7007'));
    await settle();

    expect(lastQuery()).toContain('libraryId=7007');
    expect(pageOf(lastQuery())).toBe('4');
    expect(result.current.filters.page).toBe(4);
  });

  it('切库之后第一次改筛选仍然回到第 1 页', async () => {
    window.localStorage.setItem('library:7009:settings', JSON.stringify({ page: 4 }));
    const { result } = renderLibrary('/library/7008');
    await settle();
    act(() => result.current.navigate('/library/7009'));
    await settle();

    act(() => result.current.filters.setActiveTag('少年'));
    await settle();

    expect(lastQuery()).toContain('tags=');
    expect(pageOf(lastQuery())).toBe('1');
  });
});
