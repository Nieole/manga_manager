/**
 * @vitest-environment jsdom
 *
 * 守资料库页「后端会拒的动作不摆给普通用户」：收藏写的是 series.is_favorite（站点级标记，不是
 * 每用户收藏），与重扫、刮削、批量编辑、加入合集、智能视图存删一样在后端都要管理员。批量标记
 * 已读/未读走每用户的进度端点，必须原样留着（反向判据），管理员那侧也必须一切照旧。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { apiClient } from '../../api/client';
import { AuthProvider, useAuth } from '../../auth/AuthProvider';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import { ToastProvider } from '../../components/ToastProvider';
import { __resetScrapeProvidersCacheForTest } from '../../hooks/useScrapeProviders';
import type { Series } from './types';
import LibraryPage from './index';

const CARD_SERIES: Series = {
  id: 1,
  name: '测试系列',
  volume_count: 1,
  actual_book_count: 1,
  read_count: 0,
  total_pages: { Valid: true, Float64: 10 },
  is_favorite: false,
};

// 探针消费真的 useAuth，把「鉴权已落地」变成一条可等待的判据：普通用户那侧全是「找不到」，
// 不等这一步的话，鉴权还没回来时用例就跑完了，任何实现都能通过。
function AuthProbe() {
  const { loading, isAdmin } = useAuth();
  return <span>{loading ? '加载中' : isAdmin ? '管理员' : '普通用户'}</span>;
}

async function renderPage(role: 'admin' | 'regular') {
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
    if (url.startsWith('/api/auth/status')) {
      return Promise.resolve({
        data: {
          setup_required: false,
          authenticated: true,
          csrf_token: 'csrf',
          user: { id: 1, username: role, role, display_name: role, must_change_password: false },
        },
      });
    }
    if (url.startsWith('/api/series/search')) {
      return Promise.resolve({ data: { items: [CARD_SERIES], total: 1, next_cursor: '' } });
    }
    return Promise.resolve({ data: [] });
  }) as never);
  vi.spyOn(apiClient, 'post').mockResolvedValue({ data: {} } as never);
  render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter initialEntries={['/library/1']}>
          <AuthProvider>
            <AuthProbe />
            <Routes>
              <Route path="/library/:libId" element={<LibraryPage />} />
            </Routes>
          </AuthProvider>
        </MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
  await screen.findByText(role === 'admin' ? '管理员' : '普通用户');
  await screen.findByRole('button', { name: '批量操作' });
}

// 底部选择栏在未进入批量模式时不渲染，先照用户的顺序把它唤出来。
function enterSelection() {
  fireEvent.click(screen.getByRole('button', { name: '批量操作' }));
  fireEvent.click(screen.getByRole('button', { name: '全选本页' }));
}

function action(name: string) {
  return screen.queryByRole('button', { name });
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  window.localStorage.clear();
  __resetScrapeProvidersCacheForTest();
});

describe('资料库页的管理员动作', () => {
  it('普通用户的卡片上没有刮削、重扫与收藏', async () => {
    await renderPage('regular');

    expect(action('刮削元数据')).toBeNull();
    expect(action('重新扫描该系列')).toBeNull();
    // 反向判据：系列本身照旧列得出来，进得去。
    expect(screen.getByRole('link', { name: /测试系列/ })).toBeTruthy();
  });

  it('普通用户的批量操作栏只剩标记已读/未读', async () => {
    await renderPage('regular');
    enterSelection();

    expect(action('标记收藏')).toBeNull();
    expect(action('移除收藏')).toBeNull();
    expect(action('加入合集')).toBeNull();
    expect(action('批量编辑')).toBeNull();
    // 反向判据：阅读进度是每用户写操作，后端放行，界面也必须留着。
    expect(action('标记已读')).toBeTruthy();
    expect(action('标记未读')).toBeTruthy();
  });

  it('管理员一切照旧：卡片动作与整条批量操作栏都在', async () => {
    await renderPage('admin');

    expect(action('刮削元数据')).toBeTruthy();
    expect(action('重新扫描该系列')).toBeTruthy();

    enterSelection();
    expect(action('标记收藏')).toBeTruthy();
    expect(action('加入合集')).toBeTruthy();
    expect(action('批量编辑')).toBeTruthy();
    expect(action('标记已读')).toBeTruthy();
  });
});
