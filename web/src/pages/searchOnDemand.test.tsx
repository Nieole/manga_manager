/**
 * @vitest-environment jsdom
 *
 * 守任务中心与日志页的搜索框「打完再查」：两处都摆了回车与查询/刷新按钮，逐字符发请求既是
 * 白打后端，也让轮询定时器随 fetch 身份反复重建、打字期间永远不到点。再守世代号：慢网下先发
 * 的响应后到，不能盖掉按新关键词取回的那一份——否则框里写着 scan，列出来的是 sca 的结果。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import { LocaleProvider } from '../i18n/LocaleProvider';
import { ToastProvider } from '../components/ToastProvider';
import { messages as zhCN } from '../i18n/locales/zh-CN';
import BackgroundTasks from './BackgroundTasks';
import Logs from './Logs';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), del: vi.fn() }));

vi.mock('../api/client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    delete: mocks.del,
    defaults: { headers: { common: {} as Record<string, string> } },
  },
  isAxiosError: () => false,
  isCancel: () => false,
  getApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

// 任务清单一行：搜索框筛的就是它，行上那句文案取自这个任务最近一次运行。
function taskSummary(taskId: number, message: string) {
  return {
    task_id: taskId,
    type: 'scan_library',
    scope: 'library',
    scope_id: 1,
    scope_name: '主库',
    disabled: false,
    fail_streak: 0,
    last_run: {
      run_id: taskId,
      task_id: taskId,
      key: `scan_library_${taskId}`,
      type: 'scan_library',
      scope: 'library',
      scope_id: 1,
      scope_name: '主库',
      status: 'completed',
      message,
      error: '',
      current: 1,
      total: 1,
      can_cancel: false,
      can_pause: false,
      can_resume: false,
      retryable: false,
      started_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
  };
}

const emptyLive = { active: 0, queued: 0, slots: 0, paused: false, runs: [] };

// 搜索框驱动的是任务清单那条取数：实况帧不带筛选，因此不该数进来。
function taskRequests() {
  return mocks.get.mock.calls
    .map((call) => String(call[0]))
    .filter((url) => url.startsWith('/api/system/tasks/summary'));
}

function logRequests() {
  return mocks.get.mock.calls
    .map((call) => String(call[0]))
    .filter((url) => url.startsWith('/api/system/logs'));
}

function wrap(node: React.ReactNode) {
  return render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter>{node}</MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

describe('任务中心的搜索框', () => {
  it('逐字符打字不发请求，回车才发一次并带上关键词', async () => {
    mocks.get.mockImplementation((url: string) => {
      if (url.startsWith('/api/system/tasks/live')) return Promise.resolve({ data: emptyLive });
      if (url.startsWith('/api/system/tasks')) return Promise.resolve({ data: [] });
      return Promise.resolve({ data: null });
    });

    wrap(<BackgroundTasks />);
    await waitFor(() => expect(taskRequests()).toHaveLength(1));

    const box = screen.getByPlaceholderText('搜索任务');
    for (const value of ['s', 'sc', 'sca', 'scan']) {
      fireEvent.change(box, { target: { value } });
    }
    await Promise.resolve();
    expect(taskRequests()).toHaveLength(1);

    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(taskRequests()).toHaveLength(2));
    expect(taskRequests()[1]).toContain('q=scan');
  });

  it('慢网下先提交的关键词响应后到，不会盖掉后提交那一次的结果', async () => {
    let releaseSca: (() => void) | null = null;
    mocks.get.mockImplementation((url: string) => {
      if (url.startsWith('/api/system/tasks/live')) return Promise.resolve({ data: emptyLive });
      if (url.startsWith('/api/system/tasks')) {
        if (url.includes('q=sca&') || url.endsWith('q=sca')) {
          return new Promise((resolve) => {
            releaseSca = () => resolve({ data: [taskSummary(1, '过期的 sca 结果')] });
          });
        }
        if (url.includes('q=scan')) return Promise.resolve({ data: [taskSummary(2, '最新的 scan 结果')] });
        return Promise.resolve({ data: [] });
      }
      return Promise.resolve({ data: null });
    });

    wrap(<BackgroundTasks />);
    const box = await screen.findByPlaceholderText('搜索任务');

    fireEvent.change(box, { target: { value: 'sca' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(releaseSca).not.toBeNull());

    fireEvent.change(box, { target: { value: 'scan' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(screen.getByText('最新的 scan 结果')).toBeTruthy());

    releaseSca!();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByText('过期的 sca 结果')).toBeNull();
    expect(screen.getByText('最新的 scan 结果')).toBeTruthy();
  });
});

describe('日志页的搜索框', () => {
  it('逐字符打字不发请求，回车才发一次并带上关键词', async () => {
    mocks.get.mockImplementation((url: string) => {
      if (url.startsWith('/api/system/logs')) {
        return Promise.resolve({ data: { items: [], summary: { total: 0, by_level: {} } } });
      }
      return Promise.resolve({ data: null });
    });

    wrap(<Logs />);
    await waitFor(() => expect(logRequests()).toHaveLength(1));

    const box = screen.getByPlaceholderText('搜索日志关键字');
    for (const value of ['e', 'er', 'err']) {
      fireEvent.change(box, { target: { value } });
    }
    await Promise.resolve();
    expect(logRequests()).toHaveLength(1);

    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(logRequests()).toHaveLength(2));
    expect(logRequests()[1]).toContain('q=err');
  });
});
