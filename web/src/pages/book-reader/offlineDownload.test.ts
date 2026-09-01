/**
 * @vitest-environment jsdom
 *
 * 本文件守卫离线下载的收尾：索引随下载开始就写、收尾前先确认自己没被删除或换用户作废、
 * 页图按不带 query 的页路径落盘。破了都不报错，只留下删不掉的孤儿字节、删了又复活却
 * 读不了的书、以及改一次画质就翻一倍的计数与磁盘占用。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  cacheBookForOffline,
  deleteAllOfflineBooks,
  deleteOfflineBook,
  listOfflineBooks,
  reconcileOfflineOwner,
} from './offlineReader';

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
    // 真的清空字节：换用户与下载收尾并发时，这一条决定了残片看不看得见。
    delete: async () => {
      cache.entries.clear();
      return true;
    },
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

// interruptFetch 让每个请求都成功，并在第 at 个请求返回前插入一次并发操作。
// 第 1–3 个是静态 URL（页清单、书籍信息、阅读器壳），之后才是逐页图。
function interruptFetch(at: number, interrupt: () => Promise<void> | void) {
  let seen = 0;
  vi.stubGlobal('fetch', async () => {
    seen += 1;
    if (seen === at) await interrupt();
    return { ok: true, status: 200, clone: () => ({}) } as unknown as Response;
  });
}

// interruptCacheKeys 让 cache.keys() 在第 at 次调用返回前插入一次并发操作，且只插一次。
// 收尾阶段的两次 keys 分别是清扫旧键与读回状态，把删除放进这两段 await 里，作废窗口就是
// 确定性的，不必靠计时赛跑；只插一次也让并发操作自己调 keys 时不至于递归回来。
function interruptCacheKeys(at: number, interrupt: () => Promise<void> | void) {
  const original = cache.keys.bind(cache);
  let seen = 0;
  let fired = false;
  cache.keys = async () => {
    seen += 1;
    if (seen === at && !fired) {
      fired = true;
      await interrupt();
    }
    return original();
  };
}

const OPTIONS = {
  bookId: '42',
  title: '测试书',
  pages: [1, 2, 3, 4, 5],
  imageProfile: 'original',
  imageUrlForPage: (page: number) => `/api/pages/42/${page}`,
};

// 用户改过画质后重下的同一本书：路径一样，只是多了 query。
const WEBP_OPTIONS = {
  ...OPTIONS,
  imageProfile: 'WEBP 80',
  imageUrlForPage: (page: number) => `/api/pages/42/${page}?format=webp&q=80`,
};

const pageKeys = () => Array.from(cache.entries.keys()).filter((url) => url.includes('/api/pages/42/'));

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

describe('下载收尾的作废检查', () => {
  // 复现：已下载过的书改了画质再次「缓存本书」，下载进行中点删除。收尾若照旧写回索引，
  // 用户删掉的书会复活成 100%，而删除时清掉的页清单与书籍信息不会再下一遍——
  // 这本「已缓存」的书离线打开时拿不到页清单，读不了。
  it('下载途中删除这本书，收尾不把它写回索引', async () => {
    interruptFetch(5, () => deleteOfflineBook('42'));

    const status = await cacheBookForOffline(OPTIONS);

    expect(status).toBeNull();
    expect(await listOfflineBooks()).toEqual([]);
    expect(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).toEqual({});
  });

  it('下载途中删除这本书，已落盘与后续写入的字节都不留残片', async () => {
    interruptFetch(5, () => deleteOfflineBook('42'));

    await cacheBookForOffline(OPTIONS);

    // 索引里已经没有这本书了，字节再留着就是永远删不掉的占用。
    expect(cache.entries.size).toBe(0);
  });

  // 换用户与下载收尾并发：reconcileOfflineOwner 清空索引并清扫字节之后，
  // 在飞的下载不能把上一个用户的书重新写回这台设备。
  it('下载途中换用户清空索引，收尾同样不复活这本书', async () => {
    localStorage.setItem('manga-manager:offline-owner', '1');
    interruptFetch(5, () => {
      reconcileOfflineOwner(2);
    });

    const status = await cacheBookForOffline(OPTIONS);

    expect(status).toBeNull();
    expect(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).toEqual({});
    expect(cache.entries.size).toBe(0);
  });

  it('没有并发删除时正常收尾，书完整地留在书架上', async () => {
    stubFetch(Infinity);

    const status = await cacheBookForOffline(OPTIONS);

    expect(status?.cachedPages).toBe(5);
    expect((await listOfflineBooks()).map((b) => b.bookId)).toEqual(['42']);
    // 页清单、书籍信息、阅读器壳一个都不能少，否则离线打开时读不了。
    const keys = Array.from(cache.entries.keys()).map((url) => new URL(url).pathname);
    expect(keys).toContain('/api/pages/42');
    expect(keys).toContain('/api/book-info/42');
    expect(keys).toContain('/reader/42');
  });
});

describe('改画质重下同一本书', () => {
  // 复现：两套 URL 路径相同、query 不同，都算进同一本书的已缓存页数 → 进度条 200%。
  // 而 Service Worker 读图对 /api/pages/ 忽略 query，第二套字节根本读不到，纯浪费配额。
  it('重下不重复计数，也不翻倍占磁盘', async () => {
    stubFetch(Infinity);
    const first = await cacheBookForOffline(OPTIONS);
    expect(first?.cachedPages).toBe(5);
    const sizeAfterFirst = cache.entries.size;

    const second = await cacheBookForOffline(WEBP_OPTIONS);

    expect(second?.cachedPages).toBe(5);
    expect(second?.imageProfile).toBe('WEBP 80');
    expect(cache.entries.size).toBe(sizeAfterFirst);
  });

  it('页图按不带 query 的页路径落盘，与画质偏好解耦', async () => {
    stubFetch(Infinity);

    await cacheBookForOffline(WEBP_OPTIONS);

    // 缓存键不带 query；带 query 的地址只用于发请求，好让用户拿到自己选的画质。
    expect(pageKeys().map((url) => new URL(url).pathname).sort()).toEqual([
      '/api/pages/42/1',
      '/api/pages/42/2',
      '/api/pages/42/3',
      '/api/pages/42/4',
      '/api/pages/42/5',
    ]);
    expect(pageKeys().some((url) => url.includes('?'))).toBe(false);
  });

  it('重下按用户选的画质发请求，落盘的是新画质的字节', async () => {
    const requested: string[] = [];
    vi.stubGlobal('fetch', async (input: Request | string) => {
      requested.push(typeof input === 'string' ? input : input.url);
      return { ok: true, status: 200, clone: () => ({}) } as unknown as Response;
    });

    await cacheBookForOffline(WEBP_OPTIONS);

    expect(requested.filter((url) => url.includes('format=webp&q=80'))).toHaveLength(5);
  });

  it('重下清掉上一版遗留的带 query 页字节', async () => {
    // 修复前下载的书在缓存里留着带 query 的键，光靠覆盖写清不掉，得靠重下时的清扫。
    await cache.put(new Request('https://x.test/api/pages/42/1?format=webp&q=80'), {});
    stubFetch(Infinity);

    const status = await cacheBookForOffline(OPTIONS);

    expect(status?.cachedPages).toBe(5);
    expect(pageKeys().some((url) => url.includes('?'))).toBe(false);
  });
});

describe('收尾阶段的作废窗口', () => {
  // 复现：收尾先清扫旧键（一整段 await），再无条件把索引写回去。用户在这段 await 里删掉
  // 这本书，那句写回就把整条记录连同 urls 与令牌一起复活——字节已经被清掉了，
  // 书架上于是留下一本看着已下好、点开却读不到的僵尸书。
  it('清扫旧键期间删除这本书，索引里不复活', async () => {
    stubFetch(Infinity);
    interruptCacheKeys(1, () => deleteOfflineBook('42'));

    const status = await cacheBookForOffline(OPTIONS);

    expect(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).toEqual({});
    expect(await listOfflineBooks()).toEqual([]);
    expect(status).toBeNull();
    expect(cache.entries.size).toBe(0);
  });

  // 清空全部同样落在这个窗口里：deleteAllOfflineBooks 把索引写成 {} 之后，
  // 收尾那句照样往里塞回一条。
  it('清扫旧键期间清空全部离线书，索引里同样不复活', async () => {
    stubFetch(Infinity);
    interruptCacheKeys(1, () => deleteAllOfflineBooks());

    const status = await cacheBookForOffline(OPTIONS);

    expect(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).toEqual({});
    expect(status).toBeNull();
    expect(cache.entries.size).toBe(0);
  });

  // 写回之后还有一段 await：读回状态。这中间被删掉的话，索引里已经没有这本书了，
  // 再把状态交出去，阅读器就会照它把这本书显示成还在本机。
  it('读回状态期间删除这本书，不把它报成一本还在本机的书', async () => {
    stubFetch(Infinity);
    interruptCacheKeys(2, () => deleteOfflineBook('42'));

    const status = await cacheBookForOffline(OPTIONS);

    expect(status).toBeNull();
    expect(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).toEqual({});
  });

  // 清扫本身也在窗口里：它按本次下载的键列表保留、删掉这本书名下的其余键。但页图的缓存键
  // 经 pageImageCacheKey 去掉了 query，而画质、格式、滤镜、自动裁切全都只落在 query 上
  // （见 getImageUrlForBook），所以换个画质重下同一本书得到的是同一批键——被作废的那次
  // 清扫保留的正是新下载写下的那些，挖不走新下载的页。
  it('清扫不会挖走并发重下（换画质）刚写下的页', async () => {
    stubFetch(Infinity);
    interruptCacheKeys(1, async () => {
      await deleteOfflineBook('42');
      await cacheBookForOffline(WEBP_OPTIONS);
    });

    const stale = await cacheBookForOffline(OPTIONS);

    expect(stale).toBeNull();
    const stored = JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}');
    expect(stored['42'].imageProfile).toBe('WEBP 80');
    expect((await listOfflineBooks())[0].cachedPages).toBe(5);
    expect(cache.entries.size).toBe(8);
  });

  // 反向判据：没有并发删除时，收尾照旧把计数补齐，书完整地落库并出现在书架上。
  it('没有并发删除时收尾照常落库，书完整地出现在书架上', async () => {
    stubFetch(Infinity);

    const status = await cacheBookForOffline(OPTIONS);

    expect(status?.cachedPages).toBe(5);
    const stored = JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}');
    expect(stored['42'].cachedPages).toBe(5);
    expect(stored['42'].urls).toHaveLength(8);
    expect((await listOfflineBooks()).map((b) => b.bookId)).toEqual(['42']);
  });
});
