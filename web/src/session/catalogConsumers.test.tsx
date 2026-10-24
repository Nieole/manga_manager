/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
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
import { useSeriesEdit } from '../pages/series-detail/hooks/useSeriesEdit';
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
  expect(view.result.current.edit.editForm.summary.String).toBe('My draft');
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
  expect(view.result.current.edit.editForm.summary.String).toBe('Draft for nine');
});
