import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { buildBulkSyncItems, buildFallbackProgressBody, syncQueuedOfflineProgress } from './offlineReader';

describe('offline progress payload builders', () => {
  it('bulk items carry updated_at and drop invalid book ids', () => {
    expect(
      buildBulkSyncItems([
        { bookId: '42', page: 5, updatedAt: 'T1' },
        { bookId: 'not-a-number', page: 1, updatedAt: 'T2' },
      ]),
    ).toEqual([{ book_id: 42, page: 5, updated_at: 'T1' }]);
  });

  // 回归：单本回退必须带 updated_at，否则 bulk 端点不可用时会把服务端较新的跨设备进度覆盖回退。
  it('per-book fallback body carries updated_at', () => {
    expect(buildFallbackProgressBody({ bookId: '42', page: 5, updatedAt: 'T1' })).toEqual({
      page: 5,
      updated_at: 'T1',
    });
  });
});

function memStorage() {
  const store = new Map<string, string>();
  return {
    store,
    getItem: (k: string) => (store.has(k) ? store.get(k)! : null),
    setItem: (k: string, v: string) => void store.set(k, String(v)),
    removeItem: (k: string) => void store.delete(k),
    clear: () => store.clear(),
  };
}

const KEY = 'manga-manager:offline-progress';

describe('syncQueuedOfflineProgress', () => {
  let ls: ReturnType<typeof memStorage>;
  beforeEach(() => {
    ls = memStorage();
    vi.stubGlobal('window', { localStorage: ls });
    vi.stubGlobal('navigator', { onLine: true });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  const queue = (entries: Record<string, unknown>) => ls.setItem(KEY, JSON.stringify(entries));
  const remaining = () => Object.keys(JSON.parse(ls.getItem(KEY) || '{}'));

  // 回归 (bug: results 为空即清空队列)：bulk 返回 200 但结果不可解析时不能删队列，应逐本回退。
  it('does not drop the queue when bulk returns 200 with an unparseable body; falls back per-book', async () => {
    queue({ '42': { bookId: '42', page: 5, updatedAt: '2020-01-01T00:00:00Z' } });
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => { throw new Error('blank'); } }) // bulk
      .mockResolvedValueOnce({ ok: true, json: async () => ({}) }); // per-book fallback
    vi.stubGlobal('fetch', fetchMock);

    const res = await syncQueuedOfflineProgress();

    expect(fetchMock).toHaveBeenCalledTimes(2);
    const [url, opts] = fetchMock.mock.calls[1] as [string, { body: string }];
    expect(url).toBe('/api/books/42/progress');
    expect(JSON.parse(opts.body)).toEqual({ page: 5, updated_at: '2020-01-01T00:00:00Z' });
    expect(res.synced).toBe(1);
    expect(remaining()).toEqual([]);
  });

  it('keeps the queue when bulk is unconfirmed AND the per-book fallback fails (no data loss)', async () => {
    queue({ '42': { bookId: '42', page: 5, updatedAt: '2020-01-01T00:00:00Z' } });
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => null }) // bulk ok but empty results
      .mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({}) }); // fallback fails
    vi.stubGlobal('fetch', fetchMock);

    const res = await syncQueuedOfflineProgress();

    expect(res.synced).toBe(0);
    expect(res.remaining).toBe(1);
    expect(remaining()).toEqual(['42']); // NOT dropped
  });

  it('drains only per-book successes reported by the bulk endpoint', async () => {
    queue({
      '42': { bookId: '42', page: 5, updatedAt: 'T1' },
      '43': { bookId: '43', page: 9, updatedAt: 'T2' },
    });
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: true,
      json: async () => ({ results: [{ book_id: 42, status: 'updated' }] }), // 43 not reported => kept
    });
    vi.stubGlobal('fetch', fetchMock);

    const res = await syncQueuedOfflineProgress();

    expect(fetchMock).toHaveBeenCalledTimes(1); // bulk confirmed, no fallback
    expect(res.synced).toBe(1);
    expect(remaining()).toEqual(['43']);
  });
});
