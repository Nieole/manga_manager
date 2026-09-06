/**
 * @vitest-environment jsdom
 *
 * 守任务中心自己纠正丢帧：推来的帧接不上上一帧就整份重拉，接得上就一个请求都不多发。
 * 丢掉的那一帧若恰好是终态，界面会一直停在过期的进度上——轮询降到 60s 之后更是如此。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, render, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import type { RunPush } from '../api/generated';
import { ToastProvider } from '../components/ToastProvider';
import BackgroundTasks from './BackgroundTasks';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), del: vi.fn() }));

vi.mock('../api/client', () => ({
  apiClient: { get: mocks.get, post: mocks.post, delete: mocks.del },
  isAxiosError: () => false,
  isCancel: () => false,
  getApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

// 词条只要能渲染出来即可：本文件断言的是发了几个请求，与译文无关。
// 这一份必须是**同一个对象**：取数闭包把 t 收在依赖里，每次渲染换一个新的会让 effect 反复重挂，
// 页面于是自己给自己刷起请求来，而那正是本文件要数的东西。
const i18n = vi.hoisted(() => ({
  t: (key: string) => key,
  locale: 'zh-CN',
  formatDateTime: (value: string) => value,
  formatRelativeTime: (value: string) => value,
}));

vi.mock('../i18n/LocaleProvider', () => ({ useI18n: () => i18n }));

const EMPTY_LIVE = { active: 0, queued: 0, slots: 2, paused: false, paused_all: false, runs: [] };

function push(frame: RunPush) {
  act(() => {
    window.dispatchEvent(new CustomEvent('manga-manager:run-push', { detail: frame }));
  });
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('任务中心的序号缺口', () => {
  async function renderPage() {
    mocks.get.mockImplementation((url: string) => {
      if (url.startsWith('/api/system/tasks/live')) return Promise.resolve({ data: EMPTY_LIVE });
      return Promise.resolve({ data: [] });
    });
    render(
      <ToastProvider>
        <MemoryRouter>
          <BackgroundTasks />
        </MemoryRouter>
      </ToastProvider>,
    );
    // 进页面先各取一次：实况帧与任务清单。
    await waitFor(() => expect(mocks.get).toHaveBeenCalledTimes(2));
    mocks.get.mockClear();
  }

  it('接得上上一帧就一个请求都不多发 —— 序号本来就会跳号', async () => {
    await renderPage();

    push({ sequence: 811, prev: 0, live: { active: 1, queued: 0, slots: 2, paused: false, paused_all: false } });
    // 中间那几号被节流水位吞掉了，从没送出来过：链是接着的，不是缺口。
    push({ sequence: 900, prev: 811, live: { active: 2, queued: 0, slots: 2, paused: false, paused_all: false } });

    expect(mocks.get).not.toHaveBeenCalled();
  });

  it('接不上就整份重拉一次：实况帧与任务清单各取一遍', async () => {
    await renderPage();

    push({ sequence: 811, prev: 0, live: { active: 1, queued: 0, slots: 2, paused: false, paused_all: false } });
    expect(mocks.get).not.toHaveBeenCalled();

    // 手里是 811，来的这一帧却说它前面是 813 —— 中间掉了东西。
    push({ sequence: 814, prev: 813, live: { active: 0, queued: 0, slots: 2, paused: false, paused_all: false } });

    await waitFor(() => {
      const urls = mocks.get.mock.calls.map((call) => String(call[0]));
      expect(urls.some((url) => url.startsWith('/api/system/tasks/live'))).toBe(true);
      expect(urls.some((url) => url.startsWith('/api/system/tasks/summary'))).toBe(true);
    });
  });

  it('重拉过一次之后，接得上的下一帧不再重拉', async () => {
    await renderPage();

    push({ sequence: 811, prev: 0, live: { active: 1, queued: 0, slots: 2, paused: false, paused_all: false } });
    push({ sequence: 814, prev: 813, live: { active: 0, queued: 0, slots: 2, paused: false, paused_all: false } });
    await waitFor(() => expect(mocks.get).toHaveBeenCalled());
    mocks.get.mockClear();

    push({ sequence: 815, prev: 814, live: { active: 1, queued: 0, slots: 2, paused: false, paused_all: false } });
    expect(mocks.get).not.toHaveBeenCalled();
  });
});
