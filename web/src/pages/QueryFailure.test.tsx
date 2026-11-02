/** @vitest-environment jsdom */
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { LocaleProvider } from '../i18n/LocaleProvider';
import { messages } from '../i18n/locales/en-US';
import Dashboard from './Dashboard';
import Stats from './Stats';

const mocks = vi.hoisted(() => ({ get: vi.fn(), user: { id: 1 }, refresh: 0 }));
vi.mock('../api/client', () => ({ apiClient: { get: mocks.get, defaults: { headers: { common: {} } } }, getApiErrorMessage: () => 'failed' }));
vi.mock('../auth/AuthProvider', () => ({ useAuth: () => ({ user: mocks.user, isAdmin: true }) }));
vi.mock('../session/ApplicationSessionProvider', () => ({ useLibraryRefresh: () => mocks.refresh }));
vi.mock('../components/ToastProvider', () => ({ useToast: () => ({ showToast: vi.fn() }) }));
function page(Component: typeof Dashboard) {
  return <LocaleProvider initialLocale="en-US" initialMessages={messages} fallbackMessages={messages}><MemoryRouter><Component /></MemoryRouter></LocaleProvider>;
}
beforeEach(() => {
  mocks.user = { id: 1 };
  mocks.refresh = 0;
  mocks.get.mockReset().mockRejectedValue(new Error('offline'));
  vi.stubGlobal('IntersectionObserver', class { observe() {} disconnect() {} });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it('Dashboard reports unavailable libraries instead of false onboarding and can recover to a real empty library', async () => {
  render(page(Dashboard));
  expect(await screen.findByText('Libraries: unavailable.')).toBeTruthy();
  expect(screen.queryByText(messages['dashboard.onboarding.title'])).toBeNull();
  mocks.get.mockResolvedValue({ data: [] });
  fireEvent.click(screen.getByRole('button', { name: 'Retry Libraries' }));
  expect(await screen.findByText(messages['dashboard.onboarding.title'])).toBeTruthy();
});

it('Stats fails independently without fabricated zeroes and recovers to a genuine zero/empty review', async () => {
  render(page(Stats));
  expect(await screen.findByText('Reading streak: unavailable.')).toBeTruthy();
  expect(await screen.findByText('Reading time: unavailable.')).toBeTruthy();
  expect(await screen.findByText('Period review: unavailable.')).toBeTruthy();
  expect(screen.queryByText('0 days')).toBeNull();
  expect(screen.queryByText(messages['stats.readingTime.empty'])).toBeNull();
  mocks.get.mockResolvedValue({ data: { pages: 0, read_seconds: 0, active_days: 0, books_touched: 0, books_completed: 0, top_series: [] } });
  fireEvent.click(screen.getByRole('button', { name: 'Retry Period review' }));
  expect(await screen.findByText(messages['stats.review.empty'])).toBeTruthy();
  expect(screen.getByText('Reading streak: unavailable.')).toBeTruthy();
});

function deferred() {
  let resolve!: (value: { data: unknown }) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<{ data: unknown }>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const overview = { total_series: 4, total_books: 17, read_books: 3, total_pages: 230, active_days_7: 2, library_sizes: [] };
const periodData = { pages: 321, read_seconds: 0, active_days: 2, books_touched: 1, books_completed: 0, top_series: [] };
function success(url: string) {
  if (url === '/api/libraries') return [{ id: 1, name: 'Comics' }];
  if (url === '/api/stats/dashboard') return overview;
  if (url === '/api/stats/streak') return { current: 0, longest: 0 };
  if (url.includes('reading-time')) return { total_seconds: 0, top: [] };
  if (url.includes('/period?')) return periodData;
  if (url.includes('/summary')) return { counts: { total: 0 } };
  return [];
}

it('Dashboard preserves labelled stale overview while optional failures are retryable, then clears stale on recovery', async () => {
  mocks.get.mockImplementation(async (url: string) => ({ data: success(url) }));
  const view = render(page(Dashboard));
  expect(await screen.findByText('17')).toBeTruthy();
  mocks.get.mockRejectedValue(new Error('offline'));
  mocks.refresh++;
  view.rerender(page(Dashboard));
  expect(await screen.findByRole('button', { name: 'Retry Overview' })).toBeTruthy();
  expect(screen.getByText('Overview: showing earlier results; update not confirmed.')).toBeTruthy();
  expect(screen.getByText('17')).toBeTruthy();
  mocks.get.mockImplementation(async (url: string) => ({ data: success(url) }));
  fireEvent.click(screen.getByRole('button', { name: 'Retry Overview' }));
  await screen.findByText('17');
  await waitFor(() => expect(screen.queryByText('Overview: showing earlier results; update not confirmed.')).toBeNull());
});

it('Dashboard optional panels fail independently of the overview, and recommendations recover', async () => {
  mocks.get.mockImplementation(async (url: string) => {
    if (url === '/api/libraries' || url === '/api/stats/dashboard') return { data: success(url) };
    throw new Error('offline');
  });
  render(page(Dashboard));
  expect(await screen.findByText('17')).toBeTruthy();
  for (const panel of ['Continue reading', 'Review inbox', 'Reading activity']) {
    expect(await screen.findByText(`${panel}: unavailable.`)).toBeTruthy();
  }
  fireEvent.click(screen.getByRole('button', { name: messages['dashboard.recommendations.refresh'] }));
  expect(await screen.findByText('Recommendations: unavailable.')).toBeTruthy();
  mocks.get.mockResolvedValue({ data: [{ series_id: 9, title: 'Recovered recommendation', reason: 'For you', cover_path: '' }] });
  fireEvent.click(screen.getByRole('button', { name: 'Retry Recommendations' }));
  expect(await screen.findByText('Recovered recommendation')).toBeTruthy();
});

it('Stats keeps same-period values labelled stale but never relabels another period or accepts its late reply', async () => {
  mocks.get.mockImplementation(async (url: string) => ({ data: success(url) }));
  const view = render(page(Stats));
  expect(await screen.findByText('321')).toBeTruthy();
  mocks.get.mockRejectedValue(new Error('offline'));
  mocks.refresh++;
  view.rerender(page(Stats));
  expect(await screen.findByRole('button', { name: 'Retry Period review' })).toBeTruthy();
  expect(screen.getByText('Period review: showing earlier results; update not confirmed.')).toBeTruthy();
  expect(screen.getByText('321')).toBeTruthy();
  const old = deferred();
  mocks.get.mockImplementation((url: string) => url.includes('month=1') ? old.promise : Promise.reject(new Error('offline')));
  fireEvent.change(screen.getAllByRole('combobox')[1], { target: { value: '1' } });
  expect(screen.queryByText('321')).toBeNull();
  fireEvent.change(screen.getAllByRole('combobox')[1], { target: { value: '2' } });
  expect(await screen.findByText('Period review: unavailable.')).toBeTruthy();
  await act(async () => old.resolve({ data: { ...periodData, pages: 999 } }));
  expect(screen.queryByText('999')).toBeNull();
  mocks.get.mockResolvedValue({ data: { ...periodData, pages: 222 } });
  fireEvent.click(screen.getByRole('button', { name: 'Retry Period review' }));
  expect(await screen.findByText('222')).toBeTruthy();
});

it.each([Dashboard, Stats])('%s discards prior-identity values and late successes/failures', async Component => {
  mocks.get.mockImplementation(async (url: string) => ({ data: success(url) }));
  const view = render(page(Component));
  expect(await screen.findByText(Component === Dashboard ? '17' : '321')).toBeTruthy();
  const old = deferred();
  mocks.get.mockImplementation(() => old.promise);
  mocks.refresh++;
  view.rerender(page(Component));
  mocks.user = { id: 2 };
  mocks.get.mockRejectedValue(new Error('new identity offline'));
  view.rerender(page(Component));
  expect(await screen.findByText(Component === Dashboard ? 'Libraries: unavailable.' : 'Period review: unavailable.')).toBeTruthy();
  expect(screen.queryByText(Component === Dashboard ? '17' : '321')).toBeNull();
  await act(async () => old.resolve({ data: Component === Dashboard ? overview : periodData }));
  expect(screen.queryByText(Component === Dashboard ? '17' : '321')).toBeNull();
});

it('recommendations in flight cannot populate the next identity', async () => {
  const old = deferred();
  mocks.get.mockImplementation((url: string) => url.includes('/recommendations') ? old.promise : Promise.resolve({ data: success(url) }));
  const view = render(page(Dashboard));
  await screen.findByText('17');
  fireEvent.click(screen.getByRole('button', { name: messages['dashboard.recommendations.refresh'] }));
  mocks.user = { id: 2 };
  view.rerender(page(Dashboard));
  await act(async () => old.resolve({ data: [{ series_id: 9, title: 'Private recommendation', reason: '', cover_path: '' }] }));
  expect(screen.queryByText('Private recommendation')).toBeNull();
});

it('heatmap range replies are fenced and failure does not fabricate a blank activity grid', async () => {
  const old = deferred();
  mocks.get.mockImplementation((url: string) => url.includes('weeks=13') ? old.promise : Promise.resolve({ data: success(url) }));
  render(page(Dashboard));
  await screen.findByText('17');
  fireEvent.change(screen.getByRole('combobox'), { target: { value: '13' } });
  mocks.get.mockRejectedValue(new Error('range unavailable'));
  fireEvent.change(screen.getByRole('combobox'), { target: { value: '26' } });
  expect(await screen.findByText('Reading activity: unavailable.')).toBeTruthy();
  await act(async () => old.resolve({ data: [{ date: '2026-10-01', page_count: 300 }] }));
  expect(screen.getByText('Reading activity: unavailable.')).toBeTruthy();
  expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('26');
});

it.each([Dashboard, Stats])('%s ignores an older refresh failure after a newer refresh succeeds', async Component => {
  const old = deferred();
  mocks.get.mockImplementation(() => old.promise);
  const view = render(page(Component));
  mocks.get.mockImplementation(async (url: string) => ({ data: success(url) }));
  mocks.refresh++;
  view.rerender(page(Component));
  expect(await screen.findByText(Component === Dashboard ? '17' : '321')).toBeTruthy();
  await act(async () => old.reject(new Error('late failure')));
  expect(screen.queryByRole('button', { name: Component === Dashboard ? 'Retry Overview' : 'Retry Period review' })).toBeNull();
});

it('successful zero streak and empty time ranking remain real data, not unavailable', async () => {
  mocks.get.mockImplementation(async (url: string) => ({ data: success(url) }));
  render(page(Stats));
  expect(await screen.findByText(messages['stats.readingTime.empty'])).toBeTruthy();
  expect(screen.getByText('0 days')).toBeTruthy();
  expect(screen.queryByText('Reading time: unavailable.')).toBeNull();
});

it('Dashboard labels an earlier empty library stale when its refresh fails', async () => {
  mocks.get.mockResolvedValue({ data: [] });
  const view = render(page(Dashboard));
  await screen.findByText(messages['dashboard.onboarding.title']);
  mocks.get.mockRejectedValue(new Error('offline'));
  mocks.refresh++;
  view.rerender(page(Dashboard));
  expect(await screen.findByRole('button', { name: 'Retry Libraries' })).toBeTruthy();
  expect(screen.getByText('Libraries: showing earlier results; update not confirmed.')).toBeTruthy();
});

it('Dashboard shows genuine zero totals and explicitly empty lists after successful requests', async () => {
  mocks.get.mockImplementation(async (url: string) => ({ data: url === '/api/stats/dashboard' ? { ...overview, total_books: 0, total_series: 0, read_books: 0, total_pages: 0 } : success(url) }));
  render(page(Dashboard));
  expect(await screen.findByText('No recent reading yet.')).toBeTruthy();
  expect(screen.getAllByText('0').length).toBeGreaterThan(0);
  fireEvent.click(screen.getByRole('button', { name: messages['dashboard.recommendations.refresh'] }));
  expect(await screen.findByText('No recommendations available yet.')).toBeTruthy();
  expect(screen.queryByText('Overview: unavailable.')).toBeNull();
});
