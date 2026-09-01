/**
 * @vitest-environment jsdom
 *
 * 守智能书架固化弹窗对后端 409（重名）的交代：弹出本地化提示、弹窗保持打开好让用户改名。
 * 没有这一条，请求失败后界面毫无反应，用户只会反复点「创建快照」。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { BrowserRouter } from 'react-router-dom';

import { apiClient } from '../../api/client';
import { AuthProvider } from '../../auth/AuthProvider';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { ToastProvider } from '../../components/ToastProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import Collections from './index';

const SMART_VIEW = {
  view_id: 'smart:7',
  kind: 'smart',
  id: 7,
  numeric_id: 7,
  name: '高分未读',
  description: '',
  series_count: 1,
  source_type: 'smart_filter',
  created_at: '2026-01-01T00:00:00Z',
};

const SMART_ITEM = { id: 1001, name: '智能系列 1', cover_path: { String: '', Valid: false }, actual_book_count: 1 };

const ADMIN_STATUS = {
  setup_required: false,
  authenticated: true,
  csrf_token: 'csrf',
  user: { id: 1, username: 'admin', role: 'admin', display_name: 'admin', must_change_password: false },
};

function mockApi() {
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
    if (url.startsWith('/api/auth/status')) return Promise.resolve({ data: ADMIN_STATUS });
    if (url === '/api/collection-views') return Promise.resolve({ data: [SMART_VIEW] });
    if (url.startsWith('/api/collection-views/smart/7/series')) {
      return Promise.resolve({ data: { items: [SMART_ITEM], total: 1, limit: 30, offset: 0, kind: 'smart', view_id: 'smart:7', view_name: '高分未读' } });
    }
    if (url.startsWith('/api/collection-views/smart/7/snapshot-preview')) {
      return Promise.resolve({
        data: { items: [SMART_ITEM], total: 1, preview_limit: 8, snapshot_limit: 1000, snapshot_count: 1, truncated: false, name_conflict: false },
      });
    }
    return Promise.resolve({ data: [] });
  }) as never);
}

function renderPage() {
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
}

// openSnapshotModal 选中智能书架、打开固化弹窗，并等到预览到齐（提交按钮在预览加载期间是禁用的）。
async function openSnapshotModal() {
  fireEvent.click(await screen.findByText('高分未读'));
  fireEvent.click(await screen.findByTitle(zhCN['collections.snapshot']));
  const submit = await screen.findByText(zhCN['collections.snapshotSubmit']);
  await waitFor(() => expect((submit as HTMLButtonElement).disabled).toBe(false));
  return submit;
}

describe('智能书架固化撞名', () => {
  beforeEach(() => {
    mockApi();
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('后端 409 时提示重名且弹窗不关', async () => {
    const post = vi.spyOn(apiClient, 'post').mockRejectedValue({
      response: { status: 409, data: { error: 'A collection with this name already exists' } },
    });
    renderPage();
    fireEvent.click(await openSnapshotModal());

    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(zhCN['collections.nameConflict'])).toBeTruthy();
    // 弹窗留在原地，名字也还在输入框里——改一个字就能重试。
    expect(screen.getByText(zhCN['collections.snapshotSubmit'])).toBeTruthy();
  });

  it('反向判据：固化成功时不提示重名并关掉弹窗', async () => {
    vi.spyOn(apiClient, 'post').mockResolvedValue({ data: { id: 9, name: '高分未读', series_count: 1 } } as never);
    renderPage();
    fireEvent.click(await openSnapshotModal());

    await waitFor(() => expect(screen.queryByText(zhCN['collections.snapshotSubmit'])).toBeNull());
    expect(screen.queryByText(zhCN['collections.nameConflict'])).toBeNull();
  });
});
