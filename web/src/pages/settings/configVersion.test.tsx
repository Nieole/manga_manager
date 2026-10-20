/** @vitest-environment jsdom */
import { fireEvent, render, screen, waitFor, cleanup } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { SettingsProvider, useSettings } from './SettingsContext';
const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), toast: vi.fn(), t: (key: string) => key }));
vi.mock('../../api/client', () => ({ apiClient: mocks, isAxiosError: () => true }));
vi.mock('../../components/ToastProvider', () => ({ useToast: () => ({ showToast: mocks.toast }) }));
vi.mock('../../i18n/LocaleProvider', () => ({ useI18n: () => ({ t: mocks.t }), getClientLocale: () => 'en-US', translateInLocale: (_locale: string, key: string) => key }));
function Editor() {
 const { loading, config, setConfig, saveConfig } = useSettings();
 if (loading || !config) return <span>Loading</span>;
 return <><input aria-label="Workers" value={config.scanner.workers} onChange={(event) => setConfig({ ...config, scanner: { ...config.scanner, workers: Number(event.target.value) } })} /><button onClick={() => void saveConfig('library')}>Save</button><button onClick={() => void saveConfig('media')}>Save media</button></>;
}
beforeEach(() => {
 vi.clearAllMocks();
 const config = { server: {}, database: {}, library: { storage_policies: [] }, cache: {}, logging: {}, scanner: { workers: 4 }, llm: {}, tasks: {}, protocols: {}, koreader: { enabled: false, base_path: '/koreader', match_mode: 'binary_hash' } };
 mocks.get.mockImplementation(async (path: string) => {
  if (path === '/api/system/config') return { data: { config, validation: { valid: true, issues: [] }, capabilities: {} }, headers: { etag: '"version-one"' } };
  if (path === '/api/system/koreader') return { data: config.koreader, headers: { etag: '"version-one"' } };
  return { data: [], headers: {} };
 });
 mocks.post.mockResolvedValue({ data: { validation: { valid: true, issues: [] } }, headers: {} });
});
afterEach(cleanup);
it('sends the version belonging to the displayed server snapshot', async () => {
 render(<SettingsProvider><Editor /></SettingsProvider>);
 fireEvent.change(await screen.findByLabelText('Workers'), { target: { value: '7' } });
 fireEvent.click(screen.getByText('Save'));
 await waitFor(() => expect(mocks.post).toHaveBeenCalledWith('/api/system/config', expect.objectContaining({ scanner: expect.objectContaining({ workers: 7 }) }), { headers: { 'If-Match': '"version-one"' } }));
});
it('retains the draft and prompts review when another update made it stale', async () => {
 mocks.post.mockRejectedValue({ response: { status: 409 } });
 const log = vi.spyOn(console, 'error').mockImplementation(() => {});
 try {
  render(<SettingsProvider><Editor /></SettingsProvider>);
  fireEvent.change(await screen.findByLabelText('Workers'), { target: { value: '7' } });
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith('settings.toast.configChanged', 'error'));
  expect((screen.getByLabelText('Workers') as HTMLInputElement).value).toBe('7');
 } finally { log.mockRestore(); }
});

it('retains the original version for a dirty library draft after another save reveals moved policies', async () => {
 const initialGet = mocks.get.getMockImplementation()!;
 let configReads = 0;
 mocks.get.mockImplementation(async (path: string) => {
  const response = await initialGet(path);
  if (path !== '/api/system/config' || ++configReads === 1) return response;
  return { ...response, data: { ...response.data, config: { ...response.data.config, library: { storage_policies: [{ path: '/new-root', profile: 'hdd' }] } } }, headers: { etag: '"version-two"' } };
 });
 mocks.post.mockResolvedValueOnce({ data: { validation: { valid: true, issues: [] } }, headers: {} });
 mocks.post.mockRejectedValueOnce({ response: { status: 409 } });
 const log = vi.spyOn(console, 'error').mockImplementation(() => {});
 try {
  render(<SettingsProvider><Editor /></SettingsProvider>);
  fireEvent.change(await screen.findByLabelText('Workers'), { target: { value: '7' } });
  fireEvent.click(screen.getByText('Save media'));
  await waitFor(() => expect(configReads).toBe(2));
  expect((screen.getByLabelText('Workers') as HTMLInputElement).value).toBe('7');
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith('settings.toast.configChanged', 'error'));
  expect(mocks.post.mock.calls[1][2]).toEqual({ headers: { 'If-Match': '"version-one"' } });
 } finally { log.mockRestore(); }
});
