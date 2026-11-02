/** @vitest-environment jsdom */
import { act, fireEvent, render, screen, waitFor, cleanup } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { SettingsProvider, useSettings } from '../../features/settings-editor';
import { SettingsPublicationNotice } from './SettingsPublicationNotice';
const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), toast: vi.fn(), t: (key: string) => key }));
vi.mock('../../api/client', () => ({ apiClient: mocks, isAxiosError: () => true }));
vi.mock('../../components/ToastProvider', () => ({ useToast: () => ({ showToast: mocks.toast }) }));
vi.mock('../../i18n/LocaleProvider', () => ({ useI18n: () => ({ t: mocks.t }), getClientLocale: () => 'en-US', translateInLocale: (_locale: string, key: string) => key }));
function Editor() {
 const { loading, config, setConfig, saveConfig, hasSectionChanges, hasSectionConflict, discardSection } = useSettings();
 if (loading || !config) return <span>Loading</span>;
 return <><SettingsPublicationNotice /><output aria-label="Library conflict">{String(hasSectionConflict?.('library') ?? false)}</output><button onClick={() => void discardSection?.('library')}>Reload library</button><output aria-label="Library dirty">{String(hasSectionChanges('library'))}</output><input aria-label="Workers" value={config.scanner.workers} onChange={(event) => setConfig({ ...config, scanner: { ...config.scanner, workers: Number(event.target.value) } })} /><input aria-label="API key" value={config.llm.api_key ?? ''} onChange={(event) => setConfig({ ...config, llm: { ...config.llm, api_key: event.target.value } })} /><input aria-label="Archive pool" value={config.scanner.archive_pool_size ?? ''} readOnly /><button onClick={() => void saveConfig('library')}>Save</button><button onClick={() => void saveConfig('media')}>Save media</button></>;
}
beforeEach(() => {
 vi.clearAllMocks();
 const config = { server: {}, database: {}, library: { storage_policies: [] }, cache: {}, logging: {}, scanner: { workers: 4 }, llm: { api_key: '__mm_secret_unchanged__' }, tasks: {}, protocols: {}, koreader: { enabled: false, base_path: '/koreader', match_mode: 'binary_hash' } };
 mocks.get.mockImplementation(async (path: string) => {
  if (path === '/api/system/config') return { data: { config, validation: { valid: true, issues: [] }, capabilities: {} }, headers: { etag: '"version-one"' } };
  if (path === '/api/system/koreader') return { data: config.koreader, headers: { etag: '"version-one"' } };
  return { data: [], headers: {} };
 });
 mocks.post.mockResolvedValue({ data: { validation: { valid: true, issues: [] } }, headers: {} });
});
afterEach(cleanup);
it('keeps edits made while saving and advances only to the acknowledged save baseline', async () => {
 const initialGet = mocks.get.getMockImplementation()!;
 let finish!: (value: unknown) => void;
 mocks.post.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
 render(<SettingsProvider><Editor /></SettingsProvider>);
 fireEvent.change(await screen.findByLabelText('Workers'), { target: { value: '7' } });
 fireEvent.click(screen.getByText('Save'));
 await waitFor(() => expect(finish).toBeTruthy());
 fireEvent.change(screen.getByLabelText('Workers'), { target: { value: '9' } });
 const original = await initialGet('/api/system/config');
 const saved = { ...original.data.config, scanner: { ...original.data.config.scanner, workers: 7 } };
 mocks.get.mockImplementation(async (path: string) => path === '/api/system/config' ? { data: { ...original.data, config: saved }, headers: { etag: '"version-two"' } } : initialGet(path));
 await act(async () => finish({ data: { config: saved, validation: original.data.validation }, headers: { etag: '"version-two"' } }));
 await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith('settings.toast.configSaved', 'success'));
 expect((screen.getByLabelText('Workers') as HTMLInputElement).value).toBe('9');
 expect(screen.getByLabelText('Library dirty').textContent).toBe('true');
 fireEvent.click(screen.getByText('Save'));
 await waitFor(() => expect(mocks.post.mock.calls[1][2]).toEqual({ headers: { 'If-Match': '"version-two"' } }));
});
it('reports a saved configuration with pending restart instead of claiming immediate effects', async () => {
 mocks.post.mockResolvedValueOnce({ data: { validation: { valid: true, issues: [] }, publication: { saved: true, published: true, version: '"version-two"', immediate: [], next_operation: ['scanner.workers'], restart_required: ['server.port'] } }, headers: {} });
 render(<SettingsProvider><Editor /></SettingsProvider>);
 fireEvent.click(await screen.findByText('Save'));
 await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith('settings.toast.configSavedRestart', 'success'));
 expect(screen.getByText('settings.publication.pendingRestart')).toBeTruthy();
 expect(screen.getByText('server.port')).toBeTruthy();
 expect(screen.getByText('scanner.workers')).toBeTruthy();
});

it('keeps a successful save distinct from a failed follow-up refresh', async () => {
 const initialGet = mocks.get.getMockImplementation()!;
 let reads = 0;
 mocks.get.mockImplementation(async (path: string) => {
  if (path === '/api/system/config' && ++reads > 1) throw new Error('refresh unavailable');
  return initialGet(path);
 });
 const log = vi.spyOn(console, 'error').mockImplementation(() => {});
 try {
  render(<SettingsProvider><Editor /></SettingsProvider>);
  fireEvent.click(await screen.findByText('Save'));
  await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith('settings.toast.configRefreshFailed', 'error'));
  expect(mocks.toast).not.toHaveBeenCalledWith('settings.toast.configSaveFailed', 'error');
 } finally { log.mockRestore(); }
});

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

it('keeps a conflict visible until explicitly discarding this section and reloads its version', async () => {
 const initialGet = mocks.get.getMockImplementation()!;
 mocks.post.mockRejectedValueOnce({ response: { status: 409 } });
 const log = vi.spyOn(console, 'error').mockImplementation(() => {});
 try {
  render(<SettingsProvider><Editor /></SettingsProvider>);
  fireEvent.change(await screen.findByLabelText('Workers'), { target: { value: '7' } });
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(screen.getByLabelText('Library conflict').textContent).toBe('true'));
  expect((screen.getByLabelText('Workers') as HTMLInputElement).value).toBe('7');
  mocks.get.mockImplementation(async (path: string) => {
   const res = await initialGet(path);
   return path === '/api/system/config' ? { ...res, data: { ...res.data, config: { ...res.data.config, scanner: { workers: 11 } } }, headers: { etag: '"version-three"' } } : res;
  });
  fireEvent.click(screen.getByText('Reload library'));
  await waitFor(() => expect((screen.getByLabelText('Workers') as HTMLInputElement).value).toBe('11'));
  expect(screen.getByLabelText('Library conflict').textContent).toBe('false');
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(mocks.post.mock.calls[1][2]).toEqual({ headers: { 'If-Match': '"version-three"' } }));
 } finally { log.mockRestore(); }
});

it('accepts normalized saved fields without overwriting a field edited during the save', async () => {
 const originalGet = mocks.get.getMockImplementation()!;
 let finish!: (value: unknown) => void;
 mocks.post.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
 render(<SettingsProvider><Editor /></SettingsProvider>);
 fireEvent.change(await screen.findByLabelText('Workers'), { target: { value: '7' } });
 fireEvent.click(screen.getByText('Save'));
 await waitFor(() => expect(finish).toBeTruthy());
 fireEvent.change(screen.getByLabelText('Workers'), { target: { value: '9' } });
 const original = await originalGet('/api/system/config');
 const saved = { ...original.data.config, scanner: { workers: 7, archive_pool_size: 12 } };
 mocks.get.mockImplementation(async (path: string) => path === '/api/system/config' ? { data: { ...original.data, config: saved }, headers: { etag: '"two"' } } : originalGet(path));
 await act(async () => finish({ data: { config: saved, validation: original.data.validation }, headers: { etag: '"two"' } }));
 expect((screen.getByLabelText('Workers') as HTMLInputElement).value).toBe('9');
 expect((screen.getByLabelText('Archive pool') as HTMLInputElement).value).toBe('12');
});

it('does not rebase a dirty section when an unrelated save refreshes an unchanged server section', async () => {
 const initialGet = mocks.get.getMockImplementation()!;
 let configReads = 0;
 mocks.get.mockImplementation(async (path: string) => {
  const response = await initialGet(path);
  if (path !== '/api/system/config' || ++configReads === 1) return response;
  return { ...response, headers: { etag: '"version-two"' } };
 });
 render(<SettingsProvider><Editor /></SettingsProvider>);
 fireEvent.change(await screen.findByLabelText('Workers'), { target: { value: '7' } });
 fireEvent.click(screen.getByText('Save media'));
 await waitFor(() => expect(configReads).toBe(2));
 expect((screen.getByLabelText('Workers') as HTMLInputElement).value).toBe('7');
 fireEvent.click(screen.getByText('Save'));
 await waitFor(() => expect(mocks.post.mock.calls[1][2]).toEqual({ headers: { 'If-Match': '"version-one"' } }));
});

it('saves only the selected section and retains unsaved credentials across the refresh', async () => {
 render(<SettingsProvider><Editor /></SettingsProvider>);
 fireEvent.change(await screen.findByLabelText('Workers'), { target: { value: '7' } });
 fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'local-unsaved-key' } });
 fireEvent.click(screen.getByText('Save media'));
 await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith('settings.toast.configSaved', 'success'));
 expect(mocks.post.mock.calls[0][1].scanner.workers).toBe(4);
 expect(mocks.post.mock.calls[0][1].llm.api_key).toBe('__mm_secret_unchanged__');
 expect((screen.getByLabelText('Workers') as HTMLInputElement).value).toBe('7');
 expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('local-unsaved-key');
 expect(screen.getByLabelText('Library dirty').textContent).toBe('true');
});
