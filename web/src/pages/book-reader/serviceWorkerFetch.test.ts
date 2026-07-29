/**
 * 业务说明：本文件守卫 Service Worker 的路由与缓存策略。
 *
 * sw.js 决定「哪些请求断网时还能读」，而它踩错任何一条都不会报错、只会静默改变行为：
 * 把实时接口（任务、设置、SSE）缓存进去 → 用户看到过期状态；把阅读请求的策略搞反 →
 * 已下载的书离线打不开；激活时的旧缓存清理漏掉离线书缓存 → 一次版本升级把用户
 * 下载的整本书全删了。
 *
 * sw.js 是经典脚本、没有 export，import 不进来。但它顶层只做三次 self.addEventListener，
 * 所以注入 self/caches/fetch 三个全局执行一遍，就能把监听器逐个捞出来驱动。
 */

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const SW_SOURCE = readFileSync(fileURLToPath(new URL('../../../public/sw.js', import.meta.url)), 'utf8');

const STATIC_CACHE = 'manga-manager-v3';
const OFFLINE_CACHE = 'manga-manager-offline-books-v1';

class FakeCache {
  entries = new Map<string, unknown>();

  async addAll(urls: string[]) {
    for (const url of urls) this.entries.set(new URL(url, 'https://app.test').toString(), {});
  }

  async put(request: Request, response: unknown) {
    this.entries.set(request.url, response);
  }

  // match 支持 ignoreSearch：这正是页图「换了画质仍能离线读」所依赖的那一条。
  async match(request: Request | string, options?: { ignoreSearch?: boolean }) {
    const url = typeof request === 'string' ? request : request.url;
    if (this.entries.has(url)) return this.entries.get(url);
    if (!options?.ignoreSearch) return undefined;
    const path = new URL(url).pathname;
    for (const [key, value] of this.entries) {
      if (new URL(key).pathname === path) return value;
    }
    return undefined;
  }
}

interface SWHarness {
  listeners: Record<string, (event: unknown) => void>;
  caches: Map<string, FakeCache>;
  deletedCaches: string[];
  fetchMock: ReturnType<typeof vi.fn>;
}

function loadServiceWorker(): SWHarness {
  const listeners: Record<string, (event: unknown) => void> = {};
  const cacheStore = new Map<string, FakeCache>();
  const deletedCaches: string[] = [];

  const fakeCaches = {
    open: async (name: string) => {
      if (!cacheStore.has(name)) cacheStore.set(name, new FakeCache());
      return cacheStore.get(name)!;
    },
    keys: async () => Array.from(cacheStore.keys()),
    delete: async (name: string) => {
      deletedCaches.push(name);
      return cacheStore.delete(name);
    },
    match: async (request: Request | string, options?: { ignoreSearch?: boolean }) => {
      for (const cache of cacheStore.values()) {
        const hit = await cache.match(request, options);
        if (hit) return hit;
      }
      return undefined;
    },
  };

  const fakeSelf = {
    addEventListener: (type: string, handler: (event: unknown) => void) => {
      listeners[type] = handler;
    },
    skipWaiting: () => {},
    clients: { claim: () => {} },
    location: { origin: 'https://app.test' },
  };

  const fetchMock = vi.fn();
  new Function('self', 'caches', 'fetch', SW_SOURCE)(fakeSelf, fakeCaches, fetchMock);
  return { listeners, caches: cacheStore, deletedCaches, fetchMock };
}

// FakeFetchEvent 把 respondWith 收到的 promise 存下来；没被接管时它保持 null。
class FakeFetchEvent {
  responded: Promise<unknown> | null = null;
  waited: Promise<unknown> | null = null;

  constructor(public request: Request) {}

  respondWith(promise: Promise<unknown>) {
    this.responded = promise;
  }

  waitUntil(promise: Promise<unknown>) {
    this.waited = promise;
  }
}

function req(url: string, method = 'GET') {
  return new Request(url, { method });
}

let sw: SWHarness;

beforeEach(() => {
  sw = loadServiceWorker();
});

async function dispatchFetch(request: Request) {
  const event = new FakeFetchEvent(request);
  sw.listeners.fetch(event);
  return event;
}

describe('Service Worker 的接管范围', () => {
  it('实时接口一律不接管', async () => {
    // 任务、设置、SSE 缓存进去就是给用户看过期状态。
    for (const url of [
      'https://app.test/api/events',
      'https://app.test/api/tasks',
      'https://app.test/api/system/config',
    ]) {
      const event = await dispatchFetch(req(url));
      expect(event.responded, `${url} 不该被接管`).toBeNull();
    }
  });

  it('非 GET 与跨源请求不接管', async () => {
    expect((await dispatchFetch(req('https://app.test/api/pages/42/1', 'POST'))).responded).toBeNull();
    expect((await dispatchFetch(req('https://cdn.other/asset.js'))).responded).toBeNull();
  });

  it('阅读类请求断网时回落到离线书缓存', async () => {
    const offline = await (await loadOffline()).open();
    await offline.put(req('https://app.test/api/pages/42/7?format=webp&q=80'), 'cached-bytes');
    sw.fetchMock.mockRejectedValue(new Error('offline'));

    // 用户改过画质偏好之后重建的 URL 与缓存里的 query 不同，
    // 必须按页路径忽略 query 命中——否则离线读图会静默失败。
    const event = await dispatchFetch(req('https://app.test/api/pages/42/7?format=avif&q=60'));
    await expect(event.responded).resolves.toBe('cached-bytes');
  });

  it('book-info 只做精确匹配，不放宽 query', async () => {
    const offline = await (await loadOffline()).open();
    await offline.put(req('https://app.test/api/book-info/42?v=1'), 'cached-info');
    sw.fetchMock.mockRejectedValue(new Error('offline'));

    const event = await dispatchFetch(req('https://app.test/api/book-info/42?v=2'));
    await expect(event.responded).resolves.toBeUndefined();
  });

  it('阅读请求即使联网成功也不写缓存', async () => {
    // 只有用户显式「下载离线」才写离线书缓存；顺手缓存会让磁盘悄悄涨。
    sw.fetchMock.mockResolvedValue({ ok: true, clone: () => ({}) });

    const event = await dispatchFetch(req('https://app.test/api/pages/42/1'));
    await event.responded;

    for (const cache of sw.caches.values()) {
      expect(cache.entries.size).toBe(0);
    }
  });

  it('静态资产网络优先并回写静态缓存', async () => {
    sw.fetchMock.mockResolvedValue({ ok: true, clone: () => 'cloned' });

    const event = await dispatchFetch(req('https://app.test/assets/app.js'));
    await event.responded;
    // put 是在 then 里异步做的，让出一轮微任务。
    await Promise.resolve();

    expect(sw.caches.get(STATIC_CACHE)?.entries.has('https://app.test/assets/app.js')).toBe(true);
  });

  async function loadOffline() {
    return {
      open: async () => {
        const cache = new FakeCache();
        sw.caches.set(OFFLINE_CACHE, cache);
        return cache;
      },
    };
  }
});

describe('Service Worker 激活时的旧缓存清理', () => {
  it('清掉旧版本静态缓存，但绝不碰离线书缓存', async () => {
    // 这是全文件风险最高的一行：升 CACHE_NAME 时若把 OFFLINE_BOOK_CACHE 从白名单里
    // 漏掉，一次版本发布就会把所有用户已下载的书全部删光，而且无法恢复。
    sw.caches.set('manga-manager-v2', new FakeCache());
    sw.caches.set(STATIC_CACHE, new FakeCache());
    sw.caches.set(OFFLINE_CACHE, new FakeCache());

    const event = new FakeFetchEvent(req('https://app.test/'));
    sw.listeners.activate(event);
    await event.waited;

    expect(sw.deletedCaches).toContain('manga-manager-v2');
    expect(sw.deletedCaches).not.toContain(OFFLINE_CACHE);
    expect(sw.deletedCaches).not.toContain(STATIC_CACHE);
  });
});
