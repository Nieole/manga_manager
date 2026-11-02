/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import type { PropsWithChildren } from 'react';
import { apiClient } from '../api/client';
import { ApplicationSessionProvider, useLibraryRefresh } from './ApplicationSessionProvider';
import { useSeriesContext } from '../pages/series-detail/hooks/useSeriesContext';
import { useSeriesMetadataReview } from '../pages/series-detail/hooks/useSeriesMetadataReview';

const original = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = original; vi.restoreAllMocks(); vi.unstubAllGlobals(); });
function wrapper({ children }: PropsWithChildren) {
  return <ApplicationSessionProvider identity="1:admin" isAdmin>{children}</ApplicationSessionProvider>;
}
function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function setup(applied = true) {
  vi.stubGlobal('EventSource', class { close() {} });
  vi.spyOn(console, 'error').mockImplementation(() => {});
  const write = deferred();
  const refresh = deferred();
  const nextWrite = deferred();
  let committed = false;
  let refreshing = false;
  let writes = 0;
  apiClient.defaults.adapter = async config => {
    let data: unknown;
    if (config.method === 'post') {
      writes++;
      await (writes === 1 ? write.promise : nextWrite.promise);
      committed = true;
      data = { applied };
    } else {
      const id = Number(/series\/(\d+)\/context/.exec(config.url ?? '')?.[1]);
      if (committed && id === 8) { refreshing = true; await refresh.promise; }
      data = { series: { id, library_id: 1, name: `Series ${id}` }, metadata_version: committed ? 'v2' : 'v1', books: [] };
    }
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  const showToast = vi.fn();
  const view = renderHook(({ id }) => {
    const ctx = useSeriesContext({ seriesId: id });
    const review = useSeriesMetadataReview({ seriesId: id, reload: ctx.reload, showToast, t: key => key });
    return { ctx, review, catalog: useLibraryRefresh(), reviews: useLibraryRefresh({ area: 'reviews' }) };
  }, { initialProps: { id: '8' }, wrapper });
  return { ...view, write, nextWrite, refresh, showToast, writes: () => writes, refreshing: () => refreshing };
}

it.each(['apply', 'reject'] as const)('warns about committed %s with failed authoritative refresh, then retries only the query', async actionName => {
  const view = setup();
  await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(8));
  let action!: Promise<void>;
  act(() => { action = view.result.current.review[actionName](34); });
  await act(async () => view.write.resolve());
  await waitFor(() => expect(view.refreshing()).toBe(true));
  expect(view.result.current.catalog).toBe(0);
  expect(view.result.current.reviews).toBe(0);
  expect(view.showToast).not.toHaveBeenCalled();
  await act(async () => { view.refresh.reject(new Error('context unavailable')); await action; });
  expect(view.showToast.mock.calls).toEqual([[actionName === 'apply' ? 'series.toast.savedRefreshFailed' : 'series.toast.metadataReviewRejectedRefreshFailed', 'error']]);
  expect(view.result.current.catalog > 0).toBe(actionName === 'apply');
  expect(view.result.current.reviews).toBeGreaterThan(0);
  expect(view.result.current.ctx.error).not.toBeNull();
  act(() => view.result.current.ctx.retry());
  await waitFor(() => expect(view.result.current.ctx.loading).toBe(false));
  expect(view.writes()).toBe(1);
});

for (const actionName of ['apply', 'reject'] as const) {
  for (const phase of ['write', 'refresh'] as const) {
    for (const outcome of ['success', 'write failure', 'refresh failure'] as const) {
      if ((phase === 'refresh' && outcome === 'write failure') || (phase === 'write' && outcome === 'refresh failure')) continue;
      it(`isolates late ${actionName} ${outcome} after switching series during ${phase}`, async () => {
        const view = setup();
        await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(8));
        const staleAction = view.result.current.review[actionName];
        let action!: Promise<void>;
        act(() => { action = staleAction(34); });
        if (phase === 'refresh') {
          await act(async () => view.write.resolve());
          await waitFor(() => expect(view.refreshing()).toBe(true));
        }
        view.rerender({ id: '9' });
        await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(9));
        expect(view.result.current.review.busyMetadataReviewId).toBeNull();
        await act(async () => {
          if (outcome === 'write failure') view.write.reject(new Error('write unavailable'));
          else view.write.resolve();
          if (outcome === 'refresh failure' && phase === 'refresh') view.refresh.reject(new Error('context unavailable'));
          else view.refresh.resolve();
          await action;
        });
        expect(view.result.current.ctx.series?.id).toBe(9);
        expect(view.result.current.ctx.error).toBeNull();
        expect(view.showToast).not.toHaveBeenCalled();
        expect(view.result.current.catalog > 0).toBe(outcome !== 'write failure' && actionName === 'apply');
        expect(view.result.current.reviews > 0).toBe(outcome !== 'write failure');
        await act(async () => staleAction(34));
        expect(view.writes()).toBe(1);
      });
    }
  }
}

for (const actionName of ['apply', 'reject'] as const) {
  it(`${actionName} write failure does not invalidate or refresh; an explicit write retry can succeed`, async () => {
    const view = setup();
    await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(8));
    let action!: Promise<void>;
    act(() => { action = view.result.current.review[actionName](34); });
    await act(async () => { view.write.reject(new Error('write unavailable')); await action; });
    expect(view.result.current.catalog).toBe(0);
    expect(view.result.current.reviews).toBe(0);
    expect(view.refreshing()).toBe(false);
    expect(view.showToast.mock.calls).toEqual([[expect.stringContaining(actionName === 'apply' ? 'metadataReviewApplyFailed' : 'metadataReviewRejectFailed'), 'error']]);
    expect(view.result.current.review.busyMetadataReviewId).toBeNull();
    view.showToast.mockClear();
    act(() => { action = view.result.current.review[actionName](34); });
    await act(async () => view.nextWrite.resolve());
    await waitFor(() => expect(view.refreshing()).toBe(true));
    await act(async () => { view.refresh.resolve(); await action; });
    expect(view.result.current.ctx.metadataVersion).toBe('v2');
    expect(view.showToast.mock.calls).toEqual([[actionName === 'apply' ? 'series.toast.metadataReviewApplied' : 'series.toast.metadataReviewRejected', 'success']]);
    expect(view.result.current.catalog > 0).toBe(actionName === 'apply');
    expect(view.result.current.reviews).toBeGreaterThan(0);
    expect(view.writes()).toBe(2);
  });

  it(`${actionName} cannot duplicate a pending write, and its late completion cannot clear the new target's busy state`, async () => {
    const view = setup();
    await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(8));
    let oldAction!: Promise<void>;
    act(() => { oldAction = view.result.current.review[actionName](34); });
    await act(async () => view.result.current.review[actionName](34));
    expect(view.writes()).toBe(1);
    view.rerender({ id: '9' });
    await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(9));
    let newAction!: Promise<void>;
    act(() => { newAction = view.result.current.review[actionName](35); });
    expect(view.result.current.review.busyMetadataReviewId).toBe(35);
    await act(async () => { view.write.resolve(); await oldAction; });
    expect(view.result.current.review.busyMetadataReviewId).toBe(35);
    expect(view.showToast).not.toHaveBeenCalled();
    await act(async () => { view.nextWrite.resolve(); await newAction; });
    expect(view.result.current.review.busyMetadataReviewId).toBeNull();
    expect(view.writes()).toBe(2);
    expect(view.showToast.mock.calls).toEqual([[actionName === 'apply' ? 'series.toast.metadataReviewApplied' : 'series.toast.metadataReviewRejected', 'success']]);
  });
}

it('does not declare a metadata commit when apply reports no changes and the query fails', async () => {
  const view = setup(false);
  await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(8));
  let action!: Promise<void>;
  act(() => { action = view.result.current.review.apply(34); });
  await act(async () => view.write.resolve());
  await waitFor(() => expect(view.refreshing()).toBe(true));
  await act(async () => { view.refresh.reject(new Error('context unavailable')); await action; });
  expect(view.showToast.mock.calls).toEqual([['series.content.loadFailed', 'error']]);
  expect(view.result.current.catalog).toBe(0);
  expect(view.result.current.reviews).toBe(0);
  expect(view.writes()).toBe(1);
});

it('a failed real context GET after apply must not be swallowed into an ordinary success toast', async () => {
  const view = setup();
  await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(8));
  let action!: Promise<void>;
  act(() => { action = view.result.current.review.apply(34); });
  await act(async () => view.write.resolve());
  await waitFor(() => expect(view.refreshing()).toBe(true));
  await act(async () => { view.refresh.reject(new Error('context unavailable')); await action; });
  expect(view.writes()).toBe(1);
  expect(view.result.current.ctx.error).not.toBeNull();
  expect(view.showToast.mock.calls).toEqual([['series.toast.savedRefreshFailed', 'error']]);
});
