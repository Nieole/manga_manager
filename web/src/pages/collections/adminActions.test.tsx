/**
 * @vitest-environment jsdom
 *
 * 守合集页「后端会拒的动作不摆给普通用户」：合集与智能书架是站点级数据（表里没有 user_id），
 * 新建、改名、删除、固化快照与成员增删在后端一律要管理员。浏览是读操作，普通用户要原样保留
 * （反向判据），管理员那侧也必须一切照旧。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { BrowserRouter } from 'react-router-dom';

import { apiClient } from '../../api/client';
import { AuthProvider, useAuth } from '../../auth/AuthProvider';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import Collections from './index';

const MANUAL_VIEW = {
  view_id: 'collection:3',
  kind: 'collection',
  id: 3,
  numeric_id: 3,
  name: '手工精选',
  description: '',
  series_count: 1,
  source_type: 'manual',
  created_at: '2026-01-01T00:00:00Z',
};

const SMART_VIEW = {
  view_id: 'smart:7',
  kind: 'smart',
  id: 7,
  numeric_id: 7,
  name: '智能书架',
  description: '',
  series_count: 1,
  source_type: 'smart',
  created_at: '2026-01-01T00:00:00Z',
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
    if (url === '/api/collection-views') return Promise.resolve({ data: [MANUAL_VIEW, SMART_VIEW] });
    if (url === '/api/collections/3/series') {
      return Promise.resolve({ data: [{ series_id: 201, series_name: '手工系列 甲', cover_path: { String: '', Valid: false }, book_count: 1 }] });
    }
    return Promise.resolve({ data: [] });
  }) as never);
  render(
    <BrowserRouter>
      <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
        <AuthProvider>
          <AuthProbe />
          <Collections />
        </AuthProvider>
      </LocaleProvider>
    </BrowserRouter>,
  );
  await screen.findByText(role === 'admin' ? '管理员' : '普通用户');
  await screen.findByText('手工精选');
}

// 左栏里某个合集所在的那一行，好把「有没有删除按钮」问到具体一行头上。
function listRow(name: string): HTMLElement {
  const rows = screen
    .getAllByText(name)
    .map((node) => node.closest('div.group'))
    .filter((node): node is HTMLElement => node !== null);
  if (rows.length !== 1) throw new Error(`左栏应恰好有一行 ${name}，实得 ${rows.length}`);
  return rows[0];
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('合集页的管理员动作', () => {
  it('普通用户看得到合集，但没有新建与删除入口', async () => {
    await renderPage('regular');

    expect(screen.queryByRole('button', { name: '新建合集' })).toBeNull();
    expect(within(listRow('手工精选')).queryByRole('button', { name: '删除' })).toBeNull();
    expect(within(listRow('智能书架')).queryByRole('button', { name: '删除' })).toBeNull();
    // 反向判据：两个合集本身照旧列得出来，页面不是一屏空白。
    expect(screen.getByText('智能书架')).toBeTruthy();
  });

  it('普通用户进合集后看得到成员，但没有改名、快照与移除成员', async () => {
    await renderPage('regular');
    fireEvent.click(within(listRow('手工精选')).getByText('手工精选'));

    expect(await screen.findByText('手工系列 甲')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '编辑' })).toBeNull();
    expect(screen.queryByRole('button', { name: '固化快照' })).toBeNull();
    expect(screen.queryByRole('button', { name: '移除' })).toBeNull();
  });

  it('管理员一切照旧：新建、删除、改名与移除成员都在', async () => {
    await renderPage('admin');

    expect(screen.getByRole('button', { name: '新建合集' })).toBeTruthy();
    expect(within(listRow('手工精选')).getByRole('button', { name: '删除' })).toBeTruthy();
    expect(within(listRow('智能书架')).getByRole('button', { name: '删除' })).toBeTruthy();

    fireEvent.click(within(listRow('手工精选')).getByText('手工精选'));
    expect(await screen.findByText('手工系列 甲')).toBeTruthy();
    expect(screen.getByRole('button', { name: '编辑' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '移除' })).toBeTruthy();
  });
});
