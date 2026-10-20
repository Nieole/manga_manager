// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { RelocationModal } from './RelocationModal';

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('../../api/client', () => ({ apiClient: { get, post }, isAxiosError: () => false }));
vi.mock('../../i18n/LocaleProvider', () => ({ useI18n: () => ({ t: (key: string) => key }) }));
const library = { id: 1, name: 'My Library', path: '/old' };
const plan = { id: 12, library_id: 1, library_name: 'My Library', library_root: '/old', source_root: '/old', target_root: '/new',
  run_id: 7, status: 'checked', series_count: 1, book_count: 2, blocker_count: 1, anomaly_count: 2, policy_count: 0,
  anomaly_counts: { missing: 2 }, samples: [], created_at: '', updated_at: '' };

beforeEach(() => {
  get.mockReset(); post.mockReset();
  get.mockImplementation((url: string) => {
    if (url.includes('latest')) return Promise.reject({ response: { status: 404 } });
    if (url.includes('browse-dirs')) return Promise.resolve({ data: { current: '/new', parent: '/', dirs: [], drives: [] } });
    if (url.includes('details')) return Promise.resolve({ data: { items: [], total: 0, offset: 0, limit: 50 } });
    return Promise.resolve({ data: plan });
  });
  post.mockResolvedValue({ data: plan });
});
afterEach(cleanup);

it('chooses a server directory and starts a read-only preview', async () => {
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  fireEvent.click(screen.getByText('directoryPicker.browse'));
  await screen.findByText('directoryPicker.chooseCurrent');
  fireEvent.click(screen.getByText('directoryPicker.chooseCurrent'));
  fireEvent.click(screen.getByText('relocation.check'));
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/libraries/1/relocation-plans', { target_root: '/new' }));
  await screen.findByText('relocation.checkedHint');
  expect(screen.getByText('relocation.blockers')).toBeTruthy();
});

it('closing keeps the run, and reopening retrieves the persisted result and bounded details', async () => {
  get.mockImplementation((url: string) => {
    if (url.includes('details')) return Promise.resolve({ data: { items: [{ ordinal: 1, kind: 'anomaly', record_type: 'book', record_id: 4, source_path: '/old/a.cbz', target_path: '/new/a.cbz', reason: 'missing' }], total: 60, offset: 0, limit: 50 } });
    return Promise.resolve({ data: { ...plan, status: 'checking' } });
  });
  const onClose = vi.fn();
  const view = render(<RelocationModal library={library} onClose={onClose} />);
  await screen.findByText('relocation.cancelCheck');
  fireEvent.click(screen.getByText('relocation.close'));
  expect(onClose).toHaveBeenCalled();
  expect(post).not.toHaveBeenCalled();
  view.unmount();
  get.mockImplementation((url: string) => {
    if (url.includes('details')) return Promise.resolve({ data: { items: [{ ordinal: 1, kind: 'anomaly', record_type: 'book', record_id: 4, source_path: '/old/a.cbz', target_path: '/new/a.cbz', reason: 'missing' }], total: 60, offset: 0, limit: 50 } });
    return Promise.resolve({ data: plan });
  });
  render(<RelocationModal library={library} onClose={onClose} />);
  await screen.findByText('relocation.checkedHint');
  fireEvent.click(screen.getByText('relocation.anomalies'));
  await screen.findByText('/new/a.cbz');
  fireEvent.click(screen.getByText('relocation.next'));
  await waitFor(() => expect(get).toHaveBeenCalledWith('/api/relocation-plans/12/details?kind=anomaly&offset=50&limit=50'));
});

it('cancels by run id and shows errors while reading a page', async () => {
  get.mockImplementation((url: string) => url.includes('details') ? Promise.reject(new Error('offline')) : Promise.resolve({ data: { ...plan, status: 'checking' } }));
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  fireEvent.click(await screen.findByText('relocation.cancelCheck'));
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/system/runs/7/cancel'));
  await screen.findByText('relocation.detailsError');
});

it('keeps a failed detail page navigable and allows retry', async () => {
  let fail = true;
  get.mockImplementation((url: string) => {
    if (url.includes('offset=50') && fail) return Promise.reject(new Error('offline'));
    if (url.includes('details')) return Promise.resolve({ data: { items: [], total: 60, offset: 0, limit: 50 } });
    return Promise.resolve({ data: plan });
  });
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  fireEvent.click(await screen.findByText('relocation.next'));
  await screen.findByText('relocation.detailsError');
  fail = false;
  fireEvent.click(screen.getByText('relocation.retryDetails'));
  await waitFor(() => expect(screen.queryByText('relocation.detailsError')).toBeNull());
  fireEvent.click(screen.getByText('relocation.previous'));
  await waitFor(() => expect(get).toHaveBeenLastCalledWith('/api/relocation-plans/12/details?kind=blocker&offset=0&limit=50'));
});

it('ignores a slow detail response after switching categories', async () => {
  let resolveOld: (value: unknown) => void = () => {};
  get.mockImplementation((url: string) => {
    if (url.includes('kind=blocker')) return new Promise((resolve) => { resolveOld = resolve; });
    if (url.includes('details')) return Promise.resolve({ data: { items: [{ ordinal: 2, kind: 'anomaly', target_path: '/new/missing.cbz', reason: 'missing' }], total: 1 } });
    return Promise.resolve({ data: plan });
  });
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  fireEvent.click(await screen.findByText('relocation.anomalies'));
  await screen.findByText('/new/missing.cbz');
  resolveOld({ data: { items: [{ ordinal: 1, kind: 'blocker', target_path: '/stale/conflict.cbz', reason: 'record_occupied' }], total: 1 } });
  await waitFor(() => expect(screen.queryByText('/stale/conflict.cbz')).toBeNull());
  expect(screen.getByText('/new/missing.cbz')).toBeTruthy();
});

it('keeps the selected detail page when the active category is clicked again', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [{ ordinal: 1, kind: 'blocker', target_path: '/occupied/book.cbz', reason: 'record_occupied' }], total: 1 } : plan }));
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  await screen.findByText('/occupied/book.cbz');
  fireEvent.click(screen.getByText('relocation.blockers'));
  expect(screen.getByText('/occupied/book.cbz')).toBeTruthy();
});


it('compares the actual IO limits when custom policies conflict', async () => {
  const before = { storage_profile: 'custom', io_policy: { scan_concurrency: 1, archive_open_concurrency: 2, cover_concurrency: 3, hash_concurrency: 4,
    pause_background_when_reading: true, idle_only_heavy_tasks: false, disable_same_disk_page_cache: true } };
  const after = { storage_profile: 'custom', io_policy: { scan_concurrency: 5, archive_open_concurrency: 6, cover_concurrency: 7, hash_concurrency: 8,
    pause_background_when_reading: false, idle_only_heavy_tasks: true, disable_same_disk_page_cache: false } };
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? {
    items: [{ ordinal: 1, kind: 'blocker', reason: 'policy_conflict', source_path: '/old', target_path: '/new', policy_before: before, policy_after: after }], total: 1,
  } : plan }));
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  fireEvent.click(await screen.findByText('relocation.blockers'));
  await screen.findByText('relocation.reason.policy_conflict');
  expect(screen.getByText('relocation.policy.destinationExisting')).toBeTruthy();
  expect(screen.getByText('relocation.policy.destinationProposed')).toBeTruthy();
  for (const [label, numbers, flags] of [
    ['relocation.policy.before', ['1', '2', '3', '4'], ['relocation.policy.enabled', 'relocation.policy.disabled', 'relocation.policy.enabled']],
    ['relocation.policy.after', ['5', '6', '7', '8'], ['relocation.policy.disabled', 'relocation.policy.enabled', 'relocation.policy.disabled']],
  ] as const) {
    const panel = within(screen.getByRole('region', { name: label }));
    expect(panel.getByText('settings.library.storageProfile.custom')).toBeTruthy();
    for (const value of numbers) expect(panel.getByText(value)).toBeTruthy();
    const labels = ['scanConcurrency', 'archiveOpenConcurrency', 'coverConcurrency', 'hashConcurrency', 'pauseWhenReading', 'idleOnlyHeavyTasks', 'disableSameDiskPageCache'];
    labels.forEach((key) => expect(panel.getByText(`relocation.policy.${key}`)).toBeTruthy());
    const booleanValues = panel.getAllByText(/relocation.policy.(enabled|disabled)/).map((node) => node.textContent);
    expect(booleanValues).toEqual(flags);
  }
  expect(post).not.toHaveBeenCalled();
});
