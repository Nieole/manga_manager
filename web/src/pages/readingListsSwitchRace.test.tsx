/**
 * @vitest-environment jsdom
 *
 * 守阅读清单右栏的世代号：慢网下连点两个清单时，先发的响应后到，不能盖掉用户已经切过去的
 * 那一个——否则标题写着新清单，底下列的却是上一个清单的系列。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import { LocaleProvider } from '../i18n/LocaleProvider';
import { ToastProvider } from '../components/ToastProvider';
import { messages as zhCN } from '../i18n/locales/zh-CN';
import ReadingLists from './ReadingLists';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), del: vi.fn() }));

vi.mock('../api/client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    put: mocks.put,
    delete: mocks.del,
    defaults: { headers: { common: {} as Record<string, string> } },
  },
  isAxiosError: () => false,
  isCancel: () => false,
  getApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

vi.mock('../auth/AuthProvider', () => ({ useAuth: () => ({ isAdmin: false }) }));

const LIST_A = { id: 1, name: '清单甲', description: '', item_count: 1 };
const LIST_B = { id: 2, name: '清单乙', description: '', item_count: 1 };

function item(id: number, seriesName: string) {
  return {
    id,
    reading_list_id: id,
    series_id: id * 10,
    series_name: seriesName,
    series_title: '',
    book_count: 1,
    cover_path: '',
    next_book_id: 0,
    note: '',
    read_books: 0,
    completed_books: 0,
    total_books: 1,
  };
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function renderPage() {
  return render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter>
          <ReadingLists />
        </MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
}

describe('阅读清单切换取数', () => {
  it('慢网下先选的清单响应后到，也不会盖掉已经切过去的那个清单的成员', async () => {
    // 甲的成员响应先挂起，等乙的成员已经落地之后再兑现它。
    let releaseA: (() => void) | null = null;
    mocks.get.mockImplementation((url: string) => {
      if (url === '/api/reading-lists/') return Promise.resolve({ data: [LIST_A, LIST_B] });
      if (url === '/api/reading-lists/1/items') {
        return new Promise((resolve) => {
          releaseA = () => resolve({ data: [item(1, '甲的系列')] });
        });
      }
      if (url === '/api/reading-lists/2/items') return Promise.resolve({ data: [item(2, '乙的系列')] });
      return Promise.resolve({ data: [] });
    });

    renderPage();

    // 首屏自动选中甲，它的成员请求挂着不回。
    await waitFor(() => expect(releaseA).not.toBeNull());

    fireEvent.click(screen.getByText('清单乙'));
    await waitFor(() => expect(screen.getByText('乙的系列')).toBeTruthy());

    releaseA!();
    // 迟到的甲响应必须整份丢弃：右栏还是乙的成员。
    await waitFor(() => expect(mocks.get).toHaveBeenCalledWith('/api/reading-lists/2/items'));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByText('甲的系列')).toBeNull();
    expect(screen.getByText('乙的系列')).toBeTruthy();
  });
});
