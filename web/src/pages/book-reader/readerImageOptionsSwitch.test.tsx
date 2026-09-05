/**
 * @vitest-environment jsdom
 *
 * 守「换一套取图参数时，正在看的那一页不会停在转圈上」。转屏、拖窗口跨档、切适应模式都会重算
 * 目标尺寸档位：档位不进请求地址时不得清缓存，进了地址则必须把当前页重新取回来。破了的表现是
 * 当前页永久转圈（后面几页反而有图），只有手动翻页才恢复。
 */

import { act, cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { LocaleProvider } from '../../i18n/LocaleProvider';
import { ToastProvider } from '../../components/ToastProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import BookReader from '../BookReader';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));

vi.mock('../../auth/AuthProvider', () => ({ useAuth: () => ({ isAdmin: false }) }));

vi.mock('../../api/client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    defaults: { headers: { common: {} as Record<string, string> } },
  },
  isAxiosError: (err: unknown) => Boolean((err as { isAxiosError?: boolean })?.isAxiosError),
  isCancel: () => false,
  getApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

// 离线那一层整层替掉：本文件测的是取图缓存，不需要 Cache Storage。
vi.mock('./offlineReader', () => ({
  supportsOfflineReaderCache: () => false,
  cacheBookForOffline: vi.fn(),
  deleteOfflineBook: vi.fn(),
  getOfflineBookStatus: async () => null,
  getQueuedOfflineProgress: () => null,
  queueOfflineProgress: () => {},
  syncQueuedOfflineProgress: async () => ({ synced: 0, failed: 0, remaining: 0 }),
}));

interface PendingImage {
  url: string;
  resolve: () => void;
}

let pending: PendingImage[] = [];
let urlSeq = 0;

function notFound() {
  return Object.assign(new Error('not found'), { isAxiosError: true, response: { status: 404 } });
}

function mockApi() {
  mocks.get.mockImplementation(((url: string) => {
    if (url === '/api/pages/42') {
      return Promise.resolve({ data: [{ number: 1 }, { number: 2 }, { number: 3 }] });
    }
    if (url === '/api/book-info/42') {
      return Promise.resolve({
        data: {
          id: 42,
          name: '第 1 话',
          series_id: 7,
          volume: '1',
          last_read_page: { Valid: true, Int64: 1 },
        },
      });
    }
    if (url.startsWith('/api/series/')) {
      return Promise.resolve({ data: { series: { id: 7 }, books: [{ id: 42, name: '第 1 话', volume: '1' }] } });
    }
    return Promise.reject(notFound());
  }) as never);
  mocks.post.mockResolvedValue({ data: {} });
}

function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: width });
}

// flush 把已经排好的微任务链跑干净。次数是常数，不看机器快慢。
async function flush() {
  await act(async () => {
    for (let i = 0; i < 12; i += 1) {
      await Promise.resolve();
    }
  });
}

// settleImages 让当前全部在途页图请求成功返回。
async function settleImages() {
  const batch = pending.splice(0);
  await act(async () => {
    batch.forEach((request) => request.resolve());
    for (let i = 0; i < 12; i += 1) {
      await Promise.resolve();
    }
  });
}

function currentPageSrc() {
  return screen.queryByAltText('Page 1')?.getAttribute('src') ?? null;
}

beforeEach(() => {
  vi.useFakeTimers();
  localStorage.clear();
  // 翻页模式 + 基础主题：条漫走虚拟滚动，判据落不到「当前页」这一个元素上。
  localStorage.setItem('manga_read_mode', JSON.stringify('paged'));
  localStorage.setItem('manga_reader_theme', JSON.stringify('base'));
  // 预加载关掉：坏掉的是当前页，后面几页只是同一个根因的旁证。
  localStorage.setItem('manga_preload_count', JSON.stringify(0));
  pending = [];
  urlSeq = 0;
  setViewportWidth(1024);
  mockApi();

  vi.stubGlobal('URL', {
    ...URL,
    createObjectURL: () => {
      urlSeq += 1;
      return `blob:mock/${urlSeq}`;
    },
    revokeObjectURL: () => {},
  });
  vi.stubGlobal('fetch', (url: string) => new Promise((resolve) => {
    pending.push({
      url,
      resolve: () => resolve({ ok: true, status: 200, blob: async () => ({}) } as unknown as Response),
    });
  }));
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function renderReader() {
  return render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter initialEntries={['/reader/42']}>
          <Routes>
            <Route path="/reader/:bookId" element={<BookReader />} />
          </Routes>
        </MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
}

// resizeAcrossStep 把窗口宽度拖过一个 256 档位，并跑完档位防抖。
async function resizeAcrossStep() {
  await act(async () => {
    setViewportWidth(1300);
    window.dispatchEvent(new Event('resize'));
    await vi.advanceTimersByTimeAsync(400);
  });
  await flush();
}

async function openReaderAtFirstPage() {
  renderReader();
  await flush();
  await settleImages();
  expect(currentPageSrc()).toBe('blob:mock/1');
}

describe('拖窗口跨档之后的当前页', () => {
  it('档位不进请求地址时不清缓存，当前页原地不动', async () => {
    // 默认滤镜不是重采样滤镜，档位根本不写进 URL：清掉整份缓存换不来任何一个新地址。
    await openReaderAtFirstPage();

    await resizeAcrossStep();

    expect(currentPageSrc()).toBe('blob:mock/1');
    expect(pending).toHaveLength(0);
  });

  it('档位进了请求地址时旧图作废，但当前页要被重新取回来', async () => {
    localStorage.setItem('manga_image_filter', JSON.stringify('lanczos3'));
    await openReaderAtFirstPage();

    await resizeAcrossStep();

    // 新档位是一个新地址，必须真的发出去；否则当前页就停在转圈上，
    // 而清空与预热的先后顺序决定它是否被当成迟到响应丢弃。
    expect(pending.length).toBeGreaterThan(0);
    await settleImages();
    expect(currentPageSrc()).not.toBeNull();
    expect(currentPageSrc()).not.toBe('blob:mock/1');
  });
});
