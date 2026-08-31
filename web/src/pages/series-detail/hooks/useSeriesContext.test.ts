/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「详情页上的内容，永远属于地址栏此刻指向的那个系列」。
 * 在关系图、合集、作品群里连点系列即可让 A 的 /context 请求在飞时就跳到 B；破了的话
 * 地址栏是 B、简介与书列表却是 A 的，用户在这页上做批量已读就把进度写到了 A 的书上。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

import { apiClient } from '../../../api/client';
import type { MetadataReview, SeriesContextResponse } from '../types';
import { useSeriesContext } from './useSeriesContext';
import { useSeriesEdit } from './useSeriesEdit';

function makeReview(seriesId: number): MetadataReview {
  return {
    id: seriesId,
    series_id: seriesId,
    provider: 'p',
    source_url: '',
    source_id: 0,
    source_query: '',
    summary: `系列${seriesId}提案`,
    confidence: 1,
    status: 'pending',
    raw_payload: '',
    created_at: '',
    updated_at: '',
    fields: [],
  };
}

function contextOf(seriesId: number): SeriesContextResponse {
  return {
    series: {
      id: seriesId,
      name: `系列${seriesId}`,
      library_id: 1,
      path: `/lib/${seriesId}`,
      summary: { String: `系列${seriesId}简介`, Valid: true },
      book_count: 1,
      locked_fields: { String: '', Valid: false },
    },
    books: [{ id: seriesId * 100, name: `系列${seriesId}-book`, library_id: 1, volume: '', page_count: 10 }],
    tags: [{ id: seriesId, name: `标签${seriesId}` }],
    authors: [{ id: seriesId, name: `作者${seriesId}`, role: 'author' }],
    links: [],
    metadata_review: { reviews: [makeReview(seriesId)], provenance: [] },
  };
}

// /context 一律挂起，由用例决定哪个系列的响应先到——这就是本文件要复现的那个顺序。
interface PendingRequest {
  url: string;
  settle: () => void;
}

let pending: PendingRequest[] = [];

function mockGet() {
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) =>
    new Promise((resolve) => {
      const id = Number(/^\/api\/series\/(\d+)\/context$/.exec(url)?.[1]);
      pending.push({ url, settle: () => resolve({ data: Number.isNaN(id) ? [] : contextOf(id) }) });
    })) as never);
}

/** 放行某个系列此刻在飞的 /context，模拟它的响应到达。 */
async function deliver(seriesId: number) {
  const url = `/api/series/${seriesId}/context`;
  const hits = pending.filter((p) => p.url === url);
  pending = pending.filter((p) => p.url !== url);
  await act(async () => {
    hits.forEach((p) => p.settle());
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

function renderContext(seriesId: string) {
  return renderHook(({ id }: { id: string }) => useSeriesContext({ seriesId: id, refreshTrigger: 0 }), {
    initialProps: { id: seriesId },
  });
}

afterEach(() => {
  pending = [];
  vi.restoreAllMocks();
});

describe('useSeriesContext 快速切换系列', () => {
  it('A 的响应晚于 B 到达时，页面内容仍属于 B', async () => {
    mockGet();
    const { result, rerender } = renderContext('1');

    // A 还没回来就跳到 B
    rerender({ id: '2' });
    await deliver(2);
    await deliver(1);

    expect(result.current.series?.id).toBe(2);
    expect(result.current.series?.summary?.String).toBe('系列2简介');
    expect(result.current.books.map((b) => b.name)).toEqual(['系列2-book']);
    expect(result.current.tags.map((tag) => tag.name)).toEqual(['标签2']);
    expect(result.current.authors.map((a) => a.name)).toEqual(['作者2']);
    expect(result.current.metadataReviews.map((r) => r.summary)).toEqual(['系列2提案']);
  });

  it('切系列期间处于加载态，且不再挂着上一个系列的内容', async () => {
    mockGet();
    const { result, rerender } = renderContext('1');
    await deliver(1);
    expect(result.current.loading).toBe(false);
    expect(result.current.series?.id).toBe(1);

    rerender({ id: '2' });
    // 详情页的加载判据是 loading && !series：两者都必须到位，否则用户看到的是 A 的内容
    // 且没有任何加载指示，完全不知道页面在切换。
    expect(result.current.loading).toBe(true);
    expect(result.current.series).toBeNull();
    expect(result.current.books).toEqual([]);

    // A 迟到的响应既不能写内容，也不能把 B 的加载态清掉
    await deliver(1);
    expect(result.current.loading).toBe(true);
    expect(result.current.series).toBeNull();

    await deliver(2);
    expect(result.current.loading).toBe(false);
    expect(result.current.series?.id).toBe(2);
  });

  it('切系列后新上下文未到达时，保存不会把上一个系列的内容打到新系列 id 上', async () => {
    mockGet();
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ data: {} } as never);
    const showToast = vi.fn();
    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => {
        const ctx = useSeriesContext({ seriesId: id, refreshTrigger: 0 });
        const edit = useSeriesEdit({
          seriesId: id,
          series: ctx.series,
          tags: ctx.tags,
          authors: ctx.authors,
          links: ctx.links,
          reload: ctx.reload,
          showToast,
          t: (key: string) => key,
        });
        return { ctx, edit };
      },
      { initialProps: { id: '1' } },
    );
    await deliver(1);

    // seriesId 跳到 B 的瞬间点保存：此刻编辑表单里还是 A 的 title/summary/tags。
    rerender({ id: '2' });
    await act(async () => {
      // 不等 save 跑完：破了的话它会停在 PUT 之后的 reload 上，而此时 PUT 已经发出去了。
      void result.current.edit.save();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(put).not.toHaveBeenCalled();
  });
});
