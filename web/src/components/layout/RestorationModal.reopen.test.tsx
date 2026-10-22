// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages } from '../../i18n/locales/en-US';
import { RestorationModal } from './RestorationModal';

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('../../api/client', () => ({ apiClient: { get, post, defaults: { headers: { common: {} } } }, isAxiosError: () => false }));
afterEach(cleanup);

it.each([
  { status: 'completed', formats: 'cbz' },
  { status: 'paths_committed', formats: 'cbz' },
  { status: 'pending', formats: 'cbz' },
  { status: 'pending', formats: undefined },
])('reopens a $status restoration with persisted formats $formats and selected settings locked', async ({ status, formats }) => {
  get.mockReset(); post.mockReset();
  const check = { id: 40, purpose: 'restoration', library_id: 1, library_root: '/new', status: 'consumed', operation_id: 50 };
  get.mockImplementation((url: string) => Promise.resolve({ data: url.includes('operations')
    ? { id: 50, library_id: 1, purpose: 'restoration', status, scan_mode: 'interval', scan_interval: 29, scan_formats: formats }
    : url.includes('details') ? { items: [], total: 0 }
      : { protection: status === 'completed' ? null : { operation_id: 30, scan_mode: 'watch', scan_interval: 17, scan_formats: 'cbz' }, check } }));
  render(<LocaleProvider initialLocale="en-US" initialMessages={messages}>
    <RestorationModal library={{ id: 1, name: 'Books', path: '/new' }} onClose={vi.fn()} />
  </LocaleProvider>);
  await waitFor(() => expect((screen.getByLabelText('Scan mode after release') as HTMLSelectElement).value).toBe('interval'));
  const interval = screen.getByLabelText('Scan interval (minutes)') as HTMLInputElement;
  expect(interval.value).toBe('29');
  expect((screen.getByLabelText('Scan formats (comma separated)') as HTMLInputElement).value).toBe(formats ?? '');
  expect(interval.closest('fieldset')?.disabled).toBe(true);
  expect((screen.getByRole('button', { name: 'Confirm protection release' }) as HTMLButtonElement).disabled).toBe(true);
  expect(post).not.toHaveBeenCalled();
});
