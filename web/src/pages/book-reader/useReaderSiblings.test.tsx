/**
 * @vitest-environment jsdom
 *
 * 守「系列上下文取失败之后还能重取」：/context 抖一次就把「已取过」的标记写死的话，同一系列内
 * 再怎么换书都不会重取，allInVolume 恒空，顶栏的章节列表按钮永久消失且没有任何报错。
 */

import { renderHook, waitFor } from '@testing-library/react';
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

beforeEach(() => {
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
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
