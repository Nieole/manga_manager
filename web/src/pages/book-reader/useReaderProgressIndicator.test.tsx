// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { apiClient } from '../../api/client';
import { useReaderProgressIndicator } from './useReaderProgressIndicator';

vi.mock('../../api/client', () => ({ apiClient: { post: vi.fn() }, isAxiosError: () => false }));
afterEach(() => { vi.restoreAllMocks(); });

it('preserves the reading fact timestamp when a network failure queues a retry', async () => {
  vi.mocked(apiClient.post).mockRejectedValueOnce(new Error('offline'));
  vi.spyOn(console, 'error').mockImplementation(() => {});
  const queue = vi.fn();
  const { result } = renderHook(() => useReaderProgressIndicator({ bookId: '7', pagesBookIdRef: { current: '7' }, loading: false, isOnline: true, offlineQueuedPage: null, queueOfflineReaderProgress: queue }));
  await act(async () => { result.current.updateProgress(5); });
  const body = vi.mocked(apiClient.post).mock.calls[0][1] as { page: number; updated_at: string };
  expect(body.updated_at).toEqual(expect.any(String));
  expect(queue).toHaveBeenCalledWith(5, body.updated_at);
});

it('reports a later same-page reading as a distinct fact', async () => {
  vi.mocked(apiClient.post).mockReset().mockResolvedValue({});
  const { result } = renderHook(() => useReaderProgressIndicator({ bookId: '7', pagesBookIdRef: { current: '7' }, loading: false, isOnline: true, offlineQueuedPage: null, queueOfflineReaderProgress: vi.fn() }));
  await act(async () => { result.current.updateProgress(5); });
  await act(async () => { result.current.updateProgress(5); });
  const calls = vi.mocked(apiClient.post).mock.calls;
  expect(calls).toHaveLength(2);
  expect(calls[0][1]).not.toEqual(calls[1][1]);
});
