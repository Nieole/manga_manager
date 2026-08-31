/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「刮削弹窗里的候选与对比列，永远属于弹窗此刻指向的那个系列」。
 * 资料库页只禁用发起刮削的那张卡片，用户可以在 A 的请求在飞时再对 B 点一次；破了的话
 * 弹窗指向 B、候选却是 A 的，用户挑一条应用就给 B 生成了一条属于 A 的提案。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

import { apiClient } from '../../../api/client';
import type { MetaTag, SearchResult, Series as DetailSeries } from '../../series-detail/types';
import type { Series } from '../types';
import { useSeriesScraping } from './useSeriesScraping';

function makeCard(id: number): Series {
  return {
    id,
    name: `系列${id}`,
    volume_count: 0,
    actual_book_count: 0,
    read_count: 0,
    total_pages: { Float64: 0, Valid: false },
    is_favorite: false,
  };
}

function makeDetail(id: number): DetailSeries {
  return {
    id,
    name: `系列${id}`,
    library_id: 1,
    path: `/lib/${id}`,
    book_count: 0,
    locked_fields: { String: '', Valid: false },
  };
}

function makeCandidate(title: string): SearchResult {
  return {
    Title: title,
    OriginalTitle: title,
    Summary: '',
    Publisher: '',
    CoverURL: '',
    Rating: 0,
    Tags: [],
    SourceID: 1,
    ReleaseDate: '',
    VolumeCount: 0,
  };
}

function bodyFor(url: string): DetailSeries | MetaTag[] | { results: SearchResult[]; total: number } {
  const id = Number(/^\/api\/series\/(\d+)/.exec(url)?.[1]);
  if (url.includes('/scrape-search')) return { results: [makeCandidate(`系列${id}候选`)], total: 1 };
  if (url.includes('/tags')) return [{ id, name: `标签${id}` }];
  return makeDetail(id);
}

// 每个 GET 都挂起，由用例决定哪个系列的响应先到——这就是本文件要复现的那个顺序。
interface PendingRequest {
  url: string;
  settle: () => void;
}

let pending: PendingRequest[] = [];

function mockGet() {
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) =>
    new Promise((resolve) => {
      pending.push({ url, settle: () => resolve({ data: bodyFor(url) }) });
    })) as never);
}

/** 放行某个系列此刻在飞的全部请求，模拟它的响应到达。 */
async function deliver(seriesId: number) {
  const matcher = new RegExp(`^/api/series/${seriesId}(?![0-9])`);
  const hits = pending.filter((p) => matcher.test(p.url));
  pending = pending.filter((p) => !matcher.test(p.url));
  await act(async () => {
    hits.forEach((p) => p.settle());
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

function renderScraping() {
  const onError = vi.fn();
  const onSuccess = vi.fn();
  const view = renderHook(() => useSeriesScraping({ onSuccess, onError }));
  return { ...view, onError, onSuccess };
}

afterEach(() => {
  pending = [];
  vi.restoreAllMocks();
});

describe('useSeriesScraping 并发刮削', () => {
  it('A 的响应晚于 B 到达时，弹窗里的候选与对比列仍属于 B', async () => {
    mockGet();
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ data: { queued: true } } as never);
    const { result } = renderScraping();

    await act(async () => {
      void result.current.startScrape(makeCard(1), 'p1');
    });
    await act(async () => {
      void result.current.startScrape(makeCard(2), 'p2');
    });

    await deliver(2);
    await deliver(1);

    expect(result.current.scrapingSeries?.id).toBe(2);
    expect(result.current.scrapeSeriesDetail?.id).toBe(2);
    expect(result.current.scrapeSearchResults.map((r) => r.Title)).toEqual(['系列2候选']);
    expect(result.current.scrapeCurrentTags.map((tag) => tag.name)).toEqual(['标签2']);

    // 用户在弹窗里挑一条应用：提案必须由 B 的候选生成，并且落在 B 上。
    await act(async () => {
      await result.current.applyScrape({ title: result.current.scrapeSearchResults[0].Title });
    });
    expect(post).toHaveBeenCalledWith('/api/series/2/scrape-apply?provider=p2', { title: '系列2候选' });
  });

  it('重搜的响应晚于新一次刮削到达时，不覆盖新目标的候选', async () => {
    mockGet();
    const { result } = renderScraping();

    await act(async () => {
      void result.current.startScrape(makeCard(2), 'p2');
    });
    await deliver(2);

    await act(async () => {
      void result.current.reSearch();
    });
    await act(async () => {
      void result.current.startScrape(makeCard(3), 'p3');
    });
    await deliver(3);
    await deliver(2);

    expect(result.current.scrapingSeries?.id).toBe(3);
    expect(result.current.scrapeSearchResults.map((r) => r.Title)).toEqual(['系列3候选']);
  });

  it('陈旧响应到达时不清掉加载态，也不提前打开弹窗', async () => {
    mockGet();
    const { result } = renderScraping();

    await act(async () => {
      void result.current.startScrape(makeCard(1), 'p1');
    });
    await act(async () => {
      void result.current.startScrape(makeCard(2), 'p2');
    });

    await deliver(1);

    // B 还在飞：加载态必须继续转，弹窗不得带着 A 的内容先开出来。
    expect(result.current.isScraping).toBe(true);
    expect(result.current.showScrapeModal).toBe(false);
    expect(result.current.scrapeSeriesDetail).toBeNull();
  });
});
