/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { apiClient } from '../../api/client';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages } from '../../i18n/locales/zh-CN';
import { FranchiseRepairStatus } from './FranchiseRepairStatus';

const original = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = original; });
function show(isAdmin = true) {
 return render(<LocaleProvider initialLocale="zh-CN" initialMessages={messages} fallbackMessages={messages}><FranchiseRepairStatus isAdmin={isAdmin} refreshKey={0} /></LocaleProvider>);
}
it('distinguishes a first unavailable projection from a successful empty projection', async () => {
 apiClient.defaults.adapter = async config => ({ data: { available: false, stale: true, status: 'failed', desired_version: 2, applied_version: 0 }, status: 200, statusText: 'OK', headers: {}, config });
 const view = show(false);
 expect(await screen.findByText(messages['franchise.unavailableFailed'])).toBeTruthy();
 expect(screen.queryByRole('button')).toBeNull();
 view.unmount();
 apiClient.defaults.adapter = async config => ({ data: { available: true, stale: false, status: 'current', desired_version: 2, applied_version: 2 }, status: 200, statusText: 'OK', headers: {}, config });
 await act(async () => { show(); });
 expect(screen.queryByRole('status')).toBeNull();
});
it('keeps old results identified as stale and retries immediately after a failure', async () => {
 let retried = false;
 apiClient.defaults.adapter = async config => {
  if (config.method === 'post') retried = true;
  return { data: config.method === 'post' ? { status: 'success' } : { available: true, stale: !retried, status: retried ? 'current' : 'failed', desired_version: 2, applied_version: retried ? 2 : 1 }, status: 200, statusText: 'OK', headers: {}, config };
 };
 show();
 expect(await screen.findByText(messages['franchise.failedStale'])).toBeTruthy();
 fireEvent.click(screen.getByText(messages['franchise.retry']));
 await vi.waitFor(() => expect(screen.queryByText(messages['franchise.failedStale'])).toBeNull());
 expect(retried).toBe(true);
});
