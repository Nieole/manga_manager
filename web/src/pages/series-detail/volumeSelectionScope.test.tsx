/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「批量选择的范围等于页面此刻渲染的那批书」：卷视图（`?volume=...`）只渲染这一卷，
 * 底部选择栏却与整页共用。「全选」越出这一卷的话，用户以为只标了这一卷，实际把整个系列都写成
 * 已读，页面上还看不出来。同时守全选后按钮要翻成「取消全选」——可选总数与全选实际选中的那批书
 * 必须同源，否则用户无从判断自己到底选中了什么。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { apiClient } from '../../api/client';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import { ToastProvider } from '../../components/ToastProvider';
import { __resetScrapeProvidersCacheForTest } from '../../hooks/useScrapeProviders';
import type { Book, SeriesContextResponse } from './types';
import SeriesDetailPage from './index';

function makeBook(id: number, volume: string): Book {
  return {
    id,
    name: `书${id}`,
    library_id: 1,
    volume,
    page_count: 10,
    last_read_page: { Valid: false, Int64: 0 },
  };
}

// 一个既有卷、又有独立书的系列：卷视图只渲染第3卷的两本，全选不得越出这两本。
const BOOKS: Book[] = [
  makeBook(101, '第1卷'),
  makeBook(102, '第1卷'),
  makeBook(301, '第3卷'),
  makeBook(302, '第3卷'),
  makeBook(900, ''),
];

const CONTEXT: SeriesContextResponse = {
  series: {
    id: 1,
    name: '测试系列',
    library_id: 1,
    path: '/lib/1',
    book_count: BOOKS.length,
    locked_fields: { String: '', Valid: false },
  },
  books: BOOKS,
  tags: [],
  authors: [],
  links: [],
};

function mockApi() {
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) =>
    Promise.resolve({ data: url.endsWith('/context') ? CONTEXT : [] })) as never);
  return vi.spyOn(apiClient, 'post').mockResolvedValue({ data: {} } as never);
}

// 词条同步传入，避免异步加载语言包让按钮文案在「键名」和「中文」之间抖动。
async function renderPage(search: string) {
  render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter initialEntries={[`/series/1${search}`]}>
          <Routes>
            <Route path="/series/:seriesId" element={<SeriesDetailPage />} />
          </Routes>
        </MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
  await screen.findByRole('button', { name: '进入批量选择' });
}

function clickButton(name: string) {
  fireEvent.click(screen.getByRole('button', { name }));
}

// 底部选择栏在选中数为 0 时不渲染，所以要先点一本书把它唤出来——这也正是用户的操作顺序。
function enterSelectionAndPick(bookName: string) {
  clickButton('进入批量选择');
  fireEvent.click(screen.getByRole('link', { name: new RegExp(bookName) }));
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  window.localStorage.clear();
  __resetScrapeProvidersCacheForTest();
});

describe('系列详情批量选择的范围', () => {
  it('在某一卷的视图里全选，只标记这一卷的书', async () => {
    const post = mockApi();
    await renderPage('?volume=第3卷');

    enterSelectionAndPick('书301');
    clickButton('全选');
    clickButton('标为已读');

    await waitFor(() => expect(post).toHaveBeenCalled());
    expect(post).toHaveBeenCalledWith('/api/books/bulk-progress', {
      book_ids: [301, 302],
      is_read: true,
    });
  });

  it('在某一卷的视图里全选后，按钮翻成「取消全选」', async () => {
    mockApi();
    await renderPage('?volume=第3卷');

    enterSelectionAndPick('书301');
    clickButton('全选');

    expect(screen.getByRole('button', { name: '取消全选' })).toBeTruthy();
  });

  it('系列总览里全选，仍覆盖整个系列的书并翻成「取消全选」', async () => {
    const post = mockApi();
    await renderPage('');

    enterSelectionAndPick('书900');
    clickButton('全选');
    expect(screen.getByRole('button', { name: '取消全选' })).toBeTruthy();

    clickButton('标为已读');
    await waitFor(() => expect(post).toHaveBeenCalled());
    expect(post).toHaveBeenCalledWith('/api/books/bulk-progress', {
      book_ids: [101, 102, 301, 302, 900],
      is_read: true,
    });
  });
});
