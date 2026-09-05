/**
 * @vitest-environment jsdom
 *
 * 守智能视图列表不跨资料库串数据：路由是 library/:libId，切库时组件不卸载，展开面板后发出的
 * 请求若不认世代号，甲库的响应回来照样写进乙库的面板，套用一条就把甲库的条件打到乙库的网格上。
 */

import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { apiClient } from '../../../api/client';
import type { SavedSmartFilter } from '../types';
import { useSmartFilters } from './useSmartFilters';

const LIB_A = '1';
const LIB_B = '2';

function view(name: string): SavedSmartFilter {
  return {
    id: name,
    name,
    activeTag: null,
    activeAuthor: null,
    activeStatus: null,
    activeLetter: null,
    readState: null,
    minRating: null,
    maxRating: null,
    minProgress: null,
    maxProgress: null,
    addedWithinDays: null,
    sortByField: 'name',
    sortDir: 'asc',
    pageSize: 30,
    createdAt: '2026-01-01T00:00:00.000Z',
  } as SavedSmartFilter;
}

/** 受控 promise：甲库的响应什么时候回来由用例说了算，不看机器快慢。 */
function deferred<T>() {
  let settle!: (value: T) => void;
  const promise = new Promise<T>((resolve) => { settle = resolve; });
  return { promise, settle };
}

function renderSmartFilters(libId: string) {
  return renderHook(
    ({ id }: { id: string }) => useSmartFilters({ libId: id, onError: () => {}, onSaved: () => {}, onApplied: () => {} }),
    { initialProps: { id: libId } },
  );
}

beforeEach(() => {
  localStorage.clear();
  // 乙库自己的本地缓存：切过去时面板先用它填上，甲库的迟到响应不该顶掉它。
  localStorage.setItem(`lib_smart_filters_cache_${LIB_B}`, JSON.stringify([view('乙库的视图')]));
});

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

describe('智能视图列表切库', () => {
  it('展开面板后立刻切库，甲库的响应回来不会列进乙库的面板', async () => {
    const slowA = deferred<{ data: SavedSmartFilter[] }>();
    const get = vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
      if (url.includes(`/api/libraries/${LIB_A}/`)) return slowA.promise;
      return Promise.resolve({ data: [] });
    }) as never);

    const { result, rerender } = renderSmartFilters(LIB_A);

    // 在甲库展开面板：请求发出去了，挂着不回。
    act(() => { result.current.ensureLoaded(); });
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));

    // 面板还开着就切到乙库，组件不卸载。
    rerender({ id: LIB_B });
    await waitFor(() => expect(result.current.savedSmartFilters.map((item) => item.name)).toEqual(['乙库的视图']));

    await act(async () => { slowA.settle({ data: [view('甲库的视图')] }); });

    expect(result.current.savedSmartFilters.map((item) => item.name)).toEqual(['乙库的视图']);
  });

  it('切回甲库再展开面板时会重新取一次：「只跑一次」是按资料库各记一次', async () => {
    const get = vi.spyOn(apiClient, 'get').mockImplementation((async (url: string) => (
      { data: url.includes(`/api/libraries/${LIB_A}/`) ? [view('甲库的视图')] : [view('乙库的视图')] }
    )) as never);

    const { result, rerender } = renderSmartFilters(LIB_A);

    await act(async () => { result.current.ensureLoaded(); });
    await waitFor(() => expect(result.current.savedSmartFilters.map((item) => item.name)).toEqual(['甲库的视图']));

    // 同一个库内重复展开不重复取。
    act(() => { result.current.ensureLoaded(); });
    expect(get).toHaveBeenCalledTimes(1);

    rerender({ id: LIB_B });
    await act(async () => { result.current.ensureLoaded(); });
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    expect(result.current.savedSmartFilters.map((item) => item.name)).toEqual(['乙库的视图']);
  });
});
