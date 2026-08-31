/**
 * @vitest-environment jsdom
 *
 * 守合集页右栏：智能书架首屏只取一页、计数报命中总数、能翻到全部成员，手工合集不退化。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { BrowserRouter } from 'react-router-dom';

import { apiClient } from '../../api/client';
import { AuthProvider } from '../../auth/AuthProvider';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import Collections from './index';

const SMART_TOTAL = 42;
const SMART_PAGE_SIZE = 30;
const MANUAL_TOTAL = 4;

const SMART_VIEW = {
  view_id: 'smart:7',
  kind: 'smart',
  id: 7,
  numeric_id: 7,
  name: '高分未读',
  description: '',
  series_count: SMART_TOTAL,
  source_type: 'smart_filter',
  created_at: '2026-01-01T00:00:00Z',
};

const MANUAL_VIEW = {
  view_id: 'collection:3',
  kind: 'collection',
  id: 3,
  numeric_id: 3,
  name: '手工精选',
  description: '',
  series_count: MANUAL_TOTAL,
  source_type: 'manual',
  created_at: '2026-01-01T00:00:00Z',
};

const SMART_FILTER = {
  id: 7,
  name: '高分未读',
  sortByField: 'name',
  sortDir: 'asc',
  pageSize: SMART_PAGE_SIZE,
};

function smartItem(index: number) {
  return {
    id: 1000 + index,
    name: `智能系列 ${index}`,
    cover_path: { String: '', Valid: false },
    actual_book_count: 1,
  };
}

function manualItem(index: number) {
  return {
    series_id: 2000 + index,
    series_name: `手工系列 ${index}`,
    cover_path: { String: '', Valid: false },
    book_count: 1,
  };
}

interface RecordedRequest {
  url: string;
  params?: Record<string, unknown>;
}

let requests: RecordedRequest[] = [];
// 值为 null 表示这条路径的响应先挂起，由用例稍后手动兑现，用来制造慢网。
let pendingSmart: ((value: unknown) => void) | null = null;
let holdSmart = false;

function smartPage(params?: Record<string, unknown>) {
  const limit = Number(params?.limit ?? SMART_PAGE_SIZE);
  const offset = Number(params?.offset ?? 0);
  const items = [];
  for (let i = offset; i < Math.min(offset + limit, SMART_TOTAL); i++) {
    items.push(smartItem(i));
  }
  return { items, total: SMART_TOTAL, limit, offset, filter: SMART_FILTER, kind: 'smart', view_id: 'smart:7', view_name: '高分未读' };
}

// 合集页的管理动作按 isAdmin 决定渲不渲染，本文件守的不是权限，故一律以管理员登录。
const ADMIN_STATUS = {
  setup_required: false,
  authenticated: true,
  csrf_token: 'csrf',
  user: { id: 1, username: 'admin', role: 'admin', display_name: 'admin', must_change_password: false },
};

function mockApi() {
  requests = [];
  pendingSmart = null;
  holdSmart = false;
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string, config?: { params?: Record<string, unknown> }) => {
    if (url.startsWith('/api/auth/status')) return Promise.resolve({ data: ADMIN_STATUS });
    requests.push({ url, params: config?.params });
    if (url === '/api/collection-views') {
      return Promise.resolve({ data: [MANUAL_VIEW, SMART_VIEW] });
    }
    if (url.startsWith('/api/collection-views/smart/7/series')) {
      const payload = { data: smartPage(config?.params) };
      if (holdSmart) {
        return new Promise((resolve) => {
          pendingSmart = () => resolve(payload);
        });
      }
      return Promise.resolve(payload);
    }
    if (url === '/api/collections/3/series') {
      return Promise.resolve({ data: Array.from({ length: MANUAL_TOTAL }, (_, i) => manualItem(i)) });
    }
    return Promise.resolve({ data: [] });
  }) as never);
}

function renderPage() {
  return render(
    <BrowserRouter>
      <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
        <AuthProvider>
          <Collections />
        </AuthProvider>
      </LocaleProvider>
    </BrowserRouter>,
  );
}

function seriesRequests() {
  return requests.filter((r) => r.url.startsWith('/api/collection-views/smart/7/series'));
}

describe('合集页右栏的成员加载', () => {
  beforeEach(() => {
    mockApi();
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('智能书架命中数超过一页时，首屏只取一页但计数报总数，且能加载出全部成员', async () => {
    renderPage();
    fireEvent.click(await screen.findByText('高分未读'));

    // 首屏只有一页：42 个命中不能一次全灌给前端。
    await waitFor(() => {
      expect(screen.getAllByText(/^智能系列 /)).toHaveLength(SMART_PAGE_SIZE);
    });
    expect(seriesRequests()).toHaveLength(1);

    // 计数必须诚实地报出「已加载 / 共」，其中总数与左栏的 42 一致。
    expect(screen.getByText('已加载 30 / 共 42 个系列')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await waitFor(() => {
      expect(screen.getAllByText(/^智能系列 /)).toHaveLength(SMART_TOTAL);
    });

    // 追加请求带 offset，取的是第二页而不是重取第一页。
    const loadMore = seriesRequests()[1];
    expect(Number(loadMore.params?.offset)).toBe(SMART_PAGE_SIZE);
    expect(Number(loadMore.params?.limit)).toBe(SMART_PAGE_SIZE);

    // 全部加载完后不再有「加载更多」，右栏计数与左栏同为 42 个系列。
    expect(screen.queryByRole('button', { name: '加载更多' })).toBeNull();
    expect(screen.getAllByText('42 个系列')).toHaveLength(2);
  });

  it('手工合集仍一次取完，没有加载更多，计数与左栏一致', async () => {
    renderPage();
    fireEvent.click(await screen.findByText('手工精选'));

    await waitFor(() => {
      expect(screen.getAllByText(/^手工系列 /)).toHaveLength(MANUAL_TOTAL);
    });
    expect(screen.queryByRole('button', { name: '加载更多' })).toBeNull();
    expect(screen.getAllByText('4 个系列')).toHaveLength(2);
  });

  it('慢网下先点智能书架再点手工合集，先发的响应后到也不会盖掉当前成员', async () => {
    renderPage();
    await screen.findByText('高分未读');

    holdSmart = true;
    fireEvent.click(screen.getByText('高分未读'));
    await waitFor(() => {
      expect(pendingSmart).not.toBeNull();
    });

    holdSmart = false;
    fireEvent.click(screen.getByText('手工精选'));
    await waitFor(() => {
      expect(screen.getAllByText(/^手工系列 /)).toHaveLength(MANUAL_TOTAL);
    });

    // 智能书架的响应此刻才到：标题是手工精选，网格也必须还是手工精选的成员。
    pendingSmart?.(undefined);
    await waitFor(() => {
      expect(seriesRequests()).toHaveLength(1);
    });
    await Promise.resolve();
    expect(screen.queryAllByText(/^智能系列 /)).toHaveLength(0);
    expect(screen.getAllByText(/^手工系列 /)).toHaveLength(MANUAL_TOTAL);
  });
});
