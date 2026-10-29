/** @vitest-environment jsdom */
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { AuthProvider } from './AuthProvider';
import { AuthGate } from './AuthGate';
import { LocaleProvider } from '../i18n/LocaleProvider';
import { messages } from '../i18n/locales/zh-CN';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('../api/client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    defaults: { headers: { common: {} } },
    interceptors: { response: { use: () => 1, eject: () => {} } },
  },
  isAxiosError: (error: unknown) => Boolean(error && typeof error === 'object' && 'isAxiosError' in error),
  getApiErrorMessage: () => 'failed',
}));

beforeEach(() => {
  localStorage.clear();
  mocks.get.mockReset().mockResolvedValue({ data: { setup_required: true, authenticated: false } });
  mocks.post.mockReset();
});
afterEach(cleanup);

async function submit() {
  render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={messages} fallbackMessages={messages}>
      <MemoryRouter><AuthProvider><AuthGate><div>application-ready</div></AuthGate></AuthProvider></MemoryRouter>
    </LocaleProvider>,
  );
  await waitFor(() => expect(document.getElementById('setup-username')).toBeTruthy());
  for (const [id, value] of [['setup-username', ' Admin '], ['setup-password', 'password1'], ['setup-confirm', 'password1']]) {
    fireEvent.change(document.getElementById(id)!, { target: { value } });
  }
  fireEvent.submit(document.querySelector('form')!);
}

const session = {
  user: { id: 1, username: ' Admin ', role: 'admin', display_name: '', must_change_password: false },
  csrf_token: 'csrf',
};

it('setup preserves the submitted username bytes', async () => {
  mocks.post.mockResolvedValue({ data: session });
  await submit();
  await screen.findByText('application-ready');
  expect(mocks.post).toHaveBeenCalledWith('/api/auth/setup', expect.objectContaining({ username: ' Admin ' }));
});

it('a committed account with failed session leads to login instead of another setup', async () => {
  mocks.post.mockRejectedValueOnce({ isAxiosError: true, response: { status: 500, data: { account_committed: true } } }).mockResolvedValueOnce({ data: session });
  await submit();
  await waitFor(() => expect(document.getElementById('login-username')).toBeTruthy());
  expect(document.getElementById('setup-username')).toBeNull();
  fireEvent.change(document.getElementById('login-username')!, { target: { value: ' Admin ' } });
  fireEvent.change(document.getElementById('login-password')!, { target: { value: 'password1' } });
  fireEvent.submit(document.querySelector('form')!);
  await screen.findByText('application-ready');
  expect(mocks.post).toHaveBeenLastCalledWith('/api/auth/login', expect.objectContaining({ username: ' Admin ' }));
});

it('successful setup keeps legacy-claim warnings visible after entering the app', async () => {
  mocks.post.mockResolvedValue({ data: { ...session, warnings: ['reading_progress'] } });
  await submit();
  await screen.findByText('application-ready');
  expect((await screen.findByRole('alert')).textContent).toContain('历史数据');
});

it('committed password with failed replacement session returns to login and preserves offline ownership', async () => {
  localStorage.setItem('manga-manager:offline-owner', '1');
  localStorage.setItem('manga-manager:offline-books', '{"42":{"bookId":"42"}}');
  mocks.get.mockResolvedValue({ data: { setup_required: false, authenticated: true, ...session, user: { ...session.user, must_change_password: true } } });
  mocks.post.mockRejectedValueOnce({ isAxiosError: true, response: { status: 500, data: { password_committed: true, login_required: true } } }).mockResolvedValueOnce({ data: session });
  render(<LocaleProvider initialLocale="zh-CN" initialMessages={messages} fallbackMessages={messages}><MemoryRouter><AuthProvider><AuthGate><div>application-ready</div></AuthGate></AuthProvider></MemoryRouter></LocaleProvider>);
  await waitFor(() => expect(document.getElementById('fc-current')).toBeTruthy());
  for (const [id, value] of [['fc-current', 'password1'], ['fc-new', 'password2'], ['fc-confirm', 'password2']]) fireEvent.change(document.getElementById(id)!, { target: { value } });
  fireEvent.submit(document.querySelector('form')!);
  await waitFor(() => expect(document.getElementById('login-username')).toBeTruthy());
  expect((await screen.findByRole('alert')).textContent).toContain('新密码');
  expect(localStorage.getItem('manga-manager:offline-owner')).toBe('1');
  expect(localStorage.getItem('manga-manager:offline-books')).toContain('42');
  fireEvent.change(document.getElementById('login-username')!, { target: { value: ' Admin ' } });
  fireEvent.change(document.getElementById('login-password')!, { target: { value: 'password2' } });
  fireEvent.submit(document.querySelector('form')!);
  await screen.findByText('application-ready');
  expect(screen.queryByRole('alert')).toBeNull();
});
