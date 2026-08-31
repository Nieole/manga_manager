/**
 * @vitest-environment jsdom
 *
 * 守「靠管理端点撑起来的页面，普通用户既看不见入口，也进不去路由」：任务与日志、设置两屏
 * 要的接口全落在后端 isAdminOnlyPath 里，漏一道守卫就是整屏 403，看着像系统坏了而不是没权限。
 * 判据必须问真的 App 路由表与真的 Layout 侧栏——复刻一份守卫逻辑来测，等于不测。
 */

import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import App from '../App';
import { AuthProvider } from './AuthProvider';
import { LocaleProvider } from '../i18n/LocaleProvider';
import { ToastProvider } from '../components/ToastProvider';
import { messages as zhCN } from '../i18n/locales/zh-CN';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() }));

// 只替掉 HTTP 那一层：守卫的判定要跑真的 AuthProvider / App / Layout，换掉它们就等于不测本 bug。
vi.mock('../api/client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    put: mocks.put,
    patch: mocks.patch,
    delete: mocks.del,
    defaults: { headers: { common: {} as Record<string, string> } },
    interceptors: { request: { use: () => 1, eject: () => {} }, response: { use: () => 1, eject: () => {} } },
  },
  isAxiosError: () => false,
  isCancel: () => false,
  getApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

// 页面本身只用来认「进没进去」，替成标记避免为它们搭各自的数据依赖。
vi.mock('../pages/Dashboard', () => ({ default: () => <div>home-page</div> }));
vi.mock('../pages/Ops', () => ({ default: () => <div>ops-page</div> }));
vi.mock('../pages/Settings', () => ({ default: () => <div>settings-page</div> }));

// jsdom 没有 EventSource；Layout 挂载即订阅 SSE，不替它整棵树都渲染不出来。
class FakeEventSource {
  static readonly CLOSED = 2;
  readyState = 0;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  close() {}
}

function statusOf(role: 'admin' | 'regular') {
  return {
    data: {
      setup_required: false,
      authenticated: true,
      csrf_token: 'csrf',
      user: { id: 1, username: role, role, display_name: role, must_change_password: false },
    },
  };
}

function mockApi(role: 'admin' | 'regular') {
  mocks.get.mockImplementation((url: string) => {
    if (url.startsWith('/api/auth/status')) return Promise.resolve(statusOf(role));
    if (url.startsWith('/api/libraries')) return Promise.resolve({ data: [] });
    return Promise.resolve({ data: {} });
  });
}

function renderApp(path: string) {
  return render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter initialEntries={[path]}>
          <AuthProvider>
            <App />
          </AuthProvider>
        </MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
}

// 侧栏入口按可点的链接找：分组标题与页面标题也可能是同一串文字，只有 <a> 才是「点得到的入口」。
function sidebarLink(label: string) {
  return screen.queryAllByRole('link', { name: new RegExp(label) });
}

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource);
  localStorage.clear();
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('管理端点撑起来的页面', () => {
  it('普通用户的侧边栏没有「任务与日志」与「系统设置」入口', async () => {
    mockApi('regular');
    renderApp('/');
    await screen.findByText('home-page');

    expect(sidebarLink('任务与日志')).toHaveLength(0);
    expect(sidebarLink('系统设置')).toHaveLength(0);
    // 「系统」分组整组消失：只藏链接的话，普通用户会剩下一个点开空无一物的分组。
    expect(screen.queryByRole('button', { name: '系统' })).toBeNull();
    // 反向判据：普通入口照旧在，别把侧栏整个关掉。
    expect(sidebarLink('概览').length).toBeGreaterThan(0);
    expect(sidebarLink('合集').length).toBeGreaterThan(0);
  });

  it('普通用户直接输 /ops 会被送回首页，并被告知这是管理员页面', async () => {
    mockApi('regular');
    renderApp('/ops');

    expect(await screen.findByText('home-page')).toBeTruthy();
    expect(screen.queryByText('ops-page')).toBeNull();
    expect(await screen.findByText(zhCN['auth.adminOnly.toast'] as string)).toBeTruthy();
  });

  it('普通用户走旧书签 /logs 也进不去任务与日志', async () => {
    mockApi('regular');
    renderApp('/logs');

    expect(await screen.findByText('home-page')).toBeTruthy();
    expect(screen.queryByText('ops-page')).toBeNull();
  });

  it('普通用户直接输 /settings 会被送回首页', async () => {
    mockApi('regular');
    renderApp('/settings');

    expect(await screen.findByText('home-page')).toBeTruthy();
    expect(screen.queryByText('settings-page')).toBeNull();
  });

  it('普通用户加载外壳时不会去问管理端点', async () => {
    mockApi('regular');
    renderApp('/');
    await screen.findByText('home-page');

    await waitFor(() => expect(mocks.get).toHaveBeenCalled());
    const adminCalls = mocks.get.mock.calls.filter(([url]) => String(url).startsWith('/api/system/'));
    expect(adminCalls).toHaveLength(0);
  });

  it('管理员一切照旧：两个入口都在，两条路由都进得去', async () => {
    mockApi('admin');
    renderApp('/ops');

    expect(await screen.findByText('ops-page')).toBeTruthy();
    expect(sidebarLink('任务与日志').length).toBeGreaterThan(0);
    expect(sidebarLink('系统设置').length).toBeGreaterThan(0);
    expect(screen.queryByRole('button', { name: '系统' })).toBeTruthy();

    cleanup();
    renderApp('/settings');
    expect(await screen.findByText('settings-page')).toBeTruthy();
  });
});
