/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { useLibrarySeries } from '../pages/library/hooks/useLibrarySeries';
import { useLibrarySelection } from '../pages/library/hooks/useLibrarySelection';
import { EMPTY_ADVANCED_FILTERS } from '../pages/library/hooks/libraryFilterParams';
import { useSeriesProgress } from '../pages/series-detail/hooks/useSeriesProgress';
import { useReadStateMutation } from '../reading/useReadStateMutation';
import { apiClient } from '../api/client';
import { AuthProvider } from '../auth/AuthProvider';
import { LocaleProvider } from '../i18n/LocaleProvider';
import { messages } from '../i18n/locales/zh-CN';
import { ToastProvider } from '../components/ToastProvider';
import { ApplicationSessionProvider } from './ApplicationSessionProvider';
import MetadataReviews from '../pages/MetadataReviews';
import { useLibraryRefresh } from './ApplicationSessionProvider';
import Collections from '../pages/collections';
import { useSeriesContext } from '../pages/series-detail/hooks/useSeriesContext';
import { useSeriesEdit } from '../features/series-metadata/useSeriesEdit';
import type { PropsWithChildren } from 'react';
class Source {
  static current: Source;
  onmessage: ((event: { data: string }) => void) | null = null;
  constructor() { Source.current = this; }
  close() {}
}
const original = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = original; vi.unstubAllGlobals(); localStorage.clear(); });
function wrapper({ children }: PropsWithChildren) { return <MemoryRouter><LocaleProvider initialLocale="zh-CN" initialMessages={messages} fallbackMessages={messages}><ToastProvider><AuthProvider><ApplicationSessionProvider identity="1:admin" isAdmin>{children}</ApplicationSessionProvider></AuthProvider></ToastProvider></LocaleProvider></MemoryRouter>; }
function refresh() { act(() => Source.current.onmessage?.({ data: 'refresh' })); }
it('scan refresh updates selected collection members and preserves the unsaved creation form', async () => {
  vi.stubGlobal('EventSource', Source);
  let title = 'Original member';
  const requests: string[] = [];
  apiClient.defaults.adapter = async config => {
    const url = config.url ?? ''; requests.push(url);
    const data = url === '/api/auth/status' ? { authenticated: true, setup_required: false, user: { id: 1, role: 'admin' } }
      : url === '/api/collection-views' ? [{ view_id: 'collection:3', kind: 'collection', numeric_id: 3, name: 'Chosen collection', series_count: 1 }]
        : url.includes('/collections/3/series') ? [{ series_id: 8, series_name: title, book_count: 2 }] : [];
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  render(<Collections />, { wrapper });
  fireEvent.click(await screen.findByText('Chosen collection'));
  await screen.findByText('Original member');
  fireEvent.click(await screen.findByText(messages['collections.create']));
  const name = await screen.findByPlaceholderText(messages['collections.namePlaceholder']);
  fireEvent.change(name, { target: { value: 'Unsaved title' } });
  title = 'Refreshed member';
  const before = requests.length;
  refresh();
  await screen.findByText('Refreshed member');
  expect((name as HTMLInputElement).value).toBe('Unsaved title');
  expect(requests.slice(before).filter(url => url.includes('/collections/3/series'))).toHaveLength(1);
});
it('session refresh preserves the series draft and its original save version', async () => {
  vi.stubGlobal('EventSource', Source);
  let summary = 'Server before';
  let version = 'v1';
  let saved: Record<string, unknown> | undefined;
  apiClient.defaults.adapter = async config => {
    let data: unknown = [];
    if (config.url === '/api/series/8/context') data = { series: { id: 8, library_id: 1, name: 'Series', title: { String: 'Title', Valid: true }, summary: { String: summary, Valid: true }, book_count: 1 }, books: [], tags: [], authors: [], links: [], metadata_version: version };
    if (config.method === 'put') saved = JSON.parse(String(config.data));
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  const showToast = vi.fn();
  const t = (key: string) => key;
  const view = renderHook(() => {
    const ctx = useSeriesContext({ seriesId: '8' });
    const edit = useSeriesEdit({ seriesId: '8', series: ctx.series, tags: ctx.tags, authors: ctx.authors, links: ctx.links, metadataVersion: ctx.metadataVersion, reload: ctx.reload, showToast, t });
    return { ctx, edit };
  }, { wrapper });
  await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(8));
  act(() => view.result.current.edit.setIsEditing(true));
  act(() => view.result.current.edit.onFormChange('summary', 'My draft'));
  summary = 'Server after'; version = 'v2';
  refresh();
  await waitFor(() => expect(view.result.current.ctx.metadataVersion).toBe('v2'));
  expect(view.result.current.edit.editForm.summary).toBe('My draft');
  await act(async () => view.result.current.edit.save());
  expect(saved?.expected_version).toBe('v1');
});

it('refreshes known committed review work even when a later operation fails', async () => {
  vi.stubGlobal('EventSource', Source);
  vi.stubGlobal('IntersectionObserver', class { observe() {} disconnect() {} unobserve() {} });
  let applied = false;
  const item = (id: number) => ({ id, series_id: id, provider: 'anilist', library_id: 1, library_name: 'Library', series_name: `Review series ${id}`, fields: [{ id, field_name: 'title', current: 'old', proposed: 'new' }], confidence: 0.9, field_count: 1, locked_field_count: 0 });
  apiClient.defaults.adapter = async config => {
    let data: unknown = [];
    if (config.method === 'post') {
      if (config.url?.endsWith('bulk-reject')) throw new Error('later write failed');
      applied = true;
      data = { applied: [1] };
    } else if (config.url?.includes('/metadata/reviews')) data = { items: applied ? [item(2)] : [item(1), item(2)], total: applied ? 1 : 2, limit: 30, offset: 0 };
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  function Projection() { return <output data-testid="catalog">{useLibraryRefresh()}</output>; }
  render(<><Projection /><MetadataReviews embedded /></>, { wrapper });
  await screen.findAllByText('Review series 1');
  fireEvent.click(screen.getAllByTitle(messages['reviewInbox.markApply'])[0]);
  fireEvent.click(screen.getAllByTitle(messages['reviewInbox.markReject'])[1]);
  fireEvent.click(screen.getByText(messages['reviewInbox.applyMarked']));
  await waitFor(() => expect(screen.getByTestId('catalog').textContent).not.toBe('0'));
  await waitFor(() => expect(screen.queryAllByText('Review series 1')).toHaveLength(0));
  expect(screen.getAllByText('Review series 2').length).toBeGreaterThan(0);
});

it('does not let an old save reload or close the new series editor', async () => {
  vi.stubGlobal('EventSource', Source);
  let finish: (() => void) | undefined;
  apiClient.defaults.adapter = async config => {
    let data: unknown = [];
    const id = Number(/series\/(\d+)\/context/.exec(config.url ?? '')?.[1]);
    if (id) data = { series: { id, library_id: 1, name: `Series ${id}`, title: { String: `Title ${id}`, Valid: true }, summary: { String: `Summary ${id}`, Valid: true }, book_count: 1 }, books: [], tags: [], authors: [], links: [], metadata_version: `v${id}` };
    if (config.method === 'put') await new Promise<void>(resolve => { finish = resolve; });
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  const showToast = vi.fn(); const t = (key: string) => key;
  const view = renderHook(({ id }: { id: string }) => {
    const ctx = useSeriesContext({ seriesId: id });
    const edit = useSeriesEdit({ seriesId: id, series: ctx.series, tags: ctx.tags, authors: ctx.authors, links: ctx.links, metadataVersion: ctx.metadataVersion, reload: ctx.reload, showToast, t });
    return { ctx, edit };
  }, { initialProps: { id: '8' }, wrapper });
  await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(8));
  act(() => view.result.current.edit.setIsEditing(true));
  let saving: Promise<void>;
  act(() => { saving = view.result.current.edit.save(); });
  await waitFor(() => expect(finish).toBeDefined());
  view.rerender({ id: '9' });
  await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(9));
  act(() => view.result.current.edit.setIsEditing(true));
  act(() => view.result.current.edit.onFormChange('summary', 'Draft for nine'));
  await act(async () => { finish?.(); await saving; });
  expect(view.result.current.ctx.series?.id).toBe(9);
  expect(view.result.current.edit.isEditing).toBe(true);
  expect(view.result.current.edit.editForm.summary).toBe('Draft for nine');
});

it('ordinary series reload and retry do not declare catalog writes', async () => {
  vi.stubGlobal('EventSource', Source);
  apiClient.defaults.adapter = async config => ({ data: { series: { id: 8 }, books: [] }, status: 200, statusText: 'OK', headers: {}, config });
  const view = renderHook(() => ({ ctx: useSeriesContext({ seriesId: '8' }), revision: useLibraryRefresh({ area: 'stats' }) }), { wrapper });
  await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(8));
  await act(async () => view.result.current.ctx.reload());
  act(() => view.result.current.ctx.retry());
  await waitFor(() => expect(view.result.current.ctx.loading).toBe(false));
  expect(view.result.current.revision).toBe(0);
});


it('publishes known partial reading commits and retains the retry identity', async () => {
  vi.stubGlobal('EventSource', Source);
  const bodies: unknown[] = [];
  apiClient.defaults.adapter = async config => {
    bodies.push(JSON.parse(String(config.data)));
    const response = { data: { committed_book_ids: [7], updated: 1, warnings: 0 }, status: bodies.length === 1 ? 500 : 200, statusText: 'result', headers: {}, config };
    if (response.status === 500) throw Object.assign(new Error('partial'), { isAxiosError: true, response });
    return response;
  };
  const view = renderHook(() => ({ mutation: useReadStateMutation(), revision: useLibraryRefresh({ area: 'stats' }) }), { wrapper });
  await act(async () => {
    const outcome = await view.result.current.mutation.markBooks([7, 8], true);
    expect(outcome).toMatchObject({ committed: true, incomplete: true });
  });
  expect(view.result.current.revision).toBeGreaterThan(0);
  await act(async () => { await view.result.current.mutation.markBooks([7, 8], true); });
  expect(bodies[1]).toEqual(bodies[0]);
});

it('reading commits survive a failed detail refresh without reporting a failed write', async () => {
  vi.stubGlobal('EventSource', Source);
  let committed = false;
  apiClient.defaults.adapter = async config => {
    if (config.method === 'post') committed = true;
    else if (committed) throw new Error('refresh unavailable');
    return { data: config.method === 'post' ? { committed_book_ids: [7], warnings: 0 } : { series: { id: 8 }, books: [] }, status: 200, statusText: 'OK', headers: {}, config };
  };
  const showToast = vi.fn();
  const view = renderHook(() => {
    const ctx = useSeriesContext({ seriesId: '8' });
    const progress = useSeriesProgress({ seriesId: '8', reload: ctx.reload, showToast, t: key => key });
    return { ctx, progress };
  }, { wrapper });
  await waitFor(() => expect(view.result.current.ctx.series?.id).toBe(8));
  await act(async () => view.result.current.progress.quickToggleBook(7, true));
  expect(showToast).toHaveBeenCalledWith('series.toast.statusUpdated / series.content.loadFailed', 'error');
  expect(showToast).not.toHaveBeenCalledWith('series.toast.actionFailed', 'error');
});


it('library query retries and projection patches preserve filters and selection without write effects', async () => {
  vi.stubGlobal('EventSource', Source);
  const urls: string[] = [];
  apiClient.defaults.adapter = async config => {
    urls.push(config.url ?? '');
    return { data: { items: [{ id: 8, name: 'Filtered series' }], total: 1 }, status: 200, statusText: 'OK', headers: {}, config };
  };
  const view = renderHook(() => {
    const list = useLibrarySeries({ libId: '1', page: 1, pageSize: 30, activeTag: 'chosen', activeAuthor: null, activeStatus: null, activeLetter: null, advanced: EMPTY_ADVANCED_FILTERS, sortByField: 'name', sortDir: 'asc', refreshTrigger: 0, enabled: true });
    const selection = useLibrarySelection({ libId: '1', allSeries: list.allSeries, onChanged: list.refreshLoadedSeries, onError: () => {} });
    return { list, selection, revision: useLibraryRefresh({ area: 'stats' }) };
  }, { wrapper });
  await waitFor(() => expect(view.result.current.list.allSeries[0]?.id).toBe(8));
  act(() => view.result.current.selection.toggleSelectSeries(8));
  act(() => view.result.current.list.refreshLoadedSeries());
  act(() => view.result.current.list.retry());
  await waitFor(() => expect(view.result.current.list.loading).toBe(false));
  act(() => view.result.current.list.patchSeries(8, { is_favorite: true }));
  expect(view.result.current.selection.selectedSeries).toEqual([8]);
  expect(view.result.current.revision).toBe(0);
  expect(urls.filter(url => url.includes('/series/search?')).every(url => url.includes('tags=chosen'))).toBe(true);
});

it('late reading completion cannot refresh or notify the newly selected series', async () => {
  vi.stubGlobal('EventSource', Source);
  let finish: (() => void) | undefined;
  apiClient.defaults.adapter = async config => {
    await new Promise<void>(resolve => { finish = resolve; });
    return { data: { committed_book_ids: [7] }, status: 200, statusText: 'OK', headers: {}, config };
  };
  const reload = vi.fn(); const showToast = vi.fn();
  const view = renderHook(({ id }) => ({ progress: useSeriesProgress({ seriesId: id, reload, showToast, t: key => key }), revision: useLibraryRefresh({ area: 'stats' }) }), { initialProps: { id: '8' }, wrapper });
  let writing: Promise<void>;
  act(() => { writing = view.result.current.progress.quickToggleBook(7, true); });
  await waitFor(() => expect(finish).toBeDefined());
  view.rerender({ id: '9' });
  await act(async () => { finish?.(); await writing; });
  expect(reload).not.toHaveBeenCalled();
  expect(showToast).not.toHaveBeenCalled();
  expect(view.result.current.progress.busy).toBe(false);
  expect(view.result.current.revision).toBeGreaterThan(0);
});

it('partial reading failure preserves the library selection and exposes committed work', async () => {
  vi.stubGlobal('EventSource', Source);
  apiClient.defaults.adapter = async config => {
    throw Object.assign(new Error('partial'), { isAxiosError: true, response: { data: { committed_book_ids: [7] }, config, status: 500 } });
  };
  const onChanged = vi.fn(); const onError = vi.fn();
  const view = renderHook(() => ({ selection: useLibrarySelection({ libId: '1', allSeries: [], onChanged, onError }), revision: useLibraryRefresh({ area: 'stats' }) }), { wrapper });
  act(() => view.result.current.selection.toggleSelectSeries(8));
  act(() => view.result.current.selection.toggleSelectSeries(9));
  await act(async () => view.result.current.selection.bulkProgress(true));
  expect(view.result.current.selection.selectedSeries).toEqual([8, 9]);
  expect(view.result.current.revision).toBeGreaterThan(0);
  expect(onChanged).toHaveBeenCalled();
  expect(onError).toHaveBeenCalledWith('home.bulkProgressFailed');
});

it('failed reading requests without committed identities leave other projections unchanged', async () => {
  vi.stubGlobal('EventSource', Source);
  apiClient.defaults.adapter = async () => { throw new Error('connection lost'); };
  const view = renderHook(() => ({ mutation: useReadStateMutation(), revision: useLibraryRefresh({ area: 'stats' }) }), { wrapper });
  await act(async () => { await expect(view.result.current.mutation.markBooks([7], true)).rejects.toThrow('connection lost'); });
  expect(view.result.current.revision).toBe(0);
});

it.each(['edit', 'reading', 'partial reading'] as const)('%s retains its refresh warning when context responses are deferred', async kind => {
  vi.stubGlobal('EventSource', Source);
  let committed = false;
  const pending: Array<() => void> = [];
  apiClient.defaults.adapter = async config => {
    if (config.method === 'put' || config.method === 'post') {
      committed = true;
      const response = { data: { committed_book_ids: [7] }, status: 500, statusText: 'partial', headers: {}, config };
      if (kind === 'partial reading') throw Object.assign(new Error('partial'), { isAxiosError: true, response });
      return { ...response, status: 200 };
    }
    if (config.url?.endsWith('/context') && committed) await new Promise<void>((_, reject) => pending.push(() => reject(new Error('refresh unavailable'))));
    return { data: { series: { id: 8, title: { String: 'Title', Valid: true } }, books: [], metadata_version: 'v1' }, status: 200, statusText: 'OK', headers: {}, config };
  };
  const showToast = vi.fn(); const t = (key: string) => key;
  const view = renderHook(() => {
    const ctx = useSeriesContext({ seriesId: '8' });
    return {
      ctx,
      edit: useSeriesEdit({ seriesId: '8', series: ctx.series, tags: ctx.tags, authors: ctx.authors, links: ctx.links, metadataVersion: ctx.metadataVersion, reload: ctx.reload, showToast, t }),
      progress: useSeriesProgress({ seriesId: '8', reload: ctx.reload, showToast, t }),
      revision: useLibraryRefresh({ area: 'stats' }),
    };
  }, { wrapper });
  await waitFor(() => expect(view.result.current.ctx.metadataVersion).toBe('v1'));
  let action: Promise<void>;
  act(() => { action = kind === 'edit' ? view.result.current.edit.save() : view.result.current.progress.quickToggleBook(7, true); });
  await waitFor(() => expect(pending.length).toBeGreaterThan(0));
  expect(showToast).not.toHaveBeenCalled();
  // Flush every held request, including the competing invalidation request in the regression.
  await act(async () => { pending.splice(0).forEach(reject => reject()); await action; });
  expect(showToast).toHaveBeenCalledWith(kind === 'edit' ? 'series.toast.savedRefreshFailed' : kind === 'reading' ? 'series.toast.statusUpdated / series.content.loadFailed' : 'series.toast.statusUpdated / series.toast.actionFailed', 'error');
  expect(showToast.mock.calls.every(([, level]) => level === 'error')).toBe(true);
  expect(view.result.current.revision).toBeGreaterThan(0);
  await act(async () => { pending.splice(0).forEach(reject => reject()); });
  expect(view.result.current.ctx.error).toBeTruthy();
});

it.each([
  ['edit', 'write'], ['reading', 'write'], ['partial reading', 'write'],
  ['edit', 'refresh'], ['reading', 'refresh'], ['partial reading', 'refresh'],
] as const)('%s delivers committed effects after switching targets during %s', async (kind, phase) => {
  vi.stubGlobal('EventSource', Source);
  let committed = false;
  let finishWrite: (() => void) | undefined;
  let rejectRefresh: (() => void) | undefined;
  apiClient.defaults.adapter = async config => {
    if (config.method === 'put' || config.method === 'post') {
      if (phase === 'write') await new Promise<void>(resolve => { finishWrite = resolve; });
      committed = true;
      const response = { data: { committed_book_ids: [7] }, status: 500, statusText: 'partial', headers: {}, config };
      if (kind === 'partial reading') throw Object.assign(new Error('partial'), { isAxiosError: true, response });
      return { ...response, status: 200 };
    }
    const id = Number(/series\/(\d+)\/context/.exec(config.url ?? '')?.[1]);
    if (id === 8 && committed) await new Promise<void>((_, reject) => { rejectRefresh = () => reject(new Error('old refresh failed')); });
    return { data: { series: { id, title: { String: `Title ${id}`, Valid: true } }, books: [], metadata_version: `v${id}` }, status: 200, statusText: 'OK', headers: {}, config };
  };
  const showToast = vi.fn(); const t = (key: string) => key;
  const view = renderHook(({ id }) => {
    const ctx = useSeriesContext({ seriesId: id });
    return {
      ctx,
      edit: useSeriesEdit({ seriesId: id, series: ctx.series, tags: ctx.tags, authors: ctx.authors, links: ctx.links, metadataVersion: ctx.metadataVersion, reload: ctx.reload, showToast, t }),
      progress: useSeriesProgress({ seriesId: id, reload: ctx.reload, showToast, t }),
      revision: useLibraryRefresh({ area: 'stats' }),
    };
  }, { wrapper, initialProps: { id: '8' } });
  await waitFor(() => expect(view.result.current.ctx.metadataVersion).toBe('v8'));
  let action: Promise<void>;
  act(() => { action = kind === 'edit' ? view.result.current.edit.save() : view.result.current.progress.quickToggleBook(7, true); });
  await waitFor(() => expect(phase === 'write' ? finishWrite : rejectRefresh).toBeDefined());
  view.rerender({ id: '9' });
  await waitFor(() => expect(view.result.current.ctx.metadataVersion).toBe('v9'));
  act(() => { view.result.current.edit.setIsEditing(true); });
  act(() => { view.result.current.edit.onFormChange('summary', 'Draft nine'); });
  await act(async () => { finishWrite?.(); rejectRefresh?.(); await action; });
  await waitFor(() => expect(view.result.current.revision).toBeGreaterThan(0));
  expect(view.result.current.ctx.series?.id).toBe(9);
  expect(view.result.current.ctx.error).toBeNull();
  expect(view.result.current.edit.isEditing).toBe(true);
  expect(view.result.current.edit.editForm.summary).toBe('Draft nine');
  expect(showToast).not.toHaveBeenCalled();
});
