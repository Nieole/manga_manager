/**
 * @vitest-environment jsdom
 *
 * 守 allInVolume 的两条：/context 抖一次就把「已取过」的标记写死的话，同一系列内再怎么换书都不会
 * 重取，allInVolume 恒空，顶栏的章节列表按钮永久消失且没有任何报错；跨系列换书时旧系列的书不当场
 * 丢掉的话，两个系列都有「第 1 卷」就交集非空，按钮亮着而点进去跳到另一个系列的书。
 */

import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { apiClient } from '../../api/client';
import { useReaderSiblings } from './useReaderSiblings';

const SERIES_ID = 7;

const CONTEXT_PAYLOAD = {
  data: {
    series: { id: SERIES_ID },
    books: [
      { id: 1, name: '第 1 话', volume: '1' },
      { id: 2, name: '第 2 话', volume: '1' },
      { id: 3, name: '第 3 话', volume: '2' },
    ],
  },
};

// 前后书是另一条链路，这里一律按 404 回（阅读器把 404 当成「没有上一本/下一本」，不打日志）。
function notFound() {
  return Object.assign(new Error('not found'), { isAxiosError: true, response: { status: 404 } });
}

let contextCalls = 0;

function mockApi(failFirstContext: boolean) {
  contextCalls = 0;
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
    if (url.startsWith('/api/series/')) {
      contextCalls++;
      if (failFirstContext && contextCalls === 1) return Promise.reject(new Error('network blip'));
      return Promise.resolve(CONTEXT_PAYLOAD);
    }
    return Promise.reject(notFound());
  }) as never);
}

function renderSiblings(bookId: string) {
  const seriesIdRef = { current: SERIES_ID as number | null };
  return renderHook(
    ({ id }: { id: string }) => useReaderSiblings({ bookId: id, seriesIdRef, bookVolume: '1', loading: false }),
    { initialProps: { id: bookId } },
  );
}

const OTHER_SERIES_ID = 8;

// 另一个系列同样有「第 1 卷」——跨系列换书时最常见的形态。
const OTHER_CONTEXT_PAYLOAD = {
  data: {
    series: { id: OTHER_SERIES_ID },
    books: [{ id: 91, name: '别的系列 第 1 话', volume: '1' }],
  },
};

beforeEach(() => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('跨系列换书时的卷内章节列表', () => {
  it('新系列的上下文还没回来时，卷内章节列表不得列着旧系列的书', async () => {
    // 新系列的 /context 挂起，好把「请求发出去了、还没回来」这段窗口摊开。
    let releaseOther: (() => void) | null = null;
    vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
      if (url === `/api/series/${SERIES_ID}/context`) return Promise.resolve(CONTEXT_PAYLOAD);
      if (url === `/api/series/${OTHER_SERIES_ID}/context`) {
        return new Promise((resolve) => {
          releaseOther = () => resolve(OTHER_CONTEXT_PAYLOAD);
        });
      }
      return Promise.reject(notFound());
    }) as never);

    const seriesIdRef = { current: SERIES_ID as number | null };
    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useReaderSiblings({ bookId: id, seriesIdRef, bookVolume: '1', loading: false }),
      { initialProps: { id: '1' } },
    );
    await waitFor(() => expect(result.current.allInVolume).toHaveLength(2));

    // 换到另一个系列的书：seriesIdRef 由书籍数据那一侧命令式改写，随后本 hook 才发新请求。
    seriesIdRef.current = OTHER_SERIES_ID;
    rerender({ id: '91' });
    await waitFor(() => expect(releaseOther).not.toBeNull());

    // 留着旧系列的书就等于在说「这一卷里还有这几话」，而那几话属于另一个系列。
    expect(result.current.allInVolume).toHaveLength(0);
    expect(result.current.currentIndexInVolume).toBe(-1);

    await act(async () => {
      releaseOther!();
    });
    await waitFor(() => expect(result.current.allInVolume).toHaveLength(1));
    expect(result.current.currentIndexInVolume).toBe(0);
  });
});

describe('useReaderSiblings 的系列上下文取数', () => {
  it('取失败之后，同一系列内换一本书要重取，章节列表回得来', async () => {
    mockApi(true);
    const { result, rerender } = renderSiblings('1');

    await waitFor(() => expect(contextCalls).toBe(1));
    expect(result.current.allInVolume).toHaveLength(0);

    rerender({ id: '2' });
    await waitFor(() => expect(contextCalls).toBe(2));
    await waitFor(() => expect(result.current.allInVolume).toHaveLength(2));
    expect(result.current.currentIndexInVolume).toBe(1);
  });

  it('取成功之后，同一系列内换书不再重复取上下文', async () => {
    mockApi(false);
    const { result, rerender } = renderSiblings('1');

    await waitFor(() => expect(result.current.allInVolume).toHaveLength(2));
    expect(contextCalls).toBe(1);

    rerender({ id: '2' });
    await waitFor(() => expect(result.current.currentIndexInVolume).toBe(1));
    expect(contextCalls).toBe(1);
  });
});
