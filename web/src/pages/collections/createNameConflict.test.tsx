/**
 * @vitest-environment jsdom
 *
 * 守新建合集弹窗对后端 409（重名）的交代：弹出本地化提示、弹窗保持打开好让用户改名。
 * 没有这一条，请求失败后界面毫无反应，用户只会反复点「创建」。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { BrowserRouter } from 'react-router-dom';

import { apiClient } from '../../api/client';
import { AuthProvider } from '../../auth/AuthProvider';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { ToastProvider } from '../../components/ToastProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import Collections from './index';

const MANUAL_VIEW = {
  view_id: 'collection:3',
  kind: 'collection',
  id: 3,
  numeric_id: 3,
  name: '科幻',
  description: '',
  series_count: 0,
  source_type: 'manual',
  created_at: '2026-01-01T00:00:00Z',
};

async function renderPage() {
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
    if (url.startsWith('/api/auth/status')) {
      return Promise.resolve({
        data: {
          setup_required: false,
          authenticated: true,
          csrf_token: 'csrf',
          user: { id: 1, username: 'admin', role: 'admin', display_name: 'admin', must_change_password: false },
        },
      });
    }
    if (url === '/api/collection-views') return Promise.resolve({ data: [MANUAL_VIEW] });
    return Promise.resolve({ data: [] });
  }) as never);
  render(
    <BrowserRouter>
      <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
        <ToastProvider>
          <AuthProvider>
            <Collections />
          </AuthProvider>
        </ToastProvider>
      </LocaleProvider>
    </BrowserRouter>,
  );
  await screen.findByText('科幻');
}

async function openCreateModal(name: string) {
  fireEvent.click(await screen.findByText(zhCN['collections.create']));
  const input = await screen.findByPlaceholderText(zhCN['collections.namePlaceholder']);
  fireEvent.change(input, { target: { value: name } });
  fireEvent.click(screen.getByText(zhCN['common.create']));
}

describe('新建合集撞名', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('后端 409 时提示重名且弹窗不关', async () => {
    const post = vi.spyOn(apiClient, 'post').mockRejectedValue({
      response: { status: 409, data: { error: 'A collection with this name already exists' } },
    });
    await renderPage();
    await openCreateModal(' 科幻 ');

    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(zhCN['collections.nameConflict'])).toBeTruthy();
    // 弹窗留在原地，名字也还在输入框里——改一个字就能重试。
    expect(screen.getByPlaceholderText(zhCN['collections.namePlaceholder'])).toBeTruthy();
  });

  it('反向判据：建号成功时不提示重名并关掉弹窗', async () => {
    vi.spyOn(apiClient, 'post').mockResolvedValue({ data: { id: 9, name: '奇幻' } } as never);
    await renderPage();
    await openCreateModal('奇幻');

    await waitFor(() => expect(screen.queryByPlaceholderText(zhCN['collections.namePlaceholder'])).toBeNull());
    expect(screen.queryByText(zhCN['collections.nameConflict'])).toBeNull();
  });
});
