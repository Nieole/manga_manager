/** @vitest-environment jsdom */
import { useState } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { SettingsProvider, useSettings } from '../../features/settings-editor';
import { SettingsConnectionsPage } from './SettingsConnectionsPage';
import { SettingsKOReaderPage } from './SettingsKOReaderPage';
import { useConnectionTest } from '../../features/settings-actions/ConnectionTestingProvider';
import { useKOReaderDiagnostics, useKOReaderEditing } from '../../features/koreader-settings/KOReaderProvider';
import type { KOReaderDeviceDiagnostics, KOReaderStatus } from '../../features/koreader-settings/types';
const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), toast: vi.fn(), t: (key: string, values?: { url?: string }) => values?.url ? `${key}: ${values.url}` : key }));
vi.mock('../../api/client', () => ({ apiClient: mocks, isAxiosError: () => true }));
vi.mock('../../components/ToastProvider', () => ({ useToast: () => ({ showToast: mocks.toast }) }));
vi.mock('../../i18n/LocaleProvider', () => ({ useI18n: () => ({ t: mocks.t }), getClientLocale: () => 'en-US', translateInLocale: (_locale: string, key: string) => key }));
const config = { server: {}, database: {}, library: {}, cache: {}, logging: {}, scanner: { workers: 4 }, tasks: {}, llm: { provider: 'old' }, protocols: {}, koreader: { enabled: false, base_path: '/koreader', match_mode: 'binary_hash', allow_registration: false, path_ignore_extension: false } };
const status: KOReaderStatus = { ...config.koreader, path_match_depth: 0, account_count: 0, enabled_account_count: 0, stats: { configured: false, has_password: false, has_valid_sync_key: false, username: '', total_books: 0, hashed_books: 0, unmatched_progress_count: 0, matched_progress_count: 0 } };
const devices: KOReaderDeviceDiagnostics = { summary: { device_count: 0, healthy_devices: 0, attention_devices: 0, total_records: 0, matched_records: 0, unmatched_records: 0, conflict_count: 0, error_conflict_count: 0 }, devices: [], conflicts: [] };
function Consumer() {
 const { config, loading, setConfig, hasSectionChanges, hasSectionConflict, discardSection } = useSettings();
 const connection = useConnectionTest();
 const diagnostics = useKOReaderDiagnostics();
 const editor = useKOReaderEditing();
 if (loading || !config) return <span>Loading</span>;
 return <><output>Ready</output><output aria-label="Published KOReader path">{diagnostics.koreaderStatus?.base_path}</output><output aria-label="KOReader dirty">{String(hasSectionChanges('koreader'))}</output><output aria-label="KOReader conflict">{String(hasSectionConflict('koreader'))}</output><button onClick={() => void discardSection('koreader')}>Reload KOReader</button><input aria-label="Provider" value={config.llm.provider} onChange={(event) => setConfig({ ...config, llm: { ...config.llm, provider: event.target.value } })} /><button onClick={() => void connection.handleTestLLM()}>Test connection</button><output aria-label="Connection result">{connection.llmTestResult}</output><button onClick={() => void diagnostics.fetchKOReaderDevices()}>Load devices</button><output aria-label="Device count">{diagnostics.koreaderDevices?.summary.device_count}</output><input aria-label="KOReader path" value={editor.koreaderForm?.base_path || ''} onChange={(event) => editor.setKOReaderForm({ ...editor.koreaderForm!, base_path: event.target.value })} /><button onClick={() => void editor.saveKOReader()}>Save KOReader</button><button onClick={() => void diagnostics.handleToggleKOReaderAccount({ id: 1, username: 'reader', enabled: false, sync_key: 'key', created_at: '', updated_at: '' })}>Toggle account</button></>;
}
beforeEach(() => {
 vi.clearAllMocks();
 mocks.get.mockImplementation(async (path: string) => {
  if (path === '/api/system/config') return { data: { config, validation: { valid: true, issues: [] }, capabilities: {} }, headers: { etag: '"one"' } };
  if (path === '/api/system/koreader') return { data: status, headers: { etag: '"one"' } };
  if (path === '/api/system/koreader/devices?limit=20') return { data: devices, headers: {} };
  return { data: [], headers: {} };
 });
 mocks.post.mockResolvedValue({ data: {}, headers: {} });
});
afterEach(cleanup);
it('keeps the production KOReader page usable after saving and refreshing device diagnostics', async () => {
 render(<SettingsProvider><SettingsKOReaderPage /></SettingsProvider>);
 const path = await screen.findByDisplayValue('/koreader');
 fireEvent.change(path, { target: { value: '/saved' } });
 mocks.post.mockResolvedValueOnce({ data: { ...status, base_path: '/saved' }, headers: { etag: '"two"' } });
 const originalGet = mocks.get.getMockImplementation()!;
 mocks.get.mockImplementation(async (route: string) => route === '/api/system/config'
  ? { data: { config: { ...config, koreader: { ...config.koreader, base_path: '/saved' } }, validation: { valid: true, issues: [] }, capabilities: {} }, headers: { etag: '"two"' } }
  : route === '/api/system/koreader/devices?limit=20'
   ? { data: { ...devices, summary: { ...devices.summary, device_count: 2 } }, headers: {} }
   : originalGet(route));
 fireEvent.click(screen.getByText('settings.koreader.save'));
 await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith('settings.toast.koreaderSaved', 'success'));
 await waitFor(() => expect(screen.getByDisplayValue('/saved')).toBeTruthy());
 expect(await screen.findByText('2')).toBeTruthy();
 expect(screen.getByText('settings.koreader.devicesTitle')).toBeTruthy();
});
it('does not show a late connection result under a newly edited provider', async () => {
 let finish!: (value: unknown) => void;
 mocks.post.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
 render(<SettingsProvider><Consumer /></SettingsProvider>);
 fireEvent.click(await screen.findByText('Test connection'));
 await waitFor(() => expect(finish).toBeTruthy());
 fireEvent.change(screen.getByLabelText('Provider'), { target: { value: 'new' } });
 await act(async () => finish({ data: { response: 'old provider result' } }));
 expect(screen.getByLabelText('Connection result').textContent).toBe('');
 expect(mocks.toast).not.toHaveBeenCalledWith('settings.toast.llmTestSucceeded', 'success');
});

it('keeps the latest device diagnostics when earlier requests finish late', async () => {
 const originalGet = mocks.get.getMockImplementation()!;
 const finish: Array<(value: unknown) => void> = [];
 mocks.get.mockImplementation((path: string) => path.includes('/devices?') ? new Promise((resolve) => finish.push(resolve)) : originalGet(path));
 render(<SettingsProvider><Consumer /></SettingsProvider>);
 fireEvent.click(await screen.findByText('Load devices'));
 fireEvent.click(screen.getByText('Load devices'));
 await waitFor(() => expect(finish).toHaveLength(2));
 await act(async () => finish[1]({ data: { ...devices, summary: { ...devices.summary, device_count: 2 } } }));
 expect(screen.getByLabelText('Device count').textContent).toBe('2');
 await act(async () => finish[0]({ data: { ...devices, summary: { ...devices.summary, device_count: 1 } } }));
 expect(screen.getByLabelText('Device count').textContent).toBe('2');
});

it('loads configuration independently while account diagnostics are unavailable', async () => {
 const originalGet = mocks.get.getMockImplementation()!;
 mocks.get.mockImplementation((path: string) => path === '/api/system/koreader/accounts' ? new Promise(() => {}) : originalGet(path));
 render(<SettingsProvider><Consumer /></SettingsProvider>);
 expect(await screen.findByText('Ready')).toBeTruthy();
});

it('does not let account diagnostics replace a dirty KOReader form or its version', async () => {
 const originalGet = mocks.get.getMockImplementation()!;
 render(<SettingsProvider><Consumer /></SettingsProvider>);
 fireEvent.change(await screen.findByLabelText('KOReader path'), { target: { value: '/local-draft' } });
 mocks.get.mockImplementation(async (path: string) => path === '/api/system/koreader' ? { data: { ...config.koreader, base_path: '/external' }, headers: { etag: '"two"' } } : originalGet(path));
 fireEvent.click(screen.getByText('Toggle account'));
 await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith('settings.toast.koreaderAccountToggled', 'success'));
 expect((screen.getByLabelText('KOReader path') as HTMLInputElement).value).toBe('/local-draft');
 mocks.post.mockRejectedValueOnce({ response: { status: 409 } });
 const log = vi.spyOn(console, 'error').mockImplementation(() => {});
 try {
  fireEvent.click(screen.getByText('Save KOReader'));
  await waitFor(() => expect(mocks.post.mock.calls[1][2]).toEqual({ headers: { 'If-Match': '"one"' } }));
  expect((screen.getByLabelText('KOReader path') as HTMLInputElement).value).toBe('/local-draft');
 } finally { log.mockRestore(); }
});

it('preserves KOReader edits made during a save and uses the acknowledged version next time', async () => {
 let finish!: (value: unknown) => void;
 const originalGet = mocks.get.getMockImplementation()!;
 mocks.post.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
 render(<SettingsProvider><Consumer /></SettingsProvider>);
 fireEvent.change(await screen.findByLabelText('KOReader path'), { target: { value: '/sent' } });
 fireEvent.click(screen.getByText('Save KOReader'));
 await waitFor(() => expect(finish).toBeTruthy());
 fireEvent.change(screen.getByLabelText('KOReader path'), { target: { value: '/newer' } });
 const saved = { ...config.koreader, base_path: '/sent' };
 mocks.get.mockImplementation(async (path: string) => path === '/api/system/config' ? { data: { config: { ...config, koreader: saved }, validation: { valid: true, issues: [] }, capabilities: {} }, headers: { etag: '"two"' } } : originalGet(path));
 await act(async () => finish({ data: saved, headers: { etag: '"two"' } }));
 expect((screen.getByLabelText('KOReader path') as HTMLInputElement).value).toBe('/newer');
 mocks.post.mockRejectedValueOnce({ response: { status: 409 } });
 const log = vi.spyOn(console, 'error').mockImplementation(() => {});
 try {
  fireEvent.click(screen.getByText('Save KOReader'));
  await waitFor(() => expect(mocks.post.mock.calls[1][2]).toEqual({ headers: { 'If-Match': '"two"' } }));
 } finally { log.mockRestore(); }
});

it('keeps a committed KOReader save distinct from follow-up refresh failure', async () => {
 const originalGet = mocks.get.getMockImplementation()!;
 render(<SettingsProvider><Consumer /></SettingsProvider>);
 fireEvent.change(await screen.findByLabelText('KOReader path'), { target: { value: '/saved' } });
 mocks.post.mockResolvedValueOnce({ data: { ...config.koreader, base_path: '/saved' }, headers: { etag: '"two"' } });
 mocks.get.mockImplementation(async (path: string) => { if (path === '/api/system/config' || path === '/api/system/koreader') throw new Error('refresh failed'); return originalGet(path); });
 const log = vi.spyOn(console, 'error').mockImplementation(() => {});
 try {
  fireEvent.click(screen.getByText('Save KOReader'));
  await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith('settings.toast.configRefreshFailed', 'error'));
  expect(mocks.toast).toHaveBeenCalledWith('settings.toast.koreaderSaved', 'success');
  expect(mocks.toast).not.toHaveBeenCalledWith('settings.toast.koreaderSaveFailed', 'error');
  expect((screen.getByLabelText('KOReader path') as HTMLInputElement).value).toBe('/saved');
  expect(screen.getByLabelText('KOReader dirty').textContent).toBe('false');
  expect(screen.getByLabelText('Published KOReader path').textContent).toBe('/saved');
  mocks.post.mockRejectedValueOnce({ response: { status: 409 } });
  fireEvent.click(screen.getByText('Save KOReader'));
  await waitFor(() => expect(mocks.post.mock.calls[1][2]).toEqual({ headers: { 'If-Match': '"two"' } }));
 } finally { log.mockRestore(); }
});

it('does not publish save results after the editing consumer unmounts', async () => {
 let finish!: (value: unknown) => void;
 mocks.post.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
 const view = render(<SettingsProvider><Consumer /></SettingsProvider>);
 fireEvent.click(await screen.findByText('Save KOReader'));
 await waitFor(() => expect(finish).toBeTruthy());
 view.unmount();
 await act(async () => finish({ data: config.koreader, headers: { etag: '"two"' } }));
 expect(mocks.toast).not.toHaveBeenCalled();
});

it('disables both protocol toggles until the connections save completes', async () => {
 const originalGet = mocks.get.getMockImplementation()!;
 mocks.get.mockImplementation(async (path: string) => path === '/api/system/client-connections'
  ? { data: { base_url: 'http://localhost', endpoints: [], status: { opds_enabled: false, mihon_enabled: false } } }
  : originalGet(path));
 let finish!: (value: unknown) => void;
 mocks.post.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
 render(<SettingsProvider><Consumer /><SettingsConnectionsPage /></SettingsProvider>);
 await screen.findByText('Ready');
 const opds = await screen.findByRole('button', { name: 'OPDS' });
 const mihon = screen.getByRole('button', { name: 'Mihon' });
 fireEvent.click(opds);
 await waitFor(() => expect(finish).toBeTruthy());
 expect((opds as HTMLButtonElement).disabled).toBe(true);
 expect((mihon as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(mihon);
 expect(mocks.post).toHaveBeenCalledTimes(1);
 expect(mocks.toast).not.toHaveBeenCalledWith('settings.connections.protocolEnabledToast', 'success');
 await act(async () => finish({ data: { validation: { valid: true, issues: [] } }, headers: { etag: '"two"' } }));
 await waitFor(() => expect((mihon as HTMLButtonElement).disabled).toBe(false));
 expect(mocks.toast.mock.calls.filter(([key]) => key === 'settings.connections.protocolEnabledToast')).toHaveLength(1);
});

it('refreshes the production KOReader setup URL after explicit conflict reload', async () => {
 const originalGet = mocks.get.getMockImplementation()!;
 const log = vi.spyOn(console, 'error').mockImplementation(() => {});
 try {
  render(<SettingsProvider><Consumer /><SettingsKOReaderPage /></SettingsProvider>);
  fireEvent.change(await screen.findByLabelText('KOReader path'), { target: { value: '/local' } });
  mocks.post.mockRejectedValueOnce({ response: { status: 409 } });
  fireEvent.click(screen.getByText('Save KOReader'));
  await waitFor(() => expect(screen.getByLabelText('KOReader conflict').textContent).toBe('true'));
  mocks.get.mockImplementation(async (path: string) => path === '/api/system/config'
   ? { data: { config: { ...config, koreader: { ...config.koreader, base_path: '/other-admin' } }, validation: { valid: true, issues: [] }, capabilities: {} }, headers: { etag: '"two"' } }
   : path === '/api/system/koreader' ? { data: { ...status, base_path: '/other-admin' }, headers: {} } : originalGet(path));
  fireEvent.click(screen.getByText('Reload KOReader'));
  await waitFor(() => expect(screen.getByLabelText('KOReader conflict').textContent).toBe('false'));
  expect((screen.getByLabelText('KOReader path') as HTMLInputElement).value).toBe('/other-admin');
  await waitFor(() => expect(screen.getByText(`settings.koreader.setupDescription: ${window.location.origin}/other-admin`)).toBeTruthy());
  expect(mocks.toast).not.toHaveBeenCalledWith('settings.toast.koreaderSaved', 'success');
 } finally { log.mockRestore(); }
});

function ProtocolConsumer() {
 const { loading, saveProtocols } = useSettings();
 const [result, setResult] = useState('');
 if (loading) return null;
 return <><button onClick={() => void saveProtocols('opds', true)}>Start OPDS</button><button onClick={() => void saveProtocols('mihon', true).then((value) => setResult(String(value?.committed)))}>Request Mihon</button><output aria-label="Mihon committed">{result}</output></>;
}
it('returns an explicit uncommitted result for a skipped protocol request', async () => {
 let finish!: (value: unknown) => void;
 mocks.post.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
 render(<SettingsProvider><ProtocolConsumer /></SettingsProvider>);
 fireEvent.click(await screen.findByText('Start OPDS'));
 await waitFor(() => expect(finish).toBeTruthy());
 fireEvent.click(screen.getByText('Request Mihon'));
 await waitFor(() => expect(screen.getByLabelText('Mihon committed').textContent).toBe('false'));
 expect(mocks.post).toHaveBeenCalledTimes(1);
 await act(async () => finish({ data: { validation: { valid: true, issues: [] } }, headers: {} }));
});
it('does not replace reloaded KOReader status with a late pre-reload response', async () => {
 const originalGet = mocks.get.getMockImplementation()!;
 let finish!: (value: unknown) => void;
 mocks.get.mockImplementation((path: string) => path === '/api/system/koreader'
  ? new Promise((resolve) => { finish = resolve; }) : originalGet(path));
 render(<SettingsProvider><Consumer /><SettingsKOReaderPage /></SettingsProvider>);
 await screen.findByText('Ready');
 await waitFor(() => expect(finish).toBeTruthy());
 mocks.get.mockImplementation(async (path: string) => path === '/api/system/koreader'
  ? { data: { ...status, base_path: '/reloaded' } } : originalGet(path));
 fireEvent.click(screen.getByText('Reload KOReader'));
 await waitFor(() => expect(screen.getByLabelText('Published KOReader path').textContent).toBe('/reloaded'));
 await act(async () => finish({ data: status }));
 expect(screen.getByText(`settings.koreader.setupDescription: ${window.location.origin}/reloaded`)).toBeTruthy();
});
it('reports status refresh failure separately after acknowledging an explicit KOReader reload', async () => {
 const originalGet = mocks.get.getMockImplementation()!;
 render(<SettingsProvider><Consumer /></SettingsProvider>);
 fireEvent.change(await screen.findByLabelText('KOReader path'), { target: { value: '/local' } });
 await waitFor(() => expect(screen.getByLabelText('Published KOReader path').textContent).toBe('/koreader'));
 mocks.get.mockImplementation(async (path: string) => {
  if (path === '/api/system/koreader') throw new Error('status unavailable');
  if (path === '/api/system/config') return { data: { config: { ...config, koreader: { ...config.koreader, base_path: '/reloaded' } }, validation: { valid: true, issues: [] }, capabilities: {} }, headers: { etag: '"two"' } };
  return originalGet(path);
 });
 const log = vi.spyOn(console, 'error').mockImplementation(() => {});
 try {
  fireEvent.click(screen.getByText('Reload KOReader'));
  await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith('settings.toast.configRefreshFailed', 'error'));
  expect((screen.getByLabelText('KOReader path') as HTMLInputElement).value).toBe('/reloaded');
  expect(screen.getByLabelText('KOReader dirty').textContent).toBe('false');
  expect(mocks.toast).not.toHaveBeenCalledWith('settings.toast.koreaderSaved', 'success');
  expect(mocks.toast).not.toHaveBeenCalledWith('settings.toast.koreaderSaveFailed', 'error');
 } finally { log.mockRestore(); }
});
