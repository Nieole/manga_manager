/**
 * @vitest-environment jsdom
 *
 * 守「没有当前用户」的三种情形各自清什么：会话过期/被踢与断网都只是同一个人要重新登录，
 * 本机离线书目、缓存字节与尚未回传的进度队列必须原样留着；只有用户显式登出、或换成另一个
 * 用户登录才清。反向判据（换人照旧清干净）与登出前先尽力回传队列一并守在这里。
 */

import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { AuthProvider, useAuth } from './AuthProvider';
import { isOfflineReadableRoute } from './offlineAccess';
import { queueOfflineProgress } from '../pages/book-reader/offlineReader';

const PROGRESS_KEY = 'manga-manager:offline-progress';
const BOOKS_KEY = 'manga-manager:offline-books';
const OWNER_KEY = 'manga-manager:offline-owner';
const BOOK_CACHE = 'manga-manager-offline-books-v1';

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  // 401 兜底挂在响应拦截器上：接住它才能在测试里模拟「后端拒绝了这次会话」。
  rejected: { current: null as null | ((error: unknown) => unknown) },
}));

vi.mock('../api/client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    interceptors: {
      response: {
        use: (_ok: unknown, onRejected: (error: unknown) => unknown) => {
          mocks.rejected.current = onRejected;
          return 1;
        },
        eject: () => {},
      },
    },
  },
  isAxiosError: (error: unknown) => Boolean(error && typeof error === 'object' && 'isAxiosError' in error),
  getApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

const networkError = { isAxiosError: true, message: 'Network Error' };
const unauthorized = { isAxiosError: true, response: { status: 401 }, config: { url: '/api/books/1/progress' } };

const UNAUTHENTICATED = { data: { setup_required: false, authenticated: false } };
const AUTHENTICATED_A = {
  data: {
    setup_required: false,
    authenticated: true,
    user: { id: 1, username: 'a', role: 'regular', display_name: 'A', must_change_password: false },
  },
};
const SESSION_B = {
  data: {
    csrf_token: 'csrf-b',
    user: { id: 2, username: 'b', role: 'regular', display_name: 'B', must_change_password: false },
  },
};

// A 在这台设备上下载了 42 号书，并在断网时读到了第 77 页——那一页还没回传。
function seedOwnedOfflineData() {
  localStorage.setItem(OWNER_KEY, '1');
  localStorage.setItem(BOOKS_KEY, JSON.stringify({
    '42': { bookId: '42', title: '飞机上读的那本', pageCount: 120, cachedPages: 120, cachedAt: 'T1', imageProfile: 'webp 80', urls: [] },
  }));
  queueOfflineProgress('42', 77, '飞机上读的那本');
}

function readBooks() {
  return JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}') as Record<string, unknown>;
}

function readQueue() {
  return JSON.parse(localStorage.getItem(PROGRESS_KEY) || '{}') as Record<string, { page: number }>;
}

// 记下 caches.delete 收到的缓存名：清扫离线字节走的就是它。
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

function Probe() {
  const { user, loading, login, logout } = useAuth();
  return (
    <div>
      <span data-testid="state">{loading ? 'loading' : user ? user.username : 'anonymous'}</span>
      <button type="button" onClick={() => { void logout(); }}>logout</button>
      <button type="button" onClick={() => { void login('b', 'pw'); }}>login-b</button>
    </div>
  );
}

async function renderProvider() {
  render(<AuthProvider><Probe /></AuthProvider>);
  await waitFor(() => expect(screen.getByTestId('state').textContent).not.toBe('loading'));
}

beforeEach(() => {
  localStorage.clear();
  mocks.get.mockReset();
  mocks.post.mockReset();
  mocks.rejected.current = null;
});

afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe('会话过期不是换人', () => {
  it('后端答「未登录」后，离线书目、缓存字节与进度队列原样还在', async () => {
    // 30 天到期、管理员重置密码、别的设备改了密码，都从这条路回来。用户还是同一个人，
    // 清掉等于每次例行过期都把他自己下载的书和还没回传的进度删一遍。
    const deleted = stubCacheStorage();
    seedOwnedOfflineData();
    mocks.get.mockResolvedValue(UNAUTHENTICATED);

    await renderProvider();

    expect(screen.getByTestId('state').textContent).toBe('anonymous');
    expect(Object.keys(readBooks())).toEqual(['42']);
    expect(readQueue()['42'].page).toBe(77);
    expect(localStorage.getItem(OWNER_KEY)).toBe('1');
    expect(deleted).toEqual([]);
  });

  it('任意请求收到 401 后，同样不动本机的离线数据', async () => {
    const deleted = stubCacheStorage();
    seedOwnedOfflineData();
    mocks.get.mockResolvedValue(AUTHENTICATED_A);

    await renderProvider();
    expect(screen.getByTestId('state').textContent).toBe('a');

    await act(async () => {
      await mocks.rejected.current?.(unauthorized).catch(() => {});
    });

    // 登录态照旧清空，回到登录页；离线数据不跟着陪葬。
    expect(screen.getByTestId('state').textContent).toBe('anonymous');
    expect(Object.keys(readBooks())).toEqual(['42']);
    expect(readQueue()['42'].page).toBe(77);
    expect(localStorage.getItem(OWNER_KEY)).toBe('1');
    expect(deleted).toEqual([]);
  });

  it('会话过期后再断网，本人仍进得去自己下载的那本书', async () => {
    // 放行判据本来就不看会话是否有效，只看「后端够不到 + 本机 owner + 索引里有这本书」。
    // 会话过期前它同样放行，所以保住 owner 标记不打开新缺口，只是不再把数据一起毁掉。
    stubCacheStorage();
    seedOwnedOfflineData();
    mocks.get.mockResolvedValue(UNAUTHENTICATED);

    await renderProvider();

    expect(isOfflineReadableRoute('/reader/42')).toBe(true);
    expect(isOfflineReadableRoute('/reader/99')).toBe(false);
  });
});

describe('断网时的既有行为不退化', () => {
  it('状态探测连不上后端时，本机离线数据一样不动', async () => {
    const deleted = stubCacheStorage();
    seedOwnedOfflineData();
    mocks.get.mockRejectedValue(networkError);

    await renderProvider();

    expect(Object.keys(readBooks())).toEqual(['42']);
    expect(readQueue()['42'].page).toBe(77);
    expect(localStorage.getItem(OWNER_KEY)).toBe('1');
    expect(deleted).toEqual([]);
  });
});

describe('显式登出交还这台设备', () => {
  it('先把队列回传上去，再清掉书目、字节与队列', async () => {
    // 队列是用户的劳动成果，登出这一刻会话还有效，是最后一次能传上去的机会。
    const order: string[] = [];
    const deleted = stubCacheStorage();
    vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
      order.push(`sync:${String(input)}`);
      return {
        ok: true,
        json: async () => ({ results: [{ book_id: 42, status: 'updated' }] }),
      } as unknown as Response;
    }));
    seedOwnedOfflineData();
    mocks.get.mockResolvedValue(AUTHENTICATED_A);
    mocks.post.mockImplementation(async (url: string) => {
      order.push(`post:${url}`);
      return { data: {} };
    });

    await renderProvider();
    fireEvent.click(screen.getByText('logout'));

    await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('anonymous'));
    await waitFor(() => expect(localStorage.getItem(OWNER_KEY)).toBeNull());

    expect(order).toEqual(['sync:/api/books/bulk-progress/sync', 'post:/api/auth/logout']);
    expect(readBooks()).toEqual({});
    expect(readQueue()).toEqual({});
    expect(deleted).toEqual([BOOK_CACHE]);
  });
});

describe('真正换用户时仍然清干净', () => {
  it('B 登录后，A 的书目、字节与队列都不在了', async () => {
    // 隔离的判定点是「谁登进来了」，不是「此刻没人登录」——这条不能因为上面的放宽而松动。
    const deleted = stubCacheStorage();
    seedOwnedOfflineData();
    mocks.get.mockResolvedValue(UNAUTHENTICATED);
    mocks.post.mockResolvedValue(SESSION_B);

    await renderProvider();
    fireEvent.click(screen.getByText('login-b'));

    await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('b'));

    expect(readBooks()).toEqual({});
    expect(readQueue()).toEqual({});
    expect(localStorage.getItem(OWNER_KEY)).toBe('2');
    expect(deleted).toEqual([BOOK_CACHE]);
  });
});
