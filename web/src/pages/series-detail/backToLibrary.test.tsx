/**
 * @vitest-environment jsdom
 *
 * 守「返回资源库」一步到位。两条前提缺一不可：一次卡片点击只压一条历史（卡片是 <Link>，
 * onClick 再 navigate 一次就得按两下），落点由 library_id 直接算出（回退一步在搜索、
 * 关系图等入口会去到别处）。用 BrowserRouter 而非 MemoryRouter——被守的正是历史条目数。
 */

import { useCallback } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { BrowserRouter, Route, Routes, useParams } from 'react-router-dom';

import { apiClient } from '../../api/client';
import { AuthProvider } from '../../auth/AuthProvider';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import { ToastProvider } from '../../components/ToastProvider';
import { __resetScrapeProvidersCacheForTest } from '../../hooks/useScrapeProviders';
import { LibraryCard } from '../library/LibraryCard';
import { useLibraryCardActions } from '../library/hooks/useLibraryCardActions';
import {
  useLibraryFilters,
  type UseLibraryFiltersResult,
} from '../library/hooks/useLibraryFilters';
import type { Series as CardSeries } from '../library/types';
import type { SeriesContextResponse } from './types';
import SeriesDetailPage from './index';

const LIBRARY_ID = 1;
const SERIES_ID = 1;
const SERIES_NAME = '测试系列';

const CARD_SERIES: CardSeries = {
  id: SERIES_ID,
  name: SERIES_NAME,
  volume_count: 1,
  actual_book_count: 1,
  read_count: 0,
  total_pages: { Valid: true, Float64: 10 },
  is_favorite: false,
};

const CONTEXT: SeriesContextResponse = {
  series: {
    id: SERIES_ID,
    name: SERIES_NAME,
    library_id: LIBRARY_ID,
    path: '/lib/1',
    book_count: 1,
    locked_fields: { String: '', Valid: false },
  },
  books: [
    {
      id: 101,
      name: '书101',
      library_id: LIBRARY_ID,
      volume: '',
      page_count: 10,
      last_read_page: { Valid: false, Int64: 0 },
    },
  ],
  tags: [],
  authors: [],
  links: [],
};

// 资料库列表的检索接口只用来让筛选状态活起来，卡片本身由用例直接给定。
// 详情页与资料库卡片上的动作按 isAdmin 决定渲不渲染，本文件守的是导航，故一律以管理员登录。
function mockApi() {
  const admin = {
    setup_required: false,
    authenticated: true,
    csrf_token: 'csrf',
    user: { id: 1, username: 'admin', role: 'admin', display_name: 'admin', must_change_password: false },
  };
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
    if (url.startsWith('/api/auth/status')) return Promise.resolve({ data: admin });
    if (url.includes('/context')) return Promise.resolve({ data: CONTEXT });
    if (url.includes('/api/series/search')) {
      return Promise.resolve({ data: { items: [CARD_SERIES], total: 1, next_cursor: '' } });
    }
    return Promise.resolve({ data: [] });
  }) as never);
  vi.spyOn(apiClient, 'post').mockResolvedValue({ data: {} } as never);
}

// 用例读回筛选状态，验证返回资料库后筛选与页码还在。
let latestFilters: UseLibraryFiltersResult | null = null;

// 资料库路由的最小接线：真正的 useLibraryFilters + useLibraryCardActions + LibraryCard，
// 三者正是「点一张卡片会压几条历史」的实际调用点。
function LibraryRoute() {
  const { libId } = useParams<{ libId: string }>();
  const filters = useLibraryFilters({ libId });
  latestFilters = filters;
  const { rescanningId, handleCardClick, handleToggleFavorite, handleRescanSeries } =
    useLibraryCardActions({
      isSelectionMode: false,
      toggleSelectSeries: useCallback(() => {}, []),
      patchSeries: useCallback(() => {}, []),
      refreshLoadedSeries: useCallback(() => {}, []),
      showError: useCallback(() => {}, []),
      showToast: useCallback(() => {}, []),
      t: useCallback((key: string) => key, []),
    });
  return (
    <LibraryCard
      series={CARD_SERIES}
      isSelectionMode={false}
      isSelected={false}
      rescanning={rescanningId === CARD_SERIES.id}
      scrapingActive={false}
      scrapeMenuOpen={false}
      onCardClick={handleCardClick}
      onToggleFavorite={handleToggleFavorite}
      onRescan={handleRescanSeries}
      onOpenScrapeMenu={() => {}}
      onCloseScrapeMenu={() => {}}
      onChooseScrapeProvider={() => {}}
      externalSessionActive={false}
      canManage
    />
  );
}

// 词条同步传入，避免异步加载语言包让按钮文案在「键名」和「中文」之间抖动。
function renderApp(entry: string) {
  window.history.replaceState(null, '', entry);
  render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <BrowserRouter>
          <AuthProvider>
            <Routes>
              <Route path="/library/:libId" element={<LibraryRoute />} />
              <Route path="/series/:seriesId" element={<SeriesDetailPage />} />
            </Routes>
          </AuthProvider>
        </BrowserRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
}

// react-router 把历史下标记在 history.state.idx 上；进入时 replaceState(null) 让它从 0 起算。
const historyIndex = () => (window.history.state as { idx?: number } | null)?.idx ?? 0;

const clickCard = () => fireEvent.click(screen.getByRole('link', { name: new RegExp(SERIES_NAME) }));
const clickBack = () => fireEvent.click(screen.getByRole('button', { name: '返回资源库' }));

// 详情页的书籍要等 /context 落地才渲染，用它当「已经站在详情页」的判据。
const waitForDetailPage = () => screen.findByRole('button', { name: '进入批量选择' });
const waitForLibraryPage = () =>
  waitFor(() => expect(window.location.pathname).toBe(`/library/${LIBRARY_ID}`));

// vitest 没开 globals，testing-library 的自动清理不会生效。
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  window.localStorage.clear();
  latestFilters = null;
  __resetScrapeProvidersCacheForTest();
});

beforeEach(() => {
  window.localStorage.clear();
  mockApi();
});

describe('系列详情的返回资源库', () => {
  it('点一张资料库卡片只压一条历史条目', async () => {
    renderApp(`/library/${LIBRARY_ID}`);
    const before = historyIndex();

    clickCard();

    await waitForDetailPage();
    expect(historyIndex()).toBe(before + 1);
  });

  it('从资料库卡片进详情后，点一次返回就回到资料库', async () => {
    renderApp(`/library/${LIBRARY_ID}`);
    clickCard();
    await waitForDetailPage();

    clickBack();

    await waitForLibraryPage();
    expect(screen.getByRole('link', { name: new RegExp(SERIES_NAME) })).toBeTruthy();
  });

  it('深链直接打开详情页时，返回落到该系列所属的资料库', async () => {
    renderApp(`/series/${SERIES_ID}`);
    await waitForDetailPage();

    clickBack();

    await waitForLibraryPage();
  });

  it('返回资料库后筛选与页码仍在', async () => {
    renderApp(`/library/${LIBRARY_ID}?status=ongoing&page=3`);
    await waitFor(() => expect(latestFilters?.page).toBe(3));
    // 筛选落盘有 400ms 节流，等它写完再离开，才是用户浏览一会儿再点进去的真实节奏。
    await new Promise((resolve) => setTimeout(resolve, 450));

    clickCard();
    await waitForDetailPage();
    clickBack();
    await waitForLibraryPage();

    await waitFor(() => {
      expect(latestFilters?.page).toBe(3);
      expect(latestFilters?.activeStatus).toBe('ongoing');
    });
    await waitFor(() => expect(window.location.search).toContain('status=ongoing'));
    expect(window.location.search).toContain('page=3');
  });
});
