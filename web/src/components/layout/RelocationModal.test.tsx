// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { RelocationModal } from './RelocationModal';

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('../../api/client', () => ({ apiClient: { get, post }, isAxiosError: (error: unknown) => Boolean(error && typeof error === "object" && "response" in error) }));
vi.mock('../../i18n/LocaleProvider', () => ({ useI18n: () => ({ t: (key: string) => key }) }));
const library = { id: 1, name: 'My Library', path: '/old' };
const plan = { id: 12, library_id: 1, library_name: 'My Library', library_root: '/old', source_root: '/old', target_root: '/new',
  run_id: 7, status: 'checked', series_count: 1, book_count: 2, blocker_count: 1, anomaly_count: 2, policy_count: 0,
  anomaly_counts: { missing: 2 }, samples: [], created_at: '', updated_at: '' };

it('never presents a restoration check as a migration confirmation', async () => {
  get.mockResolvedValue({ data: { ...plan, purpose: 'restoration', blocker_count: 0 } });
  render(<RelocationModal library={library} planID={12} onClose={vi.fn()} />);
  await screen.findByText('restoration.useRestoration');
  expect(screen.queryByText('relocation.commit')).toBeNull();
  expect(screen.getByRole('link', { name: 'restoration.open' }).getAttribute('href')).toBe('/library/1?restoration_plan=12');
});

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

it('requires explicit consent bound to the displayed anomalous plan', async () => {
  get.mockImplementation((url: string) => url.includes('details')
    ? Promise.resolve({ data: { items: [], total: 0, offset: 0, limit: 50 } })
    : Promise.resolve({ data: { ...plan, blocker_count: 0 } }));
  post.mockResolvedValue({ data: { id: 30, plan_id: 12, status: 'completed', protected: true, series_count: 1, book_count: 2 } });
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  const confirm = await screen.findByText('relocation.commit');
  expect((confirm as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByLabelText('relocation.protection.acceptAnomalies'));
  fireEvent.change(screen.getByPlaceholderText('directoryPicker.placeholder'), { target: { value: '/other' } });
  fireEvent.change(screen.getByPlaceholderText('directoryPicker.placeholder'), { target: { value: '/new' } });
  expect((confirm as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByLabelText('relocation.protection.acceptAnomalies'));
  fireEvent.click(confirm);
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/relocation-plans/12/commit', { accept_anomalies: true }));
  await screen.findByText('relocation.protection.active');
});

it('reopens durable protection with original automatic settings', async () => {
  get.mockImplementation((url: string) => url.includes('details')
    ? Promise.resolve({ data: { items: [], total: 0, offset: 0, limit: 50 } })
    : Promise.resolve({ data: { ...plan, status: 'consumed', protection: { scan_mode: 'watch', scan_interval: 17, scan_formats: 'cbz' } } }));
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  await screen.findByText('relocation.protection.active');
  expect(screen.getByText('relocation.protection.originalSettings')).toBeTruthy();
  expect(screen.queryByText('relocation.protection.acceptAnomalies')).toBeNull();
});

it('requires explicit historical source selection and invalidates a preview when source changes', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('source-roots') ? {
    library_root: '/new', records_outside_current_root: true, candidates: ['/old/deep', '/old'],
  } : url.includes('details') ? { items: [], total: 0 } : { ...plan, library_root: '/new', source_root: '/old', blocker_count: 0, anomaly_count: 0 } }));
  render(<RelocationModal library={{ ...library, path: '/new' }} onClose={vi.fn()} />);
  await screen.findByText('relocation.repair.candidateHint');
  const source = screen.getByRole('textbox', { name: 'relocation.repair.sourceInput' }) as HTMLInputElement;
  expect(source.value).toBe('/old');
  fireEvent.change(source, { target: { value: '/old/deep' } });
  expect((screen.getByText('relocation.commit') as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '/old' }));
  expect(source.value).toBe('/old');
  fireEvent.click(screen.getByText('relocation.check'));
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/libraries/1/relocation-plans', { source_root: '/old', target_root: '/new' }));
});

it('never silently chooses a candidate when repairing a single series', async () => {
  get.mockImplementation((url: string) => url.includes('latest') ? Promise.reject({ response: { status: 404 } }) : Promise.resolve({ data: {
    library_root: '/new', records_outside_current_root: true, candidates: ['/old/deep/series', '/old/deep', '/old'],
  } }));
  render(<RelocationModal library={{ ...library, path: '/new' }} initialTarget="/new" onClose={vi.fn()} />);
  await screen.findByText('relocation.repair.candidateHint');
  expect((screen.getByRole('textbox', { name: 'relocation.repair.sourceInput' }) as HTMLInputElement).value).toBe('');
  expect((screen.getByText('relocation.check') as HTMLButtonElement).disabled).toBe(true);
});

it('does not reuse an old operation source as a new repair choice after the library root changes', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('source-roots') ? {
    library_root: '/third', records_outside_current_root: true, candidates: ['/new/deep', '/new'],
  } : url.includes('details') ? { items: [], total: 0 } : { ...plan, status: 'consumed' } }));
  render(<RelocationModal library={{ ...library, path: '/third' }} onClose={vi.fn()} />);
  await screen.findByText('relocation.repair.candidateHint');
  expect((screen.getByRole('textbox', { name: 'relocation.repair.sourceInput' }) as HTMLInputElement).value).toBe('');
  expect((screen.getByText('relocation.check') as HTMLButtonElement).disabled).toBe(true);
});

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

it('confirms only the checked server plan and disables committing an edited destination', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : { ...plan, blocker_count: 0, anomaly_count: 0 } }));
  post.mockResolvedValue({ data: { id: 5, plan_id: 12, library_id: 1, run_id: 9, status: 'completed', source_root: '/old', target_root: '/new', series_count: 1, book_count: 2, code: '' } });
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  const commit = await screen.findByText('relocation.commit');
  const target = screen.getByRole('textbox');
  fireEvent.change(target, { target: { value: '/other' } });
  expect((commit as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(target, { target: { value: '/new' } });
  fireEvent.click(commit);
  await screen.findByText('relocation.operation.completed');
  expect(post).toHaveBeenCalledWith('/api/relocation-plans/12/commit', {});
  expect(screen.queryByText('relocation.cancelCheck')).toBeNull();
});

it('reopens a persisted commit and shows its real stage without offering cancellation or a second commit', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : url.includes('relocation-operations') ? {
    id: 5, status: 'paths_committed', series_count: 1, book_count: 2, code: '',
  } : { ...plan, blocker_count: 0, anomaly_count: 0, operation_id: 5 } }));
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  await screen.findByText('relocation.operation.paths_committed');
  expect(screen.getByText('relocation.noCancelCommit')).toBeTruthy();
  expect(screen.queryByText('relocation.commit')).toBeNull();
  expect(screen.queryByText('relocation.cancelCheck')).toBeNull();
  expect((screen.getByRole('textbox') as HTMLInputElement).closest('fieldset')?.disabled).toBe(true);
});


it('shows a cancellable commit revalidation run and cancels by its run identifier', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : url.includes('tasks/live') ? { runs: [{ run_id: 9, phase: 'revalidate', current: 1, total: 2, can_cancel: true }] } : url.includes('relocation-operations') ? {
    id: 5, run_id: 9, status: 'pending', series_count: 1, book_count: 2, code: '',
  } : { ...plan, operation_id: 5 } }));
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  fireEvent.click(await screen.findByText('relocation.cancelCommit'));
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/system/runs/9/cancel'));
  expect(screen.getByText(/settings.maintenance.taskPhase.revalidate/)).toBeTruthy();
});

it('requires a new check after a stale confirmation instead of retrying an old plan', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : { ...plan, blocker_count: 0, anomaly_count: 0 } }));
  post.mockRejectedValue({ response: { data: { code: 'relocation_stale_plan' } } });
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  fireEvent.click(await screen.findByText('relocation.commit'));
  await screen.findByText('relocation.commitError.relocation_stale_plan');
  expect(screen.queryByText('relocation.commit')).toBeNull();
  expect(screen.getByText('relocation.check')).toBeTruthy();
});

it('allows confirmed policy changes and keeps a prepared recovery locked', async () => {
  const policyPlan = { ...plan, blocker_count: 0, anomaly_count: 0, policy_count: 2 };
  const operation = { id: 18, plan_id: 12, status: 'prepared', series_count: 1, book_count: 2, code: 'relocation_policy_recovery_conflict' };
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : url.includes('relocation-operations') ? operation : url.includes('tasks/live') ? { runs: [] } : policyPlan }));
  post.mockResolvedValue({ data: operation });
  render(<RelocationModal library={library} onClose={vi.fn()} />);
  fireEvent.click(await screen.findByText('relocation.commit'));
  await screen.findByText('relocation.operation.prepared');
  expect(screen.getByText('relocation.commitError.relocation_policy_recovery_conflict')).toBeTruthy();
  expect(screen.queryByText('relocation.check')).toBeNull();
  expect(screen.queryByText('relocation.commit')).toBeNull();
  expect((screen.getByRole('textbox') as HTMLInputElement).closest('fieldset')?.disabled).toBe(true);
});
