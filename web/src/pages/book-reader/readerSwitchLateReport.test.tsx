/** @vitest-environment jsdom */
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { apiClient } from '../../api/client';
import { useReaderOffline } from './useReaderOffline';
import { useReaderProgressIndicator } from './useReaderProgressIndicator';
import { getQueuedOfflineProgress } from './offlineReader';
const adapter = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = adapter; vi.restoreAllMocks(); localStorage.clear(); });
it('queues a late failed report for its original book without polluting the new book UI', async () => {
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false);
  vi.spyOn(console, 'error').mockImplementation(() => {});
  let reject: ((error: Error) => void) | undefined;
  apiClient.defaults.adapter = () => new Promise((_, fail) => { reject = fail; });
  const view = renderHook(({ id }: { id: string }) => {
    const offline = useReaderOffline({ bookId: id, bookTitle: id, pages: [], imageFilter: 'none', autoCrop: false, readerImageFormat: 'original', readerImageQuality: 80, getImageUrlForBook: () => '', t: key => key });
    const progress = useReaderProgressIndicator({ bookId: id, pagesBookIdRef: { current: id }, loading: false, isOnline: true, offlineQueuedPage: offline.offlineQueuedPage, queueOfflineReaderProgress: offline.queueProgress });
    return { offline, progress };
  }, { initialProps: { id: 'A' } });
  act(() => view.result.current.progress.updateProgress(17));
  await waitFor(() => expect(reject).toBeDefined());
  view.rerender({ id: 'B' });
  await act(async () => reject?.(new Error('network gone')));
  expect(getQueuedOfflineProgress('A')?.page).toBe(17);
  expect(getQueuedOfflineProgress('B')).toBeNull();
  expect(view.result.current.offline.offlineQueuedPage).toBeNull();
  expect(view.result.current.progress.status).toBe('idle');
});
