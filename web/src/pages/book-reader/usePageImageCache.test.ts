/**
 * @vitest-environment jsdom
 *
 * 守阅读器页图缓存的两条不变量：一是 Object URL 的生命周期，createObjectURL 造出的 URL 不显式
 * revoke 就不会被回收，整本书读下来能积几百 MB 且不报错；二是缓存的身份等同于请求地址，作废与否
 * 只看地址变没变，代际按书各算——两者任一破了，当前页会停在转圈上，只有手动翻页才恢复。
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

function setup(bookId = '42', overrides: Partial<Parameters<typeof usePageImageCache>[0]> = {}) {
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
      targetWidth: 0,
      targetHeight: 0,
      currentBookIdRef,
      ...overrides,
    }),
  );
}

type CacheOptions = Parameters<typeof usePageImageCache>[0];

// setupRerenderable 把取图参数交给 rerender，好模拟拖窗口跨档这类「参数换了一套」的动作。
function setupRerenderable(initialProps: Partial<CacheOptions>) {
  const currentBookIdRef = createRef<string | null>() as { current: string | null };
  currentBookIdRef.current = '42';
  return renderHook((props: Partial<CacheOptions>) =>
    usePageImageCache({
      imageFilter: 'none',
      w2xScale: 2,
      w2xNoise: 1,
      w2xFormat: 'png',
      autoCrop: false,
      readerImageFormat: 'webp',
      readerImageQuality: 80,
      targetWidth: 0,
      targetHeight: 0,
      currentBookIdRef,
      ...props,
    }),
  { initialProps });
}

// settle 让最早的那个在途请求成功返回。
async function settleFirst() {
  const next = pending.shift();
  await act(async () => {
    next!.resolve({});
    await Promise.resolve();
  });
}

describe('页图地址带不带目标尺寸', () => {
  it('重采样滤镜下发档位与 fit=inside，服务端才有得可做', () => {
    const { result } = setup('42', { imageFilter: 'lanczos3', targetWidth: 1280, targetHeight: 1024 });
    expect(result.current.getImageUrlForBook('42', 7))
      .toBe('/api/pages/42/7?filter=lanczos3&w=1280&h=1024&fit=inside&format=webp&q=80');
  });

  it('原始尺寸模式（档位为 0）不发尺寸，滤镜因此退回空操作', () => {
    const { result } = setup('42', { imageFilter: 'lanczos3' });
    expect(result.current.getImageUrlForBook('42', 7)).toBe('/api/pages/42/7?filter=lanczos3&format=webp&q=80');
  });

  it('纯 CSS 的滤镜与 AI 放大那条支路都不受影响', () => {
    const css = setup('42', { imageFilter: 'bilinear', targetWidth: 1280 });
    expect(css.result.current.getImageUrlForBook('42', 7)).toBe('/api/pages/42/7?format=webp&q=80');

    const ai = setup('42', { imageFilter: 'waifu2x', targetWidth: 1280 });
    expect(ai.result.current.getImageUrlForBook('42', 7))
      .toBe('/api/pages/42/7?filter=waifu2x&w2x_scale=2&w2x_noise=1&w2x_format=png&format=webp&q=80');
  });
});

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

describe('取图参数变化时的作废', () => {
  it('参数没进请求地址就不作废：白清一轮换不来任何一个新地址', async () => {
    // 非重采样滤镜下目标尺寸不写进地址。转屏、拖窗口跨档、切适应模式都只动这两个数。
    const { result, rerender } = setupRerenderable({ imageFilter: 'none', targetWidth: 1024 });
    await act(async () => {
      void result.current.ensurePageImageLoaded('42', 1);
    });
    await settleFirst();
    expect(result.current.cachedPageImageUrls[1]).toBe('blob:mock/1');

    rerender({ imageFilter: 'none', targetWidth: 1536 });
    act(() => {
      result.current.getBookCache('42');
    });

    expect(result.current.cachedPageImageUrls[1]).toBe('blob:mock/1');
    expect(revoked).toEqual([]);
    expect(pending).toHaveLength(0);
  });

  it('另一本书的作废不得把这本在途的请求判成迟到', async () => {
    const { result, rerender } = setupRerenderable({ imageFilter: 'lanczos3', targetWidth: 1024 });
    act(() => {
      result.current.getBookCache('77');
    });
    await act(async () => {
      void result.current.ensurePageImageLoaded('42', 1);
    });

    // 拖窗口跨档，地址真的换了；随后预加载下一本会先取到那本书的缓存，把它整份作废。
    rerender({ imageFilter: 'lanczos3', targetWidth: 1536 });
    act(() => {
      result.current.getBookCache('77');
    });
    await settleFirst();

    // 代际共用一个计数器时，这里当前页的图会被当成迟到响应丢掉，而此后没有任何依赖再变化——
    // 用户看到的就是当前页永久转圈，后面几页反而有图。
    expect(result.current.cachedPageImageUrls[1]).toBe('blob:mock/1');
  });
});
