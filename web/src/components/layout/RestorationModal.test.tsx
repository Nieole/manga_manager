// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { RestorationModal } from './RestorationModal';

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('../../api/client', () => ({ apiClient: { get, post }, isAxiosError: (error: unknown) => Boolean(error && typeof error === 'object' && 'response' in error) }));
vi.mock('../../i18n/LocaleProvider', () => ({ useI18n: () => ({ t: (key: string) => key }) }));
const library = { id: 1, name: 'Books', path: '/new' };
const protection = { library_id: 1, operation_id: 30, plan_id: 12, facts_digest: 'opaque', anomaly_count: 2, scan_mode: 'watch', scan_interval: 17, scan_formats: 'cbz' };
const plan = { id: 40, purpose: 'restoration', protection_id: 30, library_id: 1, library_name: 'Books', library_root: '/new', source_root: '/new', target_root: '/new', run_id: 41, status: 'checked', series_count: 1, book_count: 2, blocker_count: 0, anomaly_count: 0, policy_count: 0, anomaly_counts: {}, samples: [], created_at: '', updated_at: '' };
beforeEach(() => {
  get.mockReset(); post.mockReset();
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0, offset: 0, limit: 50 } : { protection, accepted: { ...plan, id: 12, anomaly_count: 2 }, check: plan } }));
  post.mockResolvedValue({ data: { id: 50, purpose: 'restoration', plan_id: 40, library_id: 1, status: 'completed', protected: false } });
});
afterEach(cleanup);

it('keeps a clean recheck protected until the administrator explicitly releases with selected settings', async () => {
  render(<RestorationModal library={library} onClose={vi.fn()} />);
  const release = await screen.findByRole('button', { name: 'restoration.release' });
  expect((release as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText('restoration.originalSettings')).toBeTruthy();
  expect((screen.getByLabelText('restoration.scanMode') as HTMLSelectElement).value).toBe('watch');
  expect(post).not.toHaveBeenCalled();
  fireEvent.click(screen.getByLabelText('restoration.confirm'));
  fireEvent.click(release);
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/libraries/1/relocation-restoration/release', { plan_id: 40, confirm_release: true, accept_loss_risk: false, scan_mode: 'watch', scan_interval: 17, scan_formats: 'cbz' }));
  await screen.findByText('restoration.completed');
});

it('requires separate loss-risk consent for remaining anomalies and clears both consents when settings change', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : { protection, check: { ...plan, anomaly_count: 1 } } }));
  render(<RestorationModal library={library} onClose={vi.fn()} />);
  const release = await screen.findByRole('button', { name: 'restoration.release' });
  fireEvent.click(screen.getByLabelText('restoration.confirm'));
  expect((release as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByLabelText('restoration.acceptLossRisk'));
  expect((release as HTMLButtonElement).disabled).toBe(false);
  fireEvent.change(screen.getByLabelText('restoration.scanMode'), { target: { value: 'none' } });
  expect((screen.getByLabelText('restoration.confirm') as HTMLInputElement).checked).toBe(false);
  expect((screen.getByLabelText('restoration.acceptLossRisk') as HTMLInputElement).checked).toBe(false);
  fireEvent.click(screen.getByLabelText('restoration.confirm'));
  fireEvent.click(screen.getByLabelText('restoration.acceptLossRisk'));
  fireEvent.click(release);
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/libraries/1/relocation-restoration/release', expect.objectContaining({ accept_loss_risk: true, scan_mode: 'none' })));
});

it('starts a fresh read-only check, clears consent, shows bounded current details and allows cancellation', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [{ ordinal: 1, kind: 'anomaly', reason: 'missing', source_path: '/new/missing.cbz' }], total: 51, offset: 0, limit: 50 } : url.includes('tasks/live') ? { runs: [{ run_id: 41, can_cancel: true }] } : url === '/api/relocation-plans/40' ? { ...plan, status: 'checking' } : { protection, accepted: { ...plan, id: 12, anomaly_count: 2 }, check: plan } }));
  post.mockResolvedValue({ data: { ...plan, status: 'checking' } });
  render(<RestorationModal library={library} onClose={vi.fn()} />);
  await screen.findByText('/new/missing.cbz');
  expect(get).toHaveBeenCalledWith('/api/relocation-plans/40/details?kind=anomaly&offset=0&limit=50');
  fireEvent.click(screen.getByLabelText('restoration.confirm'));
  fireEvent.click(screen.getByRole('button', { name: 'restoration.recheck' }));
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/libraries/1/relocation-restoration/checks', {}));
  expect((screen.getByLabelText('restoration.confirm') as HTMLInputElement).checked).toBe(false);
  const cancel = await screen.findByRole('button', { name: 'relocation.cancelCheck' });
  fireEvent.click(cancel);
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/system/runs/41/cancel'));
  expect(post.mock.calls.some(([url]) => String(url).endsWith('/release'))).toBe(false);
});

it('blocks a rejected stale confirmation until a new check and never retries release automatically', async () => {
  post.mockRejectedValue({ response: { status: 409, data: { code: 'facts_changed' } } });
  render(<RestorationModal library={library} onClose={vi.fn()} />);
  const release = await screen.findByRole('button', { name: 'restoration.release' });
  fireEvent.click(screen.getByLabelText('restoration.confirm'));
  fireEvent.click(release);
  await screen.findByText('restoration.error.facts_changed');
  expect((release as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByLabelText('restoration.confirm') as HTMLInputElement).checked).toBe(false);
  expect(post).toHaveBeenCalledTimes(1);
});

it('does not let an ordinary migration check or another protection generation authorize restoration', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : { protection, check: { ...plan, protection_id: 29 } } }));
  const { unmount } = render(<RestorationModal library={library} onClose={vi.fn()} />);
  await screen.findByText('restoration.stale');
  expect((screen.getByLabelText('restoration.confirm') as HTMLInputElement).disabled).toBe(true);
  unmount();
  get.mockResolvedValue({ data: { protection, check: { ...plan, purpose: undefined } } });
  render(<RestorationModal library={library} onClose={vi.fn()} />);
  await screen.findByText('restoration.invalidCheck');
  expect((screen.getByRole('button', { name: 'restoration.release' }) as HTMLButtonElement).disabled).toBe(true);
});

it('reopens the exact consumed check and reports durable recovery without offering a release', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.endsWith('/40') ? { ...plan, status: 'consumed', operation_id: 50 } : url.includes('operations') ? { id: 50, library_id: 1, purpose: 'restoration', status: 'paths_committed', protected: true } : url.includes('details') ? { items: [], total: 0 } : { protection, check: { ...plan, id: 60 } } }));
  const close = vi.fn();
  render(<RestorationModal library={library} planID={40} onClose={close} />);
  await screen.findByText('restoration.recovering');
  expect(get).toHaveBeenCalledWith('/api/relocation-plans/40');
  expect((screen.getByRole('button', { name: 'restoration.release' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'relocation.close' }));
  expect(close).toHaveBeenCalledOnce();
  expect(post).not.toHaveBeenCalled();
});

it('ignores a late prior-library response and starts the new library without inherited consent', async () => {
  let resolveOld: (value: unknown) => void = () => {};
  get.mockImplementation((url: string) => url.includes('/libraries/1/') ? new Promise((resolve) => { resolveOld = resolve; }) : Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : { protection: { ...protection, library_id: 2, operation_id: 60 }, check: { ...plan, id: 70, library_id: 2, protection_id: 60, library_root: '/other' } } }));
  const { rerender } = render(<RestorationModal library={library} onClose={vi.fn()} />);
  rerender(<RestorationModal library={{ id: 2, name: 'Other', path: '/other' }} onClose={vi.fn()} />);
  await waitFor(() => expect((screen.getByLabelText('restoration.scanMode') as HTMLSelectElement).value).toBe('watch'));
  resolveOld({ data: { protection, check: plan } });
  fireEvent.click(screen.getByLabelText('restoration.confirm'));
  fireEvent.click(screen.getByRole('button', { name: 'restoration.release' }));
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/libraries/2/relocation-restoration/release', expect.objectContaining({ plan_id: 70 })));
});

it('keeps historical and current facts distinct, paginates by 50, and retries failed detail loading', async () => {
  let fail = true;
  get.mockImplementation((url: string) => {
    if (url.includes('/12/details') && fail) return Promise.reject(new Error('offline'));
    return Promise.resolve({ data: url.includes('details') ? { items: [], total: 101, offset: url.includes('offset=50') ? 50 : 0, limit: 50 } : { protection, accepted: { ...plan, id: 12, anomaly_count: 2 }, check: plan } });
  });
  render(<RestorationModal library={library} onClose={vi.fn()} />);
  await screen.findByText('restoration.historicalCounts');
  fireEvent.click(screen.getByRole('button', { name: 'restoration.historicalDetails' }));
  await screen.findByText('relocation.detailsError');
  fail = false;
  fireEvent.click(screen.getByRole('button', { name: 'relocation.retryDetails' }));
  await screen.findByText('1–50 / 101');
  fireEvent.click(screen.getByRole('button', { name: 'relocation.next' }));
  await waitFor(() => expect(get).toHaveBeenCalledWith('/api/relocation-plans/12/details?kind=anomaly&offset=50&limit=50'));
  fireEvent.click(screen.getByRole('button', { name: 'restoration.currentDetails' }));
  await waitFor(() => expect(get).toHaveBeenCalledWith('/api/relocation-plans/40/details?kind=anomaly&offset=0&limit=50'));
  expect(post).not.toHaveBeenCalled();
});

it('refreshes protection generation on a new check without carrying old settings or consent', async () => {
  let reads = 0;
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : { protection: reads++ === 0 ? protection : { ...protection, operation_id: 80, scan_mode: 'interval', scan_interval: 25 }, check: plan } }));
  post.mockResolvedValue({ data: { ...plan, id: 90, protection_id: 80 } });
  render(<RestorationModal library={library} onClose={vi.fn()} />);
  await waitFor(() => expect((screen.getByLabelText('restoration.confirm') as HTMLInputElement).disabled).toBe(false));
  fireEvent.click(screen.getByLabelText('restoration.confirm'));
  fireEvent.click(screen.getByRole('button', { name: 'restoration.recheck' }));
  await waitFor(() => expect((screen.getByLabelText('restoration.scanMode') as HTMLSelectElement).value).toBe('interval'));
  expect((screen.getByLabelText('restoration.scanInterval') as HTMLInputElement).value).toBe('25');
  expect((screen.getByLabelText('restoration.confirm') as HTMLInputElement).checked).toBe(false);
});
