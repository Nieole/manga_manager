/**
 * @vitest-environment jsdom
 *
 * 业务说明：本文件守卫「换人之后，上一个用户的离线数据不会活到新会话里」。
 *
 * 清理此前只挂在显式登出上，而换人有四条路径，登出恰恰是共享设备上最少发生的那条
 * ——更常见的是上一个人直接关窗口。另外三条（登录/setup、刷新页面的状态探测、
 * 会话过期的 401）全都不经过登出。而新用户只要打开任意一个阅读器页面，
 * useReaderOffline 就会自动把队列里的进度同步上去：上一个人的阅读进度被写进了新账号。
 *
 * 书目索引也必须一起清：已下载的书页留在 Cache Storage 里，而 Service Worker 断网时
 * 直接从缓存命中，**不经过任何服务端鉴权**——不清索引，下一个用户就能在离线书架上
 * 看到并读完上一个人下载的书。
 */

import { afterEach, beforeEach, describe, expect, it } from 'vitest';

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
});

describe('reconcileOfflineOwner', () => {
  it('换成另一个用户时清掉上一个人的队列与书目', () => {
    localStorage.setItem(OWNER_KEY, '1');
    seedPreviousUserData();

    expect(reconcileOfflineOwner(2)).toBe(true);

    expect(JSON.parse(localStorage.getItem(PROGRESS_KEY) || '{}')).toEqual({});
    expect(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).toEqual({});
    expect(localStorage.getItem(OWNER_KEY)).toBe('2');
  });

  it('同一个用户再次登录不清自己的离线数据', () => {
    localStorage.setItem(OWNER_KEY, '1');
    seedPreviousUserData();

    expect(reconcileOfflineOwner(1)).toBe(false);

    expect(Object.keys(JSON.parse(localStorage.getItem(PROGRESS_KEY) || '{}'))).toHaveLength(1);
    expect(Object.keys(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}'))).toHaveLength(1);
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
