// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { RelocationEntry } from './RelocationEntry';

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('../../api/client', () => ({ apiClient: { get, post }, isAxiosError: () => false }));
vi.mock('../../i18n/LocaleProvider', () => ({ useI18n: () => ({ t: (key: string) => key }) }));
afterEach(() => { cleanup(); vi.clearAllMocks(); });

it('opens the requested historical plan instead of the latest, and dismisses the deep link', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : {
    id: 12, library_id: 4, source_root: '/old', target_root: '/historical', status: 'checked', samples: [],
    series_count: 0, book_count: 0, blocker_count: 0, anomaly_count: 0, policy_count: 0,
  } }));
  render(<MemoryRouter initialEntries={['/library/4?relocation_plan=12']}><RelocationEntry isAdmin libraries={[{ id: '4', name: 'Books', path: '/current' }]} selectedID={null} onClose={vi.fn()} /></MemoryRouter>);
  await screen.findByText('relocation.checkedHint');
  expect(get).toHaveBeenCalledWith('/api/relocation-plans/12');
  expect(get.mock.calls.some(([url]) => String(url).includes('latest'))).toBe(false);
  expect((screen.getByDisplayValue('/historical') as HTMLInputElement).value).toBe('/historical');
  fireEvent.click(screen.getByText('relocation.close'));
  await waitFor(() => expect(screen.queryByText('relocation.checkedHint')).toBeNull());
  expect(post).not.toHaveBeenCalled();
});

it('does not load host paths for a regular user opening a deep link', () => {
  render(<MemoryRouter initialEntries={['/library/4?relocation_plan=12']}><RelocationEntry isAdmin={false} libraries={[{ id: '4', name: 'Books', path: '/current' }]} selectedID={null} onClose={vi.fn()} /></MemoryRouter>);
  expect(get).not.toHaveBeenCalled();
  expect(screen.queryByRole('dialog')).toBeNull();
});

it('opens an edit redirect with only the proposed root and leaves the checked plan uncommittable until rechecked', async () => {
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : {
    id: 12, library_id: 4, source_root: '/old', target_root: '/historical', status: 'checked', samples: [],
    series_count: 1, book_count: 2, blocker_count: 0, anomaly_count: 0, policy_count: 0,
  } }));
  render(<MemoryRouter><RelocationEntry isAdmin libraries={[{ id: '4', name: 'Books', path: '/old' }]} selectedID="4" initialTarget="/proposed" onClose={vi.fn()} /></MemoryRouter>);
  await screen.findByDisplayValue('/proposed');
  expect((screen.getByText('relocation.commit') as HTMLButtonElement).disabled).toBe(true);
  expect(post).not.toHaveBeenCalled();
});
