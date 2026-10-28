/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { apiClient } from '../../api/client';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages } from '../../i18n/locales/zh-CN';
import { DeleteLibraryDialog } from './DeleteLibraryDialog';

const original = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = original; });
function dialog(onDeleted: (...args: unknown[]) => void) {
 return render(<LocaleProvider initialLocale="zh-CN" initialMessages={messages} fallbackMessages={messages}><DeleteLibraryDialog library={{ id: '7', name: 'Library seven' }} onClose={vi.fn()} onDeleted={onDeleted} /></LocaleProvider>);
}
it('keeps the library and explains readers and shared work until a retry commits', async () => {
 let attempts = 0;
 apiClient.defaults.adapter = async config => {
  attempts++;
  if (attempts === 1) throw Object.assign(new Error('busy'), { isAxiosError: true, response: { config, data: { code: 'library_busy', conflicts: [{ reason: 'wait_for_reads', library_id: 7 }, { run_id: 12, library_id: 7, library_ids: [7, 8], scope: 'system', status: 'running' }] }, status: 409, statusText: 'Conflict', headers: {} } });
  return { config, data: { status: 'deleted', deleted: true }, status: 200, statusText: 'OK', headers: {} };
 };
 const deleted = vi.fn();
 dialog(deleted);
 fireEvent.click(screen.getByText(messages['layout.libraryActions.confirmDelete']));
 await screen.findByText(messages['libraryDelete.readers']);
 expect(screen.getByText(messages['libraryDelete.sharedWork'].replace('{{id}}', '12'))).toBeTruthy();
 expect(deleted).not.toHaveBeenCalled();
 expect(screen.getByRole('dialog')).toBeTruthy();
 fireEvent.click(screen.getByText(messages['layout.libraryActions.confirmDelete']));
 await waitFor(() => expect(deleted).toHaveBeenCalledTimes(1));
});
it('reports watcher restoration failure while preserving the deletion target', async () => {
 apiClient.defaults.adapter = async config => { throw Object.assign(new Error('failure'), { isAxiosError: true, response: { config, data: { code: 'library_delete_failed', warnings: ['watcher_restore_failed'] }, status: 500, statusText: 'Failure', headers: {} } }); };
 const deleted = vi.fn();
 dialog(deleted);
 fireEvent.click(screen.getByText(messages['layout.libraryActions.confirmDelete']));
 expect(await screen.findByText(messages['libraryDelete.restoreFailed'])).toBeTruthy();
 expect(deleted).not.toHaveBeenCalled();
 expect(screen.getByRole('dialog')).toBeTruthy();
});

it('ignores a late delete response after the target changes', async () => {
 let finish: (() => void) | undefined;
 apiClient.defaults.adapter = config => new Promise(resolve => {
  finish = () => resolve({ config, data: { deleted: true, status: 'deleted' }, status: 200, statusText: 'OK', headers: {} });
 });
 const deleted = vi.fn();
 const view = dialog(deleted);
 fireEvent.click(screen.getByText(messages['layout.libraryActions.confirmDelete']));
 await waitFor(() => expect(finish).toBeDefined());
 view.rerender(<LocaleProvider initialLocale="zh-CN" initialMessages={messages} fallbackMessages={messages}><DeleteLibraryDialog library={{ id: '8', name: 'Library eight' }} onClose={vi.fn()} onDeleted={deleted} /></LocaleProvider>);
 await act(async () => { finish?.(); });
 expect(deleted).not.toHaveBeenCalled();
 expect(screen.getByText(/Library eight/)).toBeTruthy();
});

it('keeps cancellation and exit facts visible when draining times out', async () => {
 apiClient.defaults.adapter = async config => { throw Object.assign(new Error('busy'), { isAxiosError: true, response: { config, data: { code: 'library_busy', deleted: false, drain_timed_out: true, cancellation_requested: [14, 15], exited_runs: [14], conflicts: [{ run_id: 15, reason: 'wait_for_exit' }] }, status: 409, statusText: 'Conflict', headers: {} } }); };
 const deleted = vi.fn();
 dialog(deleted);
 fireEvent.click(screen.getByText(messages['layout.libraryActions.confirmDelete']));
 expect(await screen.findByText(/已请求取消运行 #14/)).toBeTruthy();
 expect(screen.getByText(/运行 #14 已退出访问/)).toBeTruthy();
 expect(screen.getByText(/不会自动重新执行/)).toBeTruthy();
 expect(deleted).not.toHaveBeenCalled();
});

it('hands off an already committed deletion even if later work failed', async () => {
 apiClient.defaults.adapter = async config => { throw Object.assign(new Error('later failure'), { isAxiosError: true, response: { config, data: { deleted: true, warnings: ['access_retirement_failed'] }, status: 500, statusText: 'Failure', headers: {} } }); };
 const deleted = vi.fn();
 dialog(deleted);
 fireEvent.click(screen.getByText(messages['layout.libraryActions.confirmDelete']));
 await waitFor(() => expect(deleted).toHaveBeenCalledWith({ id: '7', name: 'Library seven' }, ['access_retirement_failed']));
 expect(screen.queryByText(messages['layout.toast.deleteFailed'])).toBeNull();
});
