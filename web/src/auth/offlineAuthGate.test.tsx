/**
 * @vitest-environment jsdom
 *
 * 守「断网冷启动仍能读自己下载的书」：状态探测这时必然失败，闸门若一律判成未登录，用户被送到
 * 同样需要联网的登录页，离线阅读整个特性无路可走。放行的边界也在这里守：只放行离线书架与
 * 本机已下载的那几本书，后端能应答时（含未登录与 5xx）照旧走登录页，换过用户后上一个人下载的
 * 书连路由都进不去——Service Worker 的缓存回退不经服务端鉴权，闸门是这条路上唯一的判定点。
 */

import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { AuthGate } from './AuthGate';
import { AuthProvider } from './AuthProvider';
import { reconcileOfflineOwner } from '../pages/book-reader/offlineReader';

const BOOKS_KEY = 'manga-manager:offline-books';
const OWNER_KEY = 'manga-manager:offline-owner';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));

// 只替掉 HTTP 那一层：闸门的判定要跑真的 AuthProvider + AuthGate，换掉它们就等于不测本 bug。
vi.mock('../api/client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    interceptors: { response: { use: () => 1, eject: () => {} } },
  },
  isAxiosError: (error: unknown) => Boolean(error && typeof error === 'object' && 'isAxiosError' in error),
  getApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

// 三个鉴权页只用来认「闸门把人挡下了」，替成标记避免为它们搭 i18n / 主题依赖。
vi.mock('../pages/auth/LoginPage', () => ({ default: () => <div>login-page</div> }));
vi.mock('../pages/auth/SetupPage', () => ({ default: () => <div>setup-page</div> }));
vi.mock('../pages/auth/ForcePasswordChangePage', () => ({ default: () => <div>force-password-page</div> }));

// 断网时 axios 抛的是没有 response 的错误：请求根本没走到后端。
const networkError = { isAxiosError: true, message: 'Network Error' };
// 后端应答了 5xx：能应答就不算断网，此时 Service Worker 的阅读回退也一样救不了场（它只 catch 抛错）。
const serverError = { isAxiosError: true, response: { status: 500 } };

const AUTHENTICATED = {
  data: { setup_required: false, authenticated: true, user: { id: 1, username: 'a', role: 'regular', display_name: 'A', must_change_password: false } },
};
const UNAUTHENTICATED = { data: { setup_required: false, authenticated: false } };

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AuthProvider>
        <AuthGate>
          <Routes>
            <Route path="/" element={<div>home</div>} />
            <Route path="/offline" element={<div>offline-shelf</div>} />
            <Route path="/reader/:bookId" element={<div>reader</div>} />
          </Routes>
        </AuthGate>
      </AuthProvider>
    </MemoryRouter>,
  );
}

// A 在线时把 42 号书下载到了这台设备上。
function seedDownloadedBook(ownerId: string) {
  localStorage.setItem(OWNER_KEY, ownerId);
  localStorage.setItem(BOOKS_KEY, JSON.stringify({
    '42': { bookId: '42', title: 'A 的书', pageCount: 10, cachedPages: 10, cachedAt: 'T1', imageProfile: 'webp 80', urls: [] },
  }));
}

beforeEach(() => {
  localStorage.clear();
  mocks.get.mockReset();
  mocks.post.mockReset();
});

// vitest 没开 globals，testing-library 的自动清理不会生效。
afterEach(() => {
  cleanup();
  localStorage.clear();
});

describe('断网冷启动的离线放行', () => {
  it('刷新页面时探测不到后端，离线书架照样打得开', async () => {
    seedDownloadedBook('1');
    mocks.get.mockRejectedValue(networkError);

    renderAt('/offline');

    expect(await screen.findByText('offline-shelf')).toBeTruthy();
    expect(screen.queryByText('login-page')).toBeNull();
  });

  it('自己下载过的书，断网冷启动能直接进阅读器', async () => {
    seedDownloadedBook('1');
    mocks.get.mockRejectedValue(networkError);

    renderAt('/reader/42');

    expect(await screen.findByText('reader')).toBeTruthy();
  });

  it('断网也只放行离线书架与阅读器，别的页面仍去登录页', async () => {
    // 放行的是本机已有的字节，不是一张通行证：其余页面没有后端就没有内容可看。
    seedDownloadedBook('1');
    mocks.get.mockRejectedValue(networkError);

    renderAt('/');

    expect(await screen.findByText('login-page')).toBeTruthy();
    expect(screen.queryByText('home')).toBeNull();
  });

  it('没下载过的书 id 进不去阅读器', async () => {
    // 书 id 是可枚举的小整数，放行判据必须逐本认，不能认「设备上有人下过东西」。
    seedDownloadedBook('1');
    mocks.get.mockRejectedValue(networkError);

    renderAt('/reader/99');

    expect(await screen.findByText('login-page')).toBeTruthy();
  });

  it('从没登录过的设备不给离线放行', async () => {
    mocks.get.mockRejectedValue(networkError);

    renderAt('/offline');

    expect(await screen.findByText('login-page')).toBeTruthy();
  });
});

describe('在线时的鉴权判定不因离线放行而松动', () => {
  it('后端答「未登录」时仍去登录页', async () => {
    seedDownloadedBook('1');
    mocks.get.mockResolvedValue(UNAUTHENTICATED);

    renderAt('/offline');

    expect(await screen.findByText('login-page')).toBeTruthy();
    expect(screen.queryByText('offline-shelf')).toBeNull();
  });

  it('后端 5xx 不算断网，同样去登录页', async () => {
    seedDownloadedBook('1');
    mocks.get.mockRejectedValue(serverError);

    renderAt('/reader/42');

    expect(await screen.findByText('login-page')).toBeTruthy();
    expect(screen.queryByText('reader')).toBeNull();
  });

  it('已登录用户照常进入普通页面', async () => {
    mocks.get.mockResolvedValue(AUTHENTICATED);

    renderAt('/');

    expect(await screen.findByText('home')).toBeTruthy();
  });
});

describe('换用户之后的离线隔离', () => {
  it('B 登录过之后，断网也进不去 A 下载的书', async () => {
    seedDownloadedBook('1');
    // B 在线登录：对账把 A 的书目索引清掉，owner 换成 B。
    reconcileOfflineOwner(2);
    mocks.get.mockRejectedValue(networkError);

    renderAt('/reader/42');

    expect(await screen.findByText('login-page')).toBeTruthy();
    expect(screen.queryByText('reader')).toBeNull();
  });

  it('B 登录过之后，离线书架上也不剩 A 的书', async () => {
    seedDownloadedBook('1');
    reconcileOfflineOwner(2);
    mocks.get.mockRejectedValue(networkError);

    renderAt('/offline');

    // 书架本身可以打开（B 自己下载的书要能读），但 A 的书目索引已经不在了。
    expect(await screen.findByText('offline-shelf')).toBeTruthy();
    expect(JSON.parse(localStorage.getItem(BOOKS_KEY) || '{}')).toEqual({});
  });
});
