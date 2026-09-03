/**
 * @vitest-environment jsdom
 *
 * 守 deleteOfflineBook 不拿过期快照整份覆盖书目索引：删一本书要跨好几段 await，期间别的书
 * 可能被删掉、也可能刚下载完。写回旧快照就会让删掉的书复活成一本没有字节、读不了的僵尸书，
 * 或让刚下好的书从索引里消失、字节却留在盘上永远占着配额。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  cacheBookForOffline,
  deleteOfflineBook,
  listOfflineBooks,
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
  vi.stubGlobal('caches', {
    open: async () => cache,
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
  vi.stubGlobal('fetch', async () => ({ ok: true, status: 200, clone: () => ({}) } as unknown as Response));
});

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(window.navigator, 'serviceWorker');
  localStorage.clear();
});

function optionsFor(bookId: string) {
  return {
    bookId,
    title: `书 ${bookId}`,
    pages: [1, 2],
    imageProfile: 'original',
    imageUrlForPage: (page: number) => `/api/pages/${bookId}/${page}`,
  };
}

async function seedBook(bookId: string) {
  await cacheBookForOffline(optionsFor(bookId) as never);
}

function storedIds() {
  return Object.keys(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).sort();
}

// interruptCacheKeys 让 cache.keys() 在第 at 次调用返回前插入一次并发操作，且只插一次。
// deleteOfflineBook 的兜底清扫就落在这次 keys 上，把并发操作放进去，窗口就是确定性的，
// 不必靠计时赛跑；只插一次也让并发操作自己调 keys 时不至于递归回来。
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

describe('deleteOfflineBook 的并发窗口', () => {
  // 复现 A-1：离线书架上连着点两本书的删除。两次删除各自在开头定格一份索引快照，
  // 收尾时又整份写回去——后写的那份里还带着前一本，那本书于是复活成 0%、
  // urls 与 downloadToken 俱全，点开却什么也读不到。
  it('同时删除两本书，索引里两本都不留', async () => {
    await seedBook('1');
    await seedBook('2');
    expect(storedIds()).toEqual(['1', '2']);

    await Promise.all([deleteOfflineBook('1'), deleteOfflineBook('2')]);

    expect(storedIds()).toEqual([]);
    expect(cache.entries.size).toBe(0);
  });

  // 同一个根因的另一种落点：第二次点击不在同一个微任务里，而是落在第一次删除跑到一半时。
  it('删除书 1 跑到一半才点删除书 2，书 1 不复活', async () => {
    await seedBook('1');
    await seedBook('2');
    interruptCacheKeys(1, () => deleteOfflineBook('2'));

    await deleteOfflineBook('1');

    expect(storedIds()).toEqual([]);
    expect(cache.entries.size).toBe(0);
  });

  // 复现 A-2：删除书 1 的这段 await 里，书 2 的下载正好收尾。收尾把书 2 写进索引，
  // 删除随后用开头那份还没有书 2 的快照盖回去——书 2 从书架上凭空消失，字节却留在盘上。
  it('删除书 1 的过程中书 2 下载完成，书 2 留在索引里', async () => {
    await seedBook('1');
    interruptCacheKeys(1, () => seedBook('2'));

    await deleteOfflineBook('1');

    expect(storedIds()).toEqual(['2']);
    const listed = await listOfflineBooks();
    expect(listed.map((book) => book.bookId)).toEqual(['2']);
    expect(listed[0].cachedPages).toBe(2);
  });

  // 反向判据：没有并发时删除照旧把这本书连字节带索引一起清干净，别的书一动不动。
  it('单本删除清干净自己，不碰别的书', async () => {
    await seedBook('1');
    await seedBook('2');

    await deleteOfflineBook('1');

    expect(storedIds()).toEqual(['2']);
    const paths = Array.from(cache.entries.keys()).map((url) => new URL(url).pathname);
    expect(paths.some((path) => path.startsWith('/api/pages/1'))).toBe(false);
    expect(paths).toContain('/api/pages/2');
    expect((await listOfflineBooks())[0].cachedPages).toBe(2);
  });

  // 锁住「先删字节、后摘索引」这个顺序：反过来的话，字节删到一半失败就留下索引说没有、
  // 盘上还在的孤儿——那时这本书在书架上已经看不见，单本删除再也够不着它。
  it('删字节失败时这本书仍留在索引里，用户能再删一次', async () => {
    await seedBook('1');
    cache.delete = async () => {
      throw new Error('quota exceeded');
    };

    await expect(deleteOfflineBook('1')).rejects.toThrow();

    expect(storedIds()).toEqual(['1']);
  });
});
