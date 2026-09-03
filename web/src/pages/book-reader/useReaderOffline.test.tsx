/**
 * @vitest-environment jsdom
 *
 * 守「阅读器换书之后，离线面板讲的是新书的事」。下载在用户翻到下一本之后仍会跑完（有意如此，
 * 用户点「缓存本书」要的就是它下完），所以下载态必须按 bookId 分格：各存一份单值会让新书的
 * 面板挂着上一本的已下载页数与错误红字，还把「缓存本书」「移除」两个按钮一起锁死好几分钟。
 * 面板上那几处禁用与数字都是这几个返回值的直接投影，所以判据落在钩子上。
 */

import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useReaderOffline } from './useReaderOffline';
import type { CacheOfflineBookOptions, OfflineBookStatus } from './offlineReader';
import type { Page } from './types';

const mocks = vi.hoisted(() => ({
  cacheBookForOffline: vi.fn(),
  deleteOfflineBook: vi.fn(),
  getOfflineBookStatus: vi.fn(),
}));

// 只替掉离线存储那一层：本文件测的是钩子怎么把下载态分派回各自那本书。
vi.mock('./offlineReader', () => ({
  supportsOfflineReaderCache: () => true,
  cacheBookForOffline: mocks.cacheBookForOffline,
  deleteOfflineBook: mocks.deleteOfflineBook,
  getOfflineBookStatus: mocks.getOfflineBookStatus,
  getQueuedOfflineProgress: () => null,
  queueOfflineProgress: () => {},
  syncQueuedOfflineProgress: async () => ({ synced: 0, failed: 0, remaining: 0 }),
}));

const PAGES: Page[] = [
  { number: 1, width: 800, height: 1200 },
  { number: 2, width: 800, height: 1200 },
  { number: 3, width: 800, height: 1200 },
];

function renderOffline(initialBookId: string) {
  return renderHook(({ bookId }: { bookId: string }) => useReaderOffline({
    bookId,
    bookTitle: `书 ${bookId}`,
    pages: PAGES,
    imageFilter: 'none',
    autoCrop: false,
    readerImageFormat: 'original',
    readerImageQuality: 80,
    getImageUrlForBook: (id: string, pageNumber: number) => `/api/pages/${id}/${pageNumber}`,
    t: (key: string) => key,
  }), { initialProps: { bookId: initialBookId } });
}

// flush 把渲染后挂上的异步 effect（状态刷新、队列同步）跑完，免得断言撞进 act 警告。
async function flush() {
  await act(async () => {});
}

beforeEach(() => {
  mocks.getOfflineBookStatus.mockResolvedValue(null);
  mocks.deleteOfflineBook.mockResolvedValue(undefined);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('换书时的离线下载态', () => {
  // 复现：长书下到一半翻到末页自动跳下一本（同一个 BookReader 实例，只换 bookId）。
  // 新书的面板于是写着「正在缓存…」，分子是上一本的已下载页数、分母是新书的总页数，
  // 两个按钮全程禁用——一本长书能锁上好几分钟。
  it('上一本书还在下载时换书，新书的面板不挂它的进度与禁用态', async () => {
    let reportProgress: ((cached: number, total: number) => void) | undefined;
    mocks.cacheBookForOffline.mockImplementation((options: CacheOfflineBookOptions) => {
      reportProgress = options.onProgress;
      return new Promise<OfflineBookStatus | null>(() => {});
    });

    const { result, rerender } = renderOffline('A');
    await flush();
    act(() => result.current.cacheBookOffline());
    act(() => reportProgress?.(2, 3));
    expect(result.current.offlineCaching).toBe(true);
    expect(result.current.offlineCachedPages).toBe(2);

    rerender({ bookId: 'B' });
    await flush();

    expect(result.current.offlineCaching).toBe(false);
    expect(result.current.offlineCachedPages).toBe(0);
  });

  // 换书不等于下载被取消：切回去要看得见它还在下，否则「没在下载」是另一种撒谎。
  it('切回原来那本书，仍看得见它在下载以及下到第几页', async () => {
    let reportProgress: ((cached: number, total: number) => void) | undefined;
    mocks.cacheBookForOffline.mockImplementation((options: CacheOfflineBookOptions) => {
      reportProgress = options.onProgress;
      return new Promise<OfflineBookStatus | null>(() => {});
    });

    const { result, rerender } = renderOffline('A');
    await flush();
    act(() => result.current.cacheBookOffline());
    act(() => reportProgress?.(2, 3));
    rerender({ bookId: 'B' });
    await flush();

    rerender({ bookId: 'A' });
    await flush();

    expect(result.current.offlineCaching).toBe(true);
    expect(result.current.offlineCachedPages).toBe(2);
  });

  // 换书之后上一本的下载才报错（配额不足、页图 404、会话过期 401）：红字属于那一本。
  it('上一本书的下载失败，错误不挂到新书的面板上', async () => {
    let failDownload: ((err: Error) => void) | undefined;
    mocks.cacheBookForOffline.mockImplementation(() => new Promise<OfflineBookStatus | null>((_, reject) => {
      failDownload = reject;
    }));

    const { result, rerender } = renderOffline('A');
    await flush();
    act(() => result.current.cacheBookOffline());
    rerender({ bookId: 'B' });
    await flush();

    await act(async () => {
      failDownload?.(new Error('磁盘配额不足'));
    });

    expect(result.current.offlineCacheError).toBeNull();
    expect(result.current.offlineCaching).toBe(false);

    // 反向判据：错误没被丢掉，切回那本书照样看得见。
    rerender({ bookId: 'A' });
    await flush();
    expect(result.current.offlineCacheError).toBe('磁盘配额不足');
  });

  // 上一本书的下载收尾时交出的是它自己的状态，盖到新书上就等于谎报新书已缓存。
  it('上一本书下载完成，不把它的已缓存状态盖到新书上', async () => {
    let finishDownload: ((status: OfflineBookStatus) => void) | undefined;
    mocks.cacheBookForOffline.mockImplementation(() => new Promise<OfflineBookStatus | null>((resolve) => {
      finishDownload = resolve;
    }));

    const { result, rerender } = renderOffline('A');
    await flush();
    act(() => result.current.cacheBookOffline());
    rerender({ bookId: 'B' });
    await flush();

    await act(async () => {
      finishDownload?.({
        bookId: 'A',
        title: '书 A',
        pageCount: 3,
        cachedPages: 3,
        cachedAt: '2026-09-03T00:00:00.000Z',
        imageProfile: 'original',
      });
    });

    expect(result.current.offlineStatus).toBeNull();
  });

  // 删除同样跨书存活，「移除」按钮的禁用态也得跟着那一本走。
  it('删除上一本书的过程中换书，新书的移除按钮不被锁住', async () => {
    let finishDelete: (() => void) | undefined;
    mocks.deleteOfflineBook.mockImplementation(() => new Promise<void>((resolve) => {
      finishDelete = resolve;
    }));

    const { result, rerender } = renderOffline('A');
    await flush();
    act(() => result.current.deleteBookOffline());
    expect(result.current.offlineDeleting).toBe(true);

    rerender({ bookId: 'B' });
    await flush();
    expect(result.current.offlineDeleting).toBe(false);

    await act(async () => {
      finishDelete?.();
    });
    expect(result.current.offlineDeleting).toBe(false);
  });

  // 反向判据：不换书时下载态照旧完整地走一遍，收尾把状态交给这本书。
  it('不换书时下载照常从进行中走到已缓存', async () => {
    const status: OfflineBookStatus = {
      bookId: 'A',
      title: '书 A',
      pageCount: 3,
      cachedPages: 3,
      cachedAt: '2026-09-03T00:00:00.000Z',
      imageProfile: 'original',
    };
    let reportProgress: ((cached: number, total: number) => void) | undefined;
    let finishDownload: ((value: OfflineBookStatus) => void) | undefined;
    mocks.cacheBookForOffline.mockImplementation((options: CacheOfflineBookOptions) => {
      reportProgress = options.onProgress;
      return new Promise<OfflineBookStatus | null>((resolve) => {
        finishDownload = resolve;
      });
    });

    const { result } = renderOffline('A');
    await flush();
    act(() => result.current.cacheBookOffline());
    act(() => reportProgress?.(3, 3));
    expect(result.current.offlineCaching).toBe(true);
    expect(result.current.offlineCachedPages).toBe(3);

    await act(async () => {
      finishDownload?.(status);
    });

    expect(result.current.offlineCaching).toBe(false);
    expect(result.current.offlineCachedPages).toBe(0);
    expect(result.current.offlineStatus).toEqual(status);
  });
});
