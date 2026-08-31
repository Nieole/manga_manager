/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「换人之后，上一个用户的离线数据不会活到新会话里」：清理必须覆盖登录/setup、
 * 刷新页面状态探测、会话过期 401、显式登出这四条换人路径，不能只挂在登出上——共享设备上
 * 登出恰恰是最少发生的那条。书目索引必须跟队列一起清：Service Worker 断网时直接从缓存命中、
 * 不经过服务端鉴权，不清索引就等于让下一个用户读到上一个人下载的书；缓存字节另做尽力清扫。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { queueOfflineProgress, reconcileOfflineOwner } from './offlineReader';

const PROGRESS_KEY = 'manga-manager:offline-progress';
const BOOKS_KEY = 'manga-manager:offline-books';
const OWNER_KEY = 'manga-manager:offline-owner';

function seedPreviousUserData() {
  queueOfflineProgress('42', 77, 'A 的书');
  localStorage.setItem(BOOKS_KEY, JSON.stringify({ '42': { bookId: '42', title: 'A 的书', pages: 10 } }));
}

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

// 记下 caches.delete 收到的缓存名：换人时清扫字节走的就是它。
function stubCacheStorage(): string[] {
  const deleted: string[] = [];
  vi.stubGlobal('caches', {
    delete: async (name: string) => {
      deleted.push(name);
      return true;
    },
  });
  return deleted;
}

describe('reconcileOfflineOwner', () => {
  it('换成另一个用户时清掉上一个人的队列与书目', () => {
    localStorage.setItem(OWNER_KEY, '1');
    seedPreviousUserData();

    expect(reconcileOfflineOwner(2)).toBe(true);

    expect(JSON.parse(localStorage.getItem(PROGRESS_KEY) || '{}')).toEqual({});
    expect(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).toEqual({});
    expect(localStorage.getItem(OWNER_KEY)).toBe('2');
  });

  it('换人时把已下载的缓存字节一并清扫掉', () => {
    // 只清索引的话，别人下载的书页仍留在 Cache Storage 里——Service Worker 的离线回退
    // 不经服务端鉴权，那些字节是这台设备上唯一还能泄露内容的东西。
    const deleted = stubCacheStorage();
    localStorage.setItem(OWNER_KEY, '1');
    seedPreviousUserData();

    expect(reconcileOfflineOwner(2)).toBe(true);

    expect(deleted).toEqual(['manga-manager-offline-books-v1']);
  });

  it('登出时同样清扫缓存字节', () => {
    const deleted = stubCacheStorage();
    localStorage.setItem(OWNER_KEY, '1');
    seedPreviousUserData();

    reconcileOfflineOwner(null);

    expect(deleted).toEqual(['manga-manager-offline-books-v1']);
  });

  it('同一个用户再次登录不清自己的离线数据', () => {
    // 每次刷新页面都会走一次对账，认成换人就等于每天把自己下载的书删一遍。
    const deleted = stubCacheStorage();
    localStorage.setItem(OWNER_KEY, '1');
    seedPreviousUserData();

    expect(reconcileOfflineOwner(1)).toBe(false);

    expect(Object.keys(JSON.parse(localStorage.getItem(PROGRESS_KEY) || '{}'))).toHaveLength(1);
    expect(Object.keys(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}'))).toHaveLength(1);
    expect(deleted).toEqual([]);
  });

  it('升级后首次登录按当前用户认领，而不是把老数据删掉', () => {
    // 没有 owner 标记时残留的归属未知。删掉的话，所有老用户升级后会平白丢一次离线书目。
    seedPreviousUserData();

    expect(reconcileOfflineOwner(1)).toBe(false);

    expect(Object.keys(JSON.parse(localStorage.getItem(PROGRESS_KEY) || '{}'))).toHaveLength(1);
    expect(localStorage.getItem(OWNER_KEY)).toBe('1');
  });

  it('登出/会话过期（userId 为空）一律清空', () => {
    localStorage.setItem(OWNER_KEY, '1');
    seedPreviousUserData();

    expect(reconcileOfflineOwner(null)).toBe(true);

    expect(JSON.parse(localStorage.getItem(PROGRESS_KEY) || '{}')).toEqual({});
    expect(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).toEqual({});
    expect(localStorage.getItem(OWNER_KEY)).toBeNull();
  });
});
