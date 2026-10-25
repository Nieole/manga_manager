// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { apiClient } from '../../api/client';
import { useReaderReadingTime } from './useReaderReadingTime';
vi.mock('../../api/client', () => ({ apiClient: { post: vi.fn() } }));
const post = vi.mocked(apiClient.post);
const advance = (ms: number) => act(() => vi.advanceTimersByTime(ms));
function visibility(value: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value });
  act(() => document.dispatchEvent(new Event('visibilitychange')));
}
beforeEach(() => {
  vi.useFakeTimers();
  post.mockReset().mockResolvedValue({});
  visibility('visible');
  Object.defineProperty(navigator, 'sendBeacon', { configurable: true, value: vi.fn(() => true) });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); });

it('reports only active visible reading and resumes after reader input', () => {
  renderHook(() => useReaderReadingTime({ bookId: '1' }));
  advance(30_000);
  expect(post).toHaveBeenLastCalledWith('/api/books/1/reading-time', { seconds: 29 });
  visibility('hidden');
  advance(120_000);
  expect(post).toHaveBeenCalledTimes(1);
  visibility('visible');
  advance(30_000);
  expect(post).toHaveBeenCalledTimes(1);
  act(() => window.dispatchEvent(new Event('keydown')));
  advance(30_000);
  expect(post).toHaveBeenCalledTimes(2);
  expect(post).toHaveBeenLastCalledWith('/api/books/1/reading-time', { seconds: expect.any(Number) });
  expect((post.mock.lastCall?.[1] as { seconds: number }).seconds).toBeGreaterThanOrEqual(29);
});

it('stops counting idle time until reader activity resumes', () => {
  renderHook(() => useReaderReadingTime({ bookId: '1' }));
  advance(120_000);
  expect(post.mock.calls.map((call) => call[1])).toEqual([{ seconds: 29 }, { seconds: 30 }, { seconds: 30 }]);
  act(() => window.dispatchEvent(new Event('wheel')));
  advance(30_000);
  expect(post).toHaveBeenLastCalledWith('/api/books/1/reading-time', { seconds: expect.any(Number) });
  expect((post.mock.lastCall?.[1] as { seconds: number }).seconds).toBeGreaterThanOrEqual(29);
});

it('settles the old book on switch and never restores a lost acknowledgement into the new book', async () => {
  let rejectOld!: (error: Error) => void;
  post.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectOld = reject; }));
  const { rerender, unmount } = renderHook(({ bookId }) => useReaderReadingTime({ bookId }), { initialProps: { bookId: '1' } });
  advance(10_000);
  rerender({ bookId: '2' });
  expect(post).toHaveBeenLastCalledWith('/api/books/1/reading-time', { seconds: 10 });
  await act(async () => { rejectOld(new Error('lost acknowledgement')); });
  advance(7_000);
  unmount();
  expect(post.mock.calls.map((call) => [call[0], call[1]])).toEqual([
    ['/api/books/1/reading-time', { seconds: 10 }], ['/api/books/2/reading-time', { seconds: 7 }],
  ]);
  advance(60_000);
  expect(post).toHaveBeenCalledTimes(2);
});

it('flushes once through beacon on hide and page exit without resending on unmount', () => {
  const { unmount } = renderHook(() => useReaderReadingTime({ bookId: '3' }));
  advance(10_000);
  visibility('hidden');
  act(() => window.dispatchEvent(new Event('pagehide')));
  unmount();
  expect(navigator.sendBeacon).toHaveBeenCalledTimes(1);
  expect(navigator.sendBeacon).toHaveBeenCalledWith('/api/books/3/reading-time', expect.any(Blob));
  expect(post).not.toHaveBeenCalled();
});

it('disables time reporting and ignores subthreshold reading', () => {
  const { rerender, unmount } = renderHook(({ enabled }) => useReaderReadingTime({ bookId: '1', enabled }), { initialProps: { enabled: false } });
  advance(60_000);
  expect(post).not.toHaveBeenCalled();
  rerender({ enabled: true });
  advance(4000);
  unmount();
  expect(post).not.toHaveBeenCalled();
});
