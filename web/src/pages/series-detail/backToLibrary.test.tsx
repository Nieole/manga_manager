/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「返回资源库」一步到位：从资料库卡片进详情、再点一次返回，就必须回到资料库。
 * 两条前提缺一不可，所以一起守在这里——
 * 一是一次卡片点击只能压一条历史条目：卡片本身是 <Link>，若 onClick 里再 navigate 一次，
 * 浏览器后退与页面返回都要按两次才动，而症状是「第一次点击没反应」，极难从表象追回卡片。
 * 二是返回的落点必须由 library_id 直接算出，不能靠回退一步猜：按钮写着「返回资源库」，
 * 从搜索结果、关系图、合集进详情时上一页并不是资料库，回退一步会去到别处。
 *
 * 这里用 BrowserRouter 而不是 MemoryRouter：历史条目数正是被守的对象，只有真实的
 * window.history 才会暴露多压的那一条。
 */

import { useCallback } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { BrowserRouter, Route, Routes, useParams } from 'react-router-dom';

import { apiClient } from '../../api/client';
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
function mockApi() {
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
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
          <Routes>
            <Route path="/library/:libId" element={<LibraryRoute />} />
            <Route path="/series/:seriesId" element={<SeriesDetailPage />} />
          </Routes>
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
