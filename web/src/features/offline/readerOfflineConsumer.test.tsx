/** @vitest-environment jsdom */
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { getQueuedOfflineProgress, listOfflineBooks, reconcileOfflineOwner } from './offlineReader';
import { useReaderOffline } from './useReaderOffline';

// Only browser storage and HTTP are substituted; the reader consumes the real feature.
class BrowserCache {
  entries = new Map<string, Response>();
  async put(request: Request, response: Response) { this.entries.set(request.url, response); }
  async keys() { return [...this.entries.keys()].map((url) => new Request(url)); }
  async delete(request: Request | string) {
    return this.entries.delete(typeof request === 'string' ? request : request.url);
  }
}

beforeEach(() => {
  localStorage.clear();
  const cache = new BrowserCache();
  vi.stubGlobal('caches', {
    open: async () => cache,
    delete: async () => { cache.entries.clear(); return true; },
  });
  Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: {} });
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true);
  reconcileOfflineOwner(1);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  Reflect.deleteProperty(navigator, 'serviceWorker');
  localStorage.clear();
});

function reader() {
  return renderHook(({ bookId }) => useReaderOffline({
    bookId,
    bookTitle: `Book ${bookId}`,
    pages: [{ number: 1, width: 800, height: 1200 }],
    imageFilter: 'none',
    autoCrop: false,
    readerImageFormat: 'original',
    readerImageQuality: 80,
    getImageUrlForBook: (id, page) => `/api/pages/${id}/${page}`,
    t: (key) => key,
  }), { initialProps: { bookId: '42' } });
}

it('finishes a download for the original book after navigation without assigning it to the next book', async () => {
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  vi.stubGlobal('fetch', async () => {
    await held;
    return new Response('page');
  });
  const { result, rerender } = reader();
  await act(async () => { result.current.cacheBookOffline(); });
  expect(result.current.offlineCaching).toBe(true);

  rerender({ bookId: '43' });
  expect(result.current.offlineCaching).toBe(false);
  await act(async () => { release(); });
  await waitFor(async () => expect(await listOfflineBooks()).toEqual([
    expect.objectContaining({ bookId: '42', title: 'Book 42', cachedPages: 1, pageCount: 1 }),
  ]));
  expect(result.current.offlineStatus).toBeNull();

  rerender({ bookId: '42' });
  await waitFor(() => expect(result.current.offlineStatus?.cachedPages).toBe(1));
  expect(result.current.offlineCaching).toBe(false);
});

it('reconnect uploads original facts for both books and an old acknowledgement cannot remove a new owner fact', async () => {
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false);
  const { result, rerender } = reader();
  await act(async () => { result.current.queueProgress(9, '2026-10-01T08:30:00.000Z'); });
  const firstFact = getQueuedOfflineProgress('42');
  rerender({ bookId: '43' });
  await act(async () => { result.current.queueProgress(2, '2026-10-01T09:00:00.000Z'); });
  const secondFact = getQueuedOfflineProgress('43');
  let acknowledge!: () => void;
  const held = new Promise<void>((resolve) => { acknowledge = resolve; });
  const bodies: unknown[] = [];
  vi.stubGlobal('fetch', async (_url: string, init: RequestInit) => {
    bodies.push(JSON.parse(String(init.body)));
    await held;
    return new Response(JSON.stringify({ results: [
      { book_id: 42, status: 'updated' }, { book_id: 43, status: 'updated' },
    ] }));
  });
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true);
  await act(async () => { window.dispatchEvent(new Event('online')); });
  expect(bodies).toEqual([{ items: [
    { book_id: 42, page: 9, updated_at: '2026-10-01T08:30:00.000Z', fact_id: firstFact?.factId },
    { book_id: 43, page: 2, updated_at: '2026-10-01T09:00:00.000Z', fact_id: secondFact?.factId },
  ] }]);

  // Authentication changes ownership before the previous network response arrives.
  reconcileOfflineOwner(2);
  await act(async () => { result.current.queueProgress(5, '2026-10-02T10:00:00.000Z'); });
  const newOwnerFact = getQueuedOfflineProgress('43');
  await act(async () => { acknowledge(); });
  expect(getQueuedOfflineProgress('42')).toBeNull();
  expect(getQueuedOfflineProgress('43')).toEqual(newOwnerFact);
  expect(result.current.offlineQueuedPage).toBe(5);
});
