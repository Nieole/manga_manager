// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { apiClient } from '../../api/client';
import { useReaderProgressIndicator } from './useReaderProgressIndicator';

vi.mock('../../api/client', () => ({ apiClient: { post: vi.fn() }, isAxiosError: () => false }));
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); });

it.each(['online', 'offline', 'network-failure'] as const)('keeps the real reading date at the local day boundary through %s', async (transport) => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-26T23:59:59.999+08:00'));
  vi.mocked(apiClient.post).mockReset();
  if (transport === 'network-failure') {
    vi.mocked(apiClient.post).mockRejectedValue(new Error('offline'));
    vi.spyOn(console, 'error').mockImplementation(() => {});
  } else {
    vi.mocked(apiClient.post).mockResolvedValue({});
  }
  const queue = vi.fn();
  const { result } = renderHook(() => useReaderProgressIndicator({ bookId: '7', pagesBookIdRef: { current: '7' }, loading: false, isOnline: transport !== 'offline', offlineQueuedPage: null, queueOfflineReaderProgress: queue }));
  await act(async () => { result.current.updateProgress(5); });
  await act(async () => { result.current.updateProgress(6); });
  if (transport !== 'offline') {
    expect(vi.mocked(apiClient.post).mock.calls.map((call) => call[1])).toEqual([
      { page: 5, updated_at: '2026-10-26T15:59:59.999Z' },
      { page: 6, updated_at: '2026-10-26T15:59:59.999Z' },
    ]);
  }
  if (transport !== 'online') {
    expect(queue.mock.calls).toEqual([[5, '2026-10-26T15:59:59.999Z'], [6, '2026-10-26T15:59:59.999Z']]);
  }
});

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
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-26T10:00:00+08:00'));
  vi.mocked(apiClient.post).mockReset().mockResolvedValue({});
  const { result } = renderHook(() => useReaderProgressIndicator({ bookId: '7', pagesBookIdRef: { current: '7' }, loading: false, isOnline: true, offlineQueuedPage: null, queueOfflineReaderProgress: vi.fn() }));
  await act(async () => { result.current.updateProgress(5); });
  vi.setSystemTime(new Date('2026-10-26T10:00:01+08:00'));
  await act(async () => { result.current.updateProgress(5); });
  const calls = vi.mocked(apiClient.post).mock.calls;
  expect(calls).toHaveLength(2);
  expect(calls.map((call) => call[1])).toEqual([
    { page: 5, updated_at: '2026-10-26T02:00:00.000Z' },
    { page: 5, updated_at: '2026-10-26T02:00:01.000Z' },
  ]);
});
