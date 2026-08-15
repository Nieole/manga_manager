/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「离线下载中途失败不会留下删不掉的孤儿缓存」：书目索引必须随每次成功
 * fetch 同步写入，不能等整个循环跑完才写，否则断网时已下载的页留在 Cache Storage 里
 * 但索引查不到，离线书架按索引渲染，这本书就从界面消失，也没有入口能单独删除那些字节。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { cacheBookForOffline, deleteOfflineBook, listOfflineBooks } from './offlineReader';

const BOOKS_KEY = 'manga-manager:offline-books';

// FakeCache 复刻我们用到的那一小块 Cache Storage 契约。
class FakeCache {
  entries = new Map<string, unknown>();

  async put(request: Request, response: unknown) {
    this.entries.set(request.url, response);
  }

  async keys() {
    return Array.from(this.entries.keys()).map((url) => new Request(url));
  }

  async delete(request: Request | string) {
    const url = typeof request === 'string' ? request : request.url;
    return this.entries.delete(url);
  }
}

let cache: FakeCache;

beforeEach(() => {
  localStorage.clear();
  cache = new FakeCache();
  // supportsOfflineReaderCache 同时看 window.caches 与 navigator.serviceWorker，两者都要备齐。
  vi.stubGlobal('caches', {
    open: async () => cache,
    delete: async () => true,
    has: async () => true,
    keys: async () => [],
    match: async () => undefined,
  });
  Object.defineProperty(window.navigator, 'serviceWorker', {
    configurable: true,
    value: { register: async () => undefined },
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(window.navigator, 'serviceWorker');
  localStorage.clear();
});

// stubFetch 让前 okCount 个请求成功、之后一律失败。
function stubFetch(okCount: number) {
  let seen = 0;
  vi.stubGlobal('fetch', async () => {
    seen += 1;
    if (seen > okCount) return { ok: false, status: 503, clone: () => ({}) } as unknown as Response;
    return { ok: true, status: 200, clone: () => ({}) } as unknown as Response;
  });
}

const OPTIONS = {
  bookId: '42',
  title: '测试书',
  pages: [1, 2, 3, 4, 5],
  imageProfile: 'original',
  imageUrlForPage: (page: number) => `/api/pages/42/${page}`,
};

describe('cacheBookForOffline', () => {
  it('全部成功时记录完整的书目', async () => {
    stubFetch(Infinity);

    const status = await cacheBookForOffline(OPTIONS);

    expect(status.pageCount).toBe(5);
    expect(status.cachedPages).toBe(5);
    const listed = await listOfflineBooks();
    expect(listed.map((b) => b.bookId)).toEqual(['42']);
  });

  it('中途失败后这本书仍出现在离线书架上，且标明只下了一部分', async () => {
    // 3 个静态 URL + 2 页之后开始失败。
    stubFetch(5);

    await expect(cacheBookForOffline(OPTIONS)).rejects.toThrow();

    const listed = await listOfflineBooks();
    expect(listed.map((b) => b.bookId)).toEqual(['42']);
    // 状态里的已缓存页数是**实时数**（按 Cache Storage 里的条目数），
    // 所以界面上会显示「2/5」而不是假装完成。
    expect(listed[0].cachedPages).toBe(2);
    expect(listed[0].pageCount).toBe(5);
  });

  // 护栏而非红/绿判据：deleteOfflineBook 的路径前缀兜底让这条用例在修复前也是绿的；
  // 它锁的是「删除要连字节一起清干净」这条约束，不负责证明书在界面上看得见。
  it('中途失败留下的残片可以被单独删除且不留字节', async () => {
    stubFetch(5);
    await expect(cacheBookForOffline(OPTIONS)).rejects.toThrow();
    expect(cache.entries.size).toBeGreaterThan(0);

    await deleteOfflineBook('42');

    expect(await listOfflineBooks()).toEqual([]);
    // 缓存里也不该再有这本书的字节——否则用户的磁盘被永久占用，
    // 而界面上已经看不到这本书了。
    expect(cache.entries.size).toBe(0);
  });

  it('第一个请求就失败时不留下空壳条目之外的东西', async () => {
    stubFetch(0);

    await expect(cacheBookForOffline(OPTIONS)).rejects.toThrow();

    const listed = await listOfflineBooks();
    expect(listed[0].cachedPages).toBe(0);
    await deleteOfflineBook('42');
    expect(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).toEqual({});
  });
});
