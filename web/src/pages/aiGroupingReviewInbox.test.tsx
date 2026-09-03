/**
 * @vitest-environment jsdom
 *
 * 守 AI 分组审阅的收件箱不被单条候选合集的处置踹回队首：滚动累积的条数不缩水、右栏停在
 * 原来那条审核上。处置本身也要看得见效果（反向判据），否则「什么都不做」也能过。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import { LocaleProvider } from '../i18n/LocaleProvider';
import { ToastProvider } from '../components/ToastProvider';
import { messages as zhCN } from '../i18n/locales/zh-CN';
import AIGroupingReviews from './AIGroupingReviews';

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

const LIMIT = 20;
const TOTAL = 40;
const TARGET = 30;
const CREATED_COLLECTION_ID = 7;

interface FakeCollection {
  id: number;
  review_id: number;
  name: string;
  description: string;
  series_ids: number[];
  series: { id: number; name: string; title: string }[];
  series_count: number;
  status: string;
  created_collection_id?: number;
}

interface FakeReview {
  id: number;
  library_id: number;
  library_name: string;
  provider: string;
  status: string;
  summary: string;
  candidate_count: number;
  collection_count: number;
  created_at: string;
  updated_at: string;
  collections: FakeCollection[];
}

// createServer 复刻后端的两条关键行为：处置完最后一条候选合集，审核跟着落定；
// 落定后的审核就不再出现在「待审核」筛选里——这正是「就地更新」必须扛住的情形。
function createServer() {
  const reviews: FakeReview[] = Array.from({ length: TOTAL }, (_, index) => {
    const id = index + 1;
    return {
      id,
      library_id: 1,
      library_name: '主库',
      provider: 'ollama',
      status: 'pending',
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
        series: [{ id: 1, name: 'Alpha', title: 'Alpha' }],
        series_count: 1,
        status: 'pending',
      }],
    };
  });

  const find = (url: string) => {
    const matched = /\/reviews\/(\d+)\/collections\/(\d+)/.exec(url);
    if (!matched) throw new Error(`unexpected url: ${url}`);
    const review = reviews.find((item) => item.id === Number(matched[1]));
    const collection = review?.collections.find((item) => item.id === Number(matched[2]));
    if (!review || !collection) throw new Error(`unknown target: ${url}`);
    return { review, collection };
  };

  const finalize = (review: FakeReview) => {
    if (review.collections.some((item) => item.status === 'pending')) return;
    review.status = review.collections.some((item) => item.status === 'applied') ? 'applied' : 'rejected';
  };

  return {
    get: (_url: string, config?: { params?: Record<string, unknown> }) => {
      const status = String(config?.params?.status ?? '');
      const offset = Number(config?.params?.offset ?? 0);
      const limit = Number(config?.params?.limit ?? LIMIT);
      const matched = reviews.filter((item) => !status || item.status === status);
      return Promise.resolve({
        data: {
          items: structuredClone(matched.slice(offset, offset + limit)),
          total: matched.length,
          limit,
          offset,
        },
      });
    },
    post: (url: string) => {
      const { review, collection } = find(url);
      if (url.endsWith('/apply')) {
        collection.status = 'applied';
        collection.created_collection_id = CREATED_COLLECTION_ID;
      } else {
        collection.status = 'rejected';
      }
      finalize(review);
      return Promise.resolve({ data: { success: true, created_collection_id: CREATED_COLLECTION_ID } });
    },
    put: (url: string, body: { name: string; description: string; series_ids: number[] }) => {
      const { collection } = find(url);
      collection.name = body.name;
      collection.description = body.description;
      collection.series_ids = body.series_ids;
      collection.series_count = body.series_ids.length;
      return Promise.resolve({ data: structuredClone(collection) });
    },
  };
}

const observerCallbacks: IntersectionObserverCallback[] = [];

class FakeIntersectionObserver {
  root = null;
  rootMargin = '';
  thresholds: number[] = [];
  constructor(callback: IntersectionObserverCallback) {
    observerCallbacks.push(callback);
  }
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords(): IntersectionObserverEntry[] {
    return [];
  }
}

// listRowCount 数左栏累积了多少条审核；末尾那条 li 是无限滚动的哨兵，不算。
function listRowCount(container: HTMLElement) {
  const list = container.querySelector('ul');
  return list ? list.querySelectorAll(':scope > li').length - 1 : 0;
}

function activeReviewTitle(container: HTMLElement) {
  return container.querySelector('h2')?.textContent ?? '';
}

async function scrollForMore() {
  const notify = observerCallbacks[observerCallbacks.length - 1];
  await act(async () => {
    notify([{ isIntersecting: true } as IntersectionObserverEntry], null as never);
  });
}

beforeEach(() => {
  observerCallbacks.length = 0;
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

// setup 把用户带到「已滚动加载两屏、选中第 30 条」的现场。
async function setup() {
  const server = createServer();
  mocks.get.mockImplementation(server.get);
  mocks.post.mockImplementation(server.post);
  mocks.put.mockImplementation(server.put);

  const view = render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter>
          <AIGroupingReviews embedded />
        </MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );

  await screen.findByText(`已加载 ${LIMIT} / ${TOTAL}`);
  await scrollForMore();
  await screen.findByText(`已加载 ${TOTAL} / ${TOTAL}`);
  expect(listRowCount(view.container)).toBe(TOTAL);

  fireEvent.click(screen.getByText(`审核单 #${TARGET}`));
  await waitFor(() => expect(activeReviewTitle(view.container)).toBe(`审核单 #${TARGET}`));
  return view;
}

describe('AI 分组审阅：处置单个候选合集', () => {
  const cases = [
    {
      name: '应用',
      act: () => fireEvent.click(screen.getByText('应用此合集')),
      // 反向判据：动作真的落地了，卡片报出新建的合集（提示条上也有同一句话）。
      settled: () => screen.findAllByText(`已创建合集 #${CREATED_COLLECTION_ID}`),
    },
    {
      name: '拒绝',
      act: () => fireEvent.click(screen.getByText('拒绝此合集')),
      settled: async () => {
        await waitFor(() => expect(screen.getAllByText('已拒绝').length).toBeGreaterThan(0));
      },
    },
    {
      name: '编辑后保存',
      act: () => {
        fireEvent.click(screen.getByText('编辑'));
        const input = screen.getByDisplayValue(`候选合集 ${TARGET}`);
        fireEvent.change(input, { target: { value: '改过的候选合集' } });
        fireEvent.click(screen.getByText('保存'));
      },
      settled: () => screen.findByText('改过的候选合集'),
    },
  ];

  for (const testCase of cases) {
    it(`${testCase.name}之后，左栏累积的条数不缩水、右栏不跳回队首`, async () => {
      const { container } = await setup();

      testCase.act();
      await testCase.settled();

      expect(listRowCount(container)).toBe(TOTAL);
      expect(activeReviewTitle(container)).toBe(`审核单 #${TARGET}`);
    });
  }
});
