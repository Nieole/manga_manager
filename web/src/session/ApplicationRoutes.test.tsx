/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useNavigate } from 'react-router-dom';
import App from '../App';
import { AuthProvider } from '../auth/AuthProvider';
import { LocaleProvider } from '../i18n/LocaleProvider';
import { ToastProvider } from '../components/ToastProvider';
import { apiClient } from '../api/client';
class Source {
  static CLOSED = 2;
  static instances: Source[] = [];
  readyState = 0;
  closed = false;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onopen: (() => void) | null = null;
  constructor() { Source.instances.push(this); }
  close() { this.closed = true; }
}
const originalAdapter = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = originalAdapter; vi.unstubAllGlobals(); Source.instances = []; });
function Navigation() {
  const navigate = useNavigate();
  return <><button onClick={() => navigate('/library/1')}>library</button><button onClick={() => navigate('/reader/1')}>reader</button><button onClick={() => navigate('/series/1/franchise-graph')}>graph</button></>;
}
it('actual navigation routes share the session, refresh the library and release it in reader and graph', async () => {
  vi.stubGlobal('EventSource', Source);
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} unobserve() {} });
  const requests: string[] = [];
  apiClient.defaults.adapter = async config => {
    const url = config.url ?? '';
    requests.push(url);
    let data: unknown = [];
    if (url === '/api/auth/status') data = { authenticated: true, setup_required: false, user: { id: 1, username: 'reader', role: 'regular', must_change_password: false } };
    else if (url === '/api/libraries') data = Array.from({ length: 7 }, (_, index) => ({ id: index + 1, name: `My library ${index + 1}`, path: `/books/${index}` }));
    else if (url.includes('/series') && !url.includes('/franchise')) data = { data: [], total: 0, items: [] };
    else if (url.includes('/settings')) data = {};
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  render(<MemoryRouter initialEntries={['/library/1']}><LocaleProvider><ToastProvider><AuthProvider><Navigation /><App /></AuthProvider></ToastProvider></LocaleProvider></MemoryRouter>);
  await waitFor(() => expect(Source.instances).toHaveLength(1));
  await waitFor(() => expect(requests.some(url => url.includes('/api/series'))).toBe(true));
  // 列表查询会在完成后保留 100ms 去重窗口。
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 120)); });
  const sidebarFilter = screen.getByPlaceholderText('搜索资料库...');
  fireEvent.change(sidebarFilter, { target: { value: 'My library 1' } });
  const before = requests.filter(url => url.includes('/api/series')).length;
  act(() => Source.instances[0].onmessage?.({ data: 'refresh' }));
  await waitFor(() => expect(requests.filter(url => url.includes('/api/series')).length).toBeGreaterThan(before));
  expect(Source.instances).toHaveLength(1);
  expect((sidebarFilter as HTMLInputElement).value).toBe('My library 1');
  fireEvent.click(screen.getByText('reader'));
  await waitFor(() => expect(Source.instances[0].closed).toBe(true));
  fireEvent.click(screen.getByText('graph'));
  expect(Source.instances).toHaveLength(1);
  expect((sidebarFilter as HTMLInputElement).value).toBe('My library 1');
  fireEvent.click(screen.getByText('library'));
  await waitFor(() => expect(Source.instances).toHaveLength(2));
});
