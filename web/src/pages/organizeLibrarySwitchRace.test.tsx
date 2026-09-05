/**
 * @vitest-environment jsdom
 *
 * 守整理工作台的健康报告：慢网下连切两个资料库，先发的响应后到不能盖掉界面——
 * 否则下拉框写着甲库，列表列的是乙库的问题。切库当场清空旧报告，迟到的那份整份丢弃。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import { LocaleProvider } from '../i18n/LocaleProvider';
import { ToastProvider } from '../components/ToastProvider';
import { messages as zhCN } from '../i18n/locales/zh-CN';
import Organize from './Organize';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));

vi.mock('../api/client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    defaults: { headers: { common: {} as Record<string, string> } },
  },
  isAxiosError: () => false,
  isCancel: () => false,
  getApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

vi.mock('../auth/AuthProvider', () => ({ useAuth: () => ({ isAdmin: false }) }));

const LIBRARIES = [{ id: 1, name: '甲库' }, { id: 2, name: '乙库' }];

function report(libraryName: string, seriesName: string) {
  return {
    data: {
      summary: [{ type: 'empty_pages', severity: 'error', count: 1 }],
      issues: [{ type: 'empty_pages', severity: 'error', series_id: 1, library_name: libraryName, series_name: seriesName }],
      limit: 80,
    },
  };
}

/** 受控 promise：由用例决定响应什么时候到，不看机器快慢。 */
function deferred<T>() {
  let settle!: (value: T) => void;
  const promise = new Promise<T>((resolve) => { settle = resolve; });
  return { promise, settle };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderPage() {
  return render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter>
          <Organize />
        </MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
}

describe('整理工作台切换资料库', () => {
  it('慢网下先选的库响应后到，也不会盖掉已经切过去的那个库的问题列表', async () => {
    const first = deferred<ReturnType<typeof report>>();
    mocks.get.mockImplementation((url: string) => {
      if (url === '/api/libraries') return Promise.resolve({ data: LIBRARIES });
      if (url.includes('library_id=1')) return first.promise;
      if (url.includes('library_id=2')) return Promise.resolve(report('乙库', '乙库的问题系列'));
      return Promise.resolve(report('全部资料库', '全部资料库的问题系列'));
    });

    const { container } = renderPage();
    await screen.findByText('全部资料库的问题系列');

    const select = container.querySelector('select') as HTMLSelectElement;
    // 切到甲库：请求挂着不回。
    fireEvent.change(select, { target: { value: '1' } });
    await waitFor(() => expect(mocks.get.mock.calls.some((call) => String(call[0]).includes('library_id=1'))).toBe(true));
    // 上一个库的问题当场清掉，别让它冒充甲库的。
    expect(screen.queryByText('全部资料库的问题系列')).toBeNull();

    // 再切到乙库：这一份先落地。
    fireEvent.change(select, { target: { value: '2' } });
    await screen.findByText('乙库的问题系列');

    await act(async () => { first.settle(report('甲库', '甲库的问题系列')); });

    expect(screen.queryByText('甲库的问题系列')).toBeNull();
    expect(screen.getByText('乙库的问题系列')).toBeTruthy();
    expect(select.value).toBe('2');
  });
});
