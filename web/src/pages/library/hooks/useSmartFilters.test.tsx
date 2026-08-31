/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「保存的智能视图必须连高级筛选一起存」。视图的成员由过滤条件推导而来，
 * 丢掉阅读状态/评分/进度/加入天数这六个维度就等于换了一个视图——应用后结果集比保存时大得多，
 * 用户只会以为视图坏了。后端 smart_filters 六列与 UpsertSmartFilterRequest 一直都收，
 * 这里守的是前端这一侧：存要发出去、读要还原回来、应用要真的生效。
 */

import type { ReactNode } from 'react';
import { act, renderHook } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { apiClient } from '../../../api/client';
import { DEFAULT_PAGE_SIZE, type SavedSmartFilter } from '../types';
import { EMPTY_ADVANCED_FILTERS, type AdvancedFilters } from './libraryFilterParams';
import { smartFilterToSnapshot } from './smartFilterNormalize';
import { useLibraryFilters } from './useLibraryFilters';
import { useSmartFilters } from './useSmartFilters';

const LIB_ID = '7';
const SMART_FILTERS_URL = `/api/libraries/${LIB_ID}/smart-filters/`;

// 现象里那组条件：未读 + 评分≥8 + 最近 30 天加入。
const ADVANCED: AdvancedFilters = {
  readState: 'unread',
  minRating: 8,
  maxRating: null,
  minProgress: null,
  maxProgress: null,
  addedWithinDays: 30,
};

// 复刻后端 upsertSmartFilter 的行为：请求体原样落库再回显。
// 前端漏发哪个字段，回显里就真的没有它——与真实服务端一致，不会替前端把字段补回去。
let stored: SavedSmartFilter[] = [];

function mockServer() {
  const post = vi.spyOn(apiClient, 'post').mockImplementation((async (_url: string, body: SavedSmartFilter) => {
    const row = { ...body, id: String(stored.length + 1), createdAt: '2026-01-01T00:00:00.000Z' };
    stored = [row, ...stored.filter((item) => item.name !== body.name)];
    return { data: row };
  }) as never);
  const get = vi.spyOn(apiClient, 'get').mockImplementation((async () => ({ data: stored })) as never);
  return { post, get };
}

function wrapper({ children }: { children: ReactNode }) {
  return <MemoryRouter>{children}</MemoryRouter>;
}

// 与 library/index.tsx 相同的接线：视图存的是当前筛选快照，应用时经 smartFilterToSnapshot 落回筛选状态。
function useLibraryWithSavedViews() {
  const filters = useLibraryFilters({ libId: LIB_ID });
  const views = useSmartFilters({
    libId: LIB_ID,
    onError: () => {},
    onSaved: () => {},
    onApplied: (filter) => filters.applySnapshot(smartFilterToSnapshot(filter)),
  });
  return { filters, views };
}

function renderLibrary() {
  return renderHook(() => useLibraryWithSavedViews(), { wrapper });
}

/** 复刻 index.tsx 交给 saveSmartFilter 的那份当前筛选状态。 */
function currentStateOf(filters: ReturnType<typeof useLibraryFilters>) {
  return {
    activeTag: filters.activeTag,
    activeAuthor: filters.activeAuthor,
    activeStatus: filters.activeStatus,
    activeLetter: filters.activeLetter,
    sortByField: filters.sortByField,
    sortDir: filters.sortDir,
    pageSize: filters.pageSize,
    advanced: filters.advanced,
  };
}

beforeEach(() => {
  stored = [];
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('智能视图与高级筛选', () => {
  it('保存视图时把六个高级筛选维度一起发给后端', async () => {
    const { post } = mockServer();
    const { result } = renderHook(() =>
      useSmartFilters({ libId: LIB_ID, onError: () => {}, onSaved: () => {}, onApplied: () => {} }),
    );

    await act(async () => {
      await result.current.saveSmartFilter('高分未读', {
        activeTag: 'SF',
        activeAuthor: null,
        activeStatus: null,
        activeLetter: null,
        sortByField: 'name',
        sortDir: 'asc',
        pageSize: DEFAULT_PAGE_SIZE,
        advanced: ADVANCED,
      });
    });

    expect(post).toHaveBeenCalledWith(
      SMART_FILTERS_URL,
      expect.objectContaining({
        name: '高分未读',
        activeTag: 'SF',
        readState: 'unread',
        minRating: 8,
        maxRating: null,
        minProgress: null,
        maxProgress: null,
        addedWithinDays: 30,
      }),
    );
  });

  it('从后端读回视图时还原六个维度，后端给 null 的维度不会变成 undefined', async () => {
    // 库里的旧视图与旧本地缓存就是这个形状：只带用户真正设过的三维，其余键整个缺席。
    stored = [
      {
        id: '1',
        name: '高分未读',
        readState: 'unread',
        minRating: 8,
        addedWithinDays: 30,
        sortByField: 'name',
        sortDir: 'asc',
        pageSize: DEFAULT_PAGE_SIZE,
        createdAt: '2026-01-01T00:00:00.000Z',
      } as SavedSmartFilter,
    ];
    mockServer();
    const { result } = renderHook(() =>
      useSmartFilters({ libId: LIB_ID, onError: () => {}, onSaved: () => {}, onApplied: () => {} }),
    );

    await act(async () => {
      result.current.ensureLoaded();
      await Promise.resolve();
    });

    const view = result.current.savedSmartFilters[0];
    expect(view.readState).toBe('unread');
    expect(view.minRating).toBe(8);
    expect(view.addedWithinDays).toBe(30);
    // null 与 undefined 在这里不等价：hasAdvancedFilters 判的是 !== null，
    // undefined 会被当成「设了这一维」，凭空多出一个不可见的筛选条件。
    expect(view.maxRating).toBeNull();
    expect(view.minProgress).toBeNull();
    expect(view.maxProgress).toBeNull();
  });

  it('应用保存的视图后高级筛选按视图恢复，不被静默清空', async () => {
    mockServer();
    const { result } = renderLibrary();

    // 用户设好「SF 标签 + 未读 + 评分≥8 + 最近 30 天」，存成视图。
    act(() => {
      result.current.filters.setActiveTag('SF');
      result.current.filters.setAdvancedFilters(ADVANCED);
    });
    await act(async () => {
      await result.current.views.saveSmartFilter('高分未读', currentStateOf(result.current.filters));
    });

    // 之后清空重来，再应用这个视图。
    act(() => {
      result.current.filters.resetAll();
    });
    expect(result.current.filters.advanced).toEqual(EMPTY_ADVANCED_FILTERS);

    act(() => {
      result.current.views.applySmartFilter(result.current.views.savedSmartFilters[0]);
    });

    expect(result.current.filters.activeTag).toBe('SF');
    expect(result.current.filters.advanced).toEqual(ADVANCED);
  });

  it('只设了高级筛选也能存出有效视图，而不是一个空视图', async () => {
    mockServer();
    const { result } = renderLibrary();

    // 标签/作者/状态/首字母一个都没设，条件全在高级筛选里。
    act(() => {
      result.current.filters.setAdvancedFilters(ADVANCED);
    });
    await act(async () => {
      await result.current.views.saveSmartFilter('只有高级筛选', currentStateOf(result.current.filters));
    });

    act(() => {
      result.current.filters.resetAll();
    });
    act(() => {
      result.current.views.applySmartFilter(result.current.views.savedSmartFilters[0]);
    });

    expect(result.current.filters.advanced).toEqual(ADVANCED);
  });

  it('保存失败时回滚乐观插入的条目，不在列表里留幽灵', async () => {
    const post = vi.spyOn(apiClient, 'post').mockRejectedValue(new Error('boom'));
    const errors: string[] = [];
    const { result } = renderHook(() =>
      useSmartFilters({
        libId: LIB_ID,
        onError: (msg) => errors.push(msg),
        onSaved: () => {},
        onApplied: () => {},
      }),
    );

    await act(async () => {
      await result.current.saveSmartFilter('高分未读', {
        activeTag: null,
        activeAuthor: null,
        activeStatus: null,
        activeLetter: null,
        sortByField: 'name',
        sortDir: 'asc',
        pageSize: DEFAULT_PAGE_SIZE,
        advanced: ADVANCED,
      });
    });

    expect(post).toHaveBeenCalledOnce();
    expect(errors).toEqual(['home.smartFilters.saveFailed']);
    // 留下来的条目带的是客户端 id，后端不认识；删它拿 404、被删除那侧的回滚原样放回，用户只能刷新页面。
    expect(result.current.savedSmartFilters).toEqual([]);
  });
});
