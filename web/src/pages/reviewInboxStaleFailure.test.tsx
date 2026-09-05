/**
 * @vitest-environment jsdom
 *
 * 守两个审阅收件箱的失败路径也认世代号：换筛选条件后旧条件那份请求失败时，只准清空它自己那一代，
 * 不能把新条件已经取回并渲染出来的列表整份抹掉——界面会毫无征兆地退回「当前没有待审核的…」。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import { LocaleProvider } from '../i18n/LocaleProvider';
import { ToastProvider } from '../components/ToastProvider';
import { messages as zhCN } from '../i18n/locales/zh-CN';
import AIGroupingReviews from './AIGroupingReviews';
import MetadataReviews from './MetadataReviews';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }));

vi.mock('../api/client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    put: mocks.put,
    defaults: { headers: { common: {} as Record<string, string> } },
  },
  isAxiosError: () => false,
  isCancel: () => false,
  getApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

class FakeIntersectionObserver implements IntersectionObserver {
  root: Element | null = null;
  rootMargin = '';
  thresholds: number[] = [];
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords(): IntersectionObserverEntry[] { return []; }
}

/** 受控 promise：旧条件那份请求什么时候失败由用例说了算，不看机器快慢。 */
function deferred<T>() {
  let fail!: (error: unknown) => void;
  const promise = new Promise<T>((_resolve, reject) => { fail = reject; });
  return { promise, fail };
}

function metadataItem(id: number, seriesName: string) {
  return {
    id,
    series_id: id,
    provider: 'anilist',
    source_url: '',
    source_id: 0,
    source_query: '',
    summary: '',
    confidence: 0.9,
    status: 'pending',
    raw_payload: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    fields: [{ field_name: 'title', label: '标题', current: '旧', proposed: '新', locked: false }],
    library_id: 1,
    library_name: '主库',
    series_name: seriesName,
    series_title: '',
    cover_book_id: 0,
    field_count: 1,
    locked_field_count: 0,
  };
}

function aiReview(id: number) {
  return {
    id,
    library_id: 1,
    library_name: '主库',
    provider: 'ollama',
    status: 'applied',
    summary: '',
    candidate_count: 1,
    collection_count: 1,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    collections: [{
      id: id * 100,
      review_id: id,
      name: `候选合集 ${id}`,
      description: '',
      series_ids: [1],
      series: [{ id: 1, name: '系列一', title: '' }],
      series_count: 1,
      status: 'applied',
    }],
  };
}

function renderPage(node: React.ReactElement) {
  return render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter>{node}</MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('审阅收件箱换筛选条件后旧请求失败', () => {
  it('元数据提案：旧关键词那份失败，不清空新关键词已经列出来的提案', async () => {
    const stale = deferred<never>();
    mocks.get.mockImplementation((_url: string, config: { params?: { q?: string } } = {}) => {
      if (!config.params?.q) return stale.promise;
      return Promise.resolve({ data: { items: [metadataItem(9, '新关键词的系列')], total: 1, limit: 30, offset: 0 } });
    });

    renderPage(<MetadataReviews embedded />);

    // 首屏那份挂着不回；用户改关键词再查，新条件这份先落地。
    fireEvent.change(screen.getByPlaceholderText(zhCN['metadataReviews.searchPlaceholder']), { target: { value: '新关键词' } });
    fireEvent.click(screen.getByText('搜索'));
    // 左栏与右栏各列一次，取全部。
    await screen.findAllByText('新关键词的系列');

    await act(async () => { stale.fail(new Error('stale request failed')); });

    expect(screen.getAllByText('新关键词的系列').length).toBeGreaterThan(0);
    expect(screen.queryByText(zhCN['metadataReviews.empty'])).toBeNull();
    expect(screen.getByText('已加载 1 / 1')).toBeTruthy();
  });

  it('分组提案：旧筛选那份失败，不清空新筛选已经列出来的提案', async () => {
    const stale = deferred<never>();
    mocks.get.mockImplementation((_url: string, config: { params?: { status?: string } } = {}) => {
      if (config.params?.status === 'pending') return stale.promise;
      return Promise.resolve({ data: { items: [aiReview(9)], total: 1, limit: 20, offset: 0 } });
    });

    const { container } = renderPage(<AIGroupingReviews embedded />);

    // 首屏的「待审核」挂着不回；用户改看「已应用」，这一份先落地。
    const statusSelect = container.querySelectorAll('select')[1] as HTMLSelectElement;
    fireEvent.change(statusSelect, { target: { value: 'applied' } });
    await screen.findAllByText('审核单 #9');

    await act(async () => { stale.fail(new Error('stale request failed')); });

    expect(screen.getAllByText('审核单 #9').length).toBeGreaterThan(0);
    expect(screen.queryByText(zhCN['aiGroupingReviews.empty'])).toBeNull();
    expect(screen.getByText('已加载 1 / 1')).toBeTruthy();
  });
});
