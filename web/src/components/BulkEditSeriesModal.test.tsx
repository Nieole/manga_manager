/** @vitest-environment jsdom */
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { apiClient } from '../api/client';
import { LocaleProvider } from '../i18n/LocaleProvider';
import { messages } from '../i18n/locales/zh-CN';
import { ApplicationSessionProvider, useLibraryRefresh } from '../session/ApplicationSessionProvider';
import BulkEditSeriesModal from './BulkEditSeriesModal';

const originalAdapter = apiClient.defaults.adapter;
beforeEach(() => { vi.stubGlobal('EventSource', class { close() {} }); });
afterEach(() => { cleanup(); apiClient.defaults.adapter = originalAdapter; vi.restoreAllMocks(); vi.unstubAllGlobals(); });

function Projection() { return <output data-testid="catalog-version">{useLibraryRefresh()}</output>; }

function renderEditor(onSuccess = vi.fn()) {
  const onClose = vi.fn(); const onError = vi.fn();
  render(<ApplicationSessionProvider identity="reader" isAdmin={false}><LocaleProvider initialLocale="zh-CN" initialMessages={messages} fallbackMessages={messages}>
    <Projection /><BulkEditSeriesModal seriesIds={[8, 9]} onClose={onClose} onSuccess={onSuccess} onError={onError} />
  </LocaleProvider></ApplicationSessionProvider>);
  fireEvent.change(screen.getByPlaceholderText(messages['bulkEdit.publisherPlaceholder']), { target: { value: 'Publisher' } });
  fireEvent.click(screen.getByText(messages['bulkEdit.apply']));
  return { onClose, onSuccess, onError };
}

it('reports committed metadata and later statistics warnings while refreshing consumers', async () => {
  apiClient.defaults.adapter = async config => ({
    data: config.method === 'post' ? { updated: 2, committed: true, committed_series_ids: [8, 9], refresh_failed_series_ids: [9] } : [],
    status: 200, statusText: 'OK', headers: {}, config,
  });
  const onClose = vi.fn(); const onSuccess = vi.fn(); const onError = vi.fn();
  render(<ApplicationSessionProvider identity="reader" isAdmin={false}><LocaleProvider initialLocale="zh-CN" initialMessages={messages} fallbackMessages={messages}>
    <Projection /><BulkEditSeriesModal seriesIds={[8, 9]} onClose={onClose} onSuccess={onSuccess} onError={onError} />
  </LocaleProvider></ApplicationSessionProvider>);
  fireEvent.change(screen.getByPlaceholderText(messages['bulkEdit.publisherPlaceholder']), { target: { value: 'Publisher' } });
  fireEvent.click(screen.getByText(messages['bulkEdit.apply']));
  await waitFor(() => expect(onSuccess).toHaveBeenCalledWith(2));
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(onError).toHaveBeenCalledWith(messages['bulkEdit.savedRefreshFailed']);
  await waitFor(() => expect(screen.getByTestId('catalog-version').textContent).not.toBe('0'));
});

it('keeps an uncommitted failure editable and does not announce changes', async () => {
  apiClient.defaults.adapter = async config => {
    if (config.method === 'post') throw Object.assign(new Error('write rejected'), { isAxiosError: true, response: { data: { error: 'Write rejected', updated: 0, committed: false, committed_series_ids: [] } } });
    return { data: [], status: 200, statusText: 'OK', headers: {}, config };
  };
  const { onClose, onSuccess, onError } = renderEditor();
  await waitFor(() => expect(onError).toHaveBeenCalledWith('Write rejected'));
  expect(onClose).not.toHaveBeenCalled();
  expect(onSuccess).not.toHaveBeenCalled();
  expect(screen.getByTestId('catalog-version').textContent).toBe('0');
  expect((screen.getByPlaceholderText(messages['bulkEdit.publisherPlaceholder']) as HTMLInputElement).value).toBe('Publisher');
});

it('retains known committed work from an error response and refreshes its consumers', async () => {
  apiClient.defaults.adapter = async config => {
    if (config.method === 'post') throw Object.assign(new Error('later step failed'), { isAxiosError: true, response: { data: { error: 'Later step failed', updated: 2, committed_series_ids: [8] } } });
    return { data: [], status: 200, statusText: 'OK', headers: {}, config };
  };
  const { onClose, onSuccess, onError } = renderEditor();
  await waitFor(() => expect(onSuccess).toHaveBeenCalledWith(1));
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(onError).toHaveBeenCalledWith(`${messages['bulkEdit.savedLaterFailed']}: Later step failed`);
  await waitFor(() => expect(screen.getByTestId('catalog-version').textContent).not.toBe('0'));
});

it('reports actual affected IDs and keeps callback faults separate from saving', async () => {
  apiClient.defaults.adapter = async config => ({
    data: config.method === 'post' ? { updated: 4, committed: true, committed_series_ids: [8], refresh_failed_series_ids: [] } : [],
    status: 200, statusText: 'OK', headers: {}, config,
  });
  const onSuccess = vi.fn(() => { throw new Error('view refresh failed'); });
  const { onClose, onError } = renderEditor(onSuccess);
  await waitFor(() => expect(onError).toHaveBeenCalledWith(messages['bulkEdit.savedRefreshFailed']));
  expect(onSuccess).toHaveBeenCalledWith(1);
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(onError).not.toHaveBeenCalledWith(messages['bulkEdit.failed']);
});
