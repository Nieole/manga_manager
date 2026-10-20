// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
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
