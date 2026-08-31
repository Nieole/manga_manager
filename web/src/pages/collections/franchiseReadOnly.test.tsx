/**
 * @vitest-environment jsdom
 *
 * 守作品群在合集页上是只读的：列得出、进得去、看得到成员，但不给删除与移除成员的入口；
 * 普通手工合集的这两个入口必须原样保留（反向判据）。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { BrowserRouter } from 'react-router-dom';

import { apiClient } from '../../api/client';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import Collections from './index';

const FRANCHISE_VIEW = {
  view_id: 'collection:5',
  kind: 'collection',
  id: 5,
  numeric_id: 5,
  name: '孤独摇滚 Franchise',
  description: '',
  series_count: 2,
  source_type: 'system_franchise',
  created_at: '2026-01-01T00:00:00Z',
};

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

function member(id: number, name: string) {
  return { series_id: id, series_name: name, cover_path: { String: '', Valid: false }, book_count: 1 };
}

function mockApi() {
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
    if (url === '/api/collection-views') {
      return Promise.resolve({ data: [FRANCHISE_VIEW, MANUAL_VIEW] });
    }
    if (url === '/api/collections/5/series') {
      return Promise.resolve({ data: [member(101, '作品群系列 甲'), member(102, '作品群系列 乙')] });
    }
    if (url === '/api/collections/3/series') {
      return Promise.resolve({ data: [member(201, '手工系列 甲')] });
    }
    return Promise.resolve({ data: [] });
  }) as never);
}

// listRow 取左栏里某个合集所在的那一行，好把「有没有删除按钮」问到具体一行头上，
// 而不是数整页的按钮个数——后者会让两个合集互相干扰。
function listRow(name: string): HTMLElement {
  // 选中后合集名在右栏标题里也会出现一次，只有左栏那处外面裹着 .group 行容器。
  const rows = screen
    .getAllByText(name)
    .map((node) => node.closest('div.group'))
    .filter((node): node is HTMLElement => node !== null);
  if (rows.length !== 1) throw new Error(`左栏应恰好有一行 ${name}，实得 ${rows.length}`);
  return rows[0];
}

function renderPage() {
  return render(
    <BrowserRouter>
      <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
        <Collections />
      </LocaleProvider>
    </BrowserRouter>,
  );
}

describe('合集页上的作品群', () => {
  beforeEach(() => {
    mockApi();
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('作品群仍然列得出、进得去、看得到成员', async () => {
    renderPage();
    fireEvent.click(await screen.findByText('孤独摇滚 Franchise'));

    // 隐藏掉会丢功能：「这个系列的相关作品」正是用户想在合集页看的东西。
    await waitFor(() => {
      expect(screen.getByText('作品群系列 甲')).toBeTruthy();
    });
    expect(screen.getByText('作品群系列 乙')).toBeTruthy();
    expect(screen.getByText('只读')).toBeTruthy();
  });

  it('作品群不给删除、改名与移除成员的入口', async () => {
    renderPage();
    fireEvent.click(await screen.findByText('孤独摇滚 Franchise'));
    await waitFor(() => {
      expect(screen.getByText('作品群系列 甲')).toBeTruthy();
    });

    // 左栏作品群那一行没有删除按钮。
    expect(within(listRow('孤独摇滚 Franchise')).queryByRole('button', { name: '删除' })).toBeNull();
    // 右栏：既没有改名的铅笔，也没有逐个成员的移除按钮。
    expect(screen.queryByRole('button', { name: '编辑' })).toBeNull();
    expect(screen.queryAllByRole('button', { name: '移除' })).toHaveLength(0);
  });

  // 反向判据：闸门只挡作品群，普通合集的入口一个都不能少。
  it('普通手工合集的删除、改名与移除成员入口不退化', async () => {
    renderPage();
    fireEvent.click(await screen.findByText('手工精选'));
    await waitFor(() => {
      expect(screen.getByText('手工系列 甲')).toBeTruthy();
    });

    expect(within(listRow('手工精选')).getByRole('button', { name: '删除' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '编辑' })).toBeTruthy();
    expect(screen.queryAllByRole('button', { name: '移除' })).toHaveLength(1);
    expect(screen.queryByText('只读')).toBeNull();
  });
});
