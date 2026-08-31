/**
 * @vitest-environment jsdom
 *
 * 守阅读器页图缓存的 Object URL 生命周期：createObjectURL 造出的 URL 不显式 revoke
 * 就不会被回收，整本书读下来能积几百 MB 且不报错。三条防线都要覆盖：滑动窗口外的页、
 * 清空缓存后迟到的响应、切书丢弃的整本缓存。
 *
 * jsdom 不实现 Object URL，用 liveObjectUrls/revoked 两个替身表判断泄漏与否。
 */

import { renderHook, act } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createRef } from 'react';

import { usePageImageCache } from './usePageImageCache';

const liveObjectUrls = new Set<string>();
const revoked: string[] = [];
let urlSeq = 0;

// deferred fetch：把 resolve 存起来，好精确控制「先清空、后到达」的时序。
interface Deferred {
  url: string;
  signal: AbortSignal;
  resolve: (blob: unknown) => void;
  reject: (err: unknown) => void;
}
let pending: Deferred[] = [];

beforeEach(() => {
  liveObjectUrls.clear();
  revoked.length = 0;
  urlSeq = 0;
  pending = [];

  vi.stubGlobal('URL', {
    ...URL,
    createObjectURL: () => {
      urlSeq += 1;
      const url = `blob:mock/${urlSeq}`;
      liveObjectUrls.add(url);
      return url;
    },
    revokeObjectURL: (url: string) => {
      revoked.push(url);
      liveObjectUrls.delete(url);
    },
  });

  vi.stubGlobal('fetch', (url: string, init?: { signal: AbortSignal }) =>
    new Promise((resolve, reject) => {
      pending.push({
        url,
        signal: init!.signal,
        resolve: (blob) => resolve({ ok: true, status: 200, blob: async () => blob } as unknown as Response),
        reject,
      });
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function setup(bookId = '42') {
  const currentBookIdRef = createRef<string | null>() as { current: string | null };
  currentBookIdRef.current = bookId;
  return renderHook(() =>
    usePageImageCache({
      imageFilter: 'none',
      w2xScale: 2,
      w2xNoise: 1,
      w2xFormat: 'png',
      autoCrop: false,
      readerImageFormat: 'webp',
      readerImageQuality: 80,
      currentBookIdRef,
    }),
  );
}

// settle 让最早的那个在途请求成功返回。
async function settleFirst() {
  const next = pending.shift();
  await act(async () => {
    next!.resolve({});
    await Promise.resolve();
  });
}

describe('页图缓存的 Object URL 生命周期', () => {
  it('滑动窗口外的页要被 revoke，而不只是从 Map 里删掉', async () => {
    const { result } = setup();
    await act(async () => {
      void result.current.ensurePageImageLoaded('42', 1);
      void result.current.ensurePageImageLoaded('42', 2);
    });
    await settleFirst();
    await settleFirst();
    expect(liveObjectUrls.size).toBe(2);

    act(() => result.current.releasePageImagesOutsideWindow('42', [2]));

    // 「Map 删了就没引用了」是最容易犯的错——Object URL 不是这么回收的。
    expect(revoked).toEqual(['blob:mock/1']);
    expect(liveObjectUrls.size).toBe(1);
  });

  it('窗口外的在途请求要被 abort', async () => {
    const { result } = setup();
    await act(async () => {
      void result.current.ensurePageImageLoaded('42', 1);
      void result.current.ensurePageImageLoaded('42', 2);
    });
    const first = pending[0];

    act(() => result.current.releasePageImagesOutsideWindow('42', [2]));

    // 不 abort 的话，用户快速翻页时会把整本书的流量都拉下来。
    expect(first.signal.aborted).toBe(true);
  });

  it('清空缓存之后迟到的响应要自我吊销，不写回缓存', async () => {
    const { result } = setup();
    let settled: unknown;
    await act(async () => {
      result.current.ensurePageImageLoaded('42', 1).catch((err) => {
        settled = err;
      });
    });

    act(() => result.current.clearAllPageImageCaches());
    await settleFirst();

    // 没有这道代际判定，迟到的 blob 会写进一个已经被清空的缓存：
    // 它既不会被后续的 release 扫到（窗口算的是新一轮的页），也没人再 revoke 它。
    expect(settled).toBeInstanceOf(DOMException);
    expect((settled as DOMException).name).toBe('AbortError');
    expect(result.current.cachedPageImageUrls).toEqual({});
    expect(liveObjectUrls.size).toBe(0);
  });

  it('切书丢弃的整本缓存要逐个 revoke', async () => {
    const { result } = setup();
    await act(async () => {
      void result.current.ensurePageImageLoaded('42', 1);
    });
    await settleFirst();
    expect(liveObjectUrls.size).toBe(1);

    // 只保留另一本书 → 42 这本整份丢弃。
    act(() => result.current.retainBookCaches(['77']));

    expect(revoked).toEqual(['blob:mock/1']);
    expect(liveObjectUrls.size).toBe(0);
  });

  it('同一页并发请求只发一次网络', async () => {
    const { result } = setup();
    await act(async () => {
      void result.current.ensurePageImageLoaded('42', 3);
      void result.current.ensurePageImageLoaded('42', 3);
    });
    // 去重靠的是 imageRequests 这张表；丢了它，预加载窗口会把同一页重复拉好几遍。
    expect(pending).toHaveLength(1);
  });

  it('请求失败不做负缓存，下次仍会重试', async () => {
    const { result } = setup();
    let firstError: unknown;
    await act(async () => {
      result.current.ensurePageImageLoaded('42', 5).catch((err) => {
        firstError = err;
      });
    });
    await act(async () => {
      pending.shift()!.reject(new Error('boom'));
      await Promise.resolve();
    });
    expect(firstError).toBeInstanceOf(Error);

    await act(async () => {
      void result.current.ensurePageImageLoaded('42', 5);
    });
    // 失败留在表里的话，这一页在本次阅读中永远加载不出来了。
    expect(pending).toHaveLength(1);
  });
});
