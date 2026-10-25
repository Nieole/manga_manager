import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { setCsrfToken } from '../../utils/apiAuth';
import {
  buildBulkSyncItems,
  buildFallbackProgressBody,
  clearQueuedOfflineProgress,
  deleteQueuedOfflineProgress,
  getQueuedOfflineProgress,
  listQueuedOfflineProgress,
  queueOfflineProgress,
  reconcileOfflineOwner,
  releaseOfflineOwner,
  syncQueuedOfflineProgress,
} from './offlineReader';

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

  it('bulk builder returns an empty list for an empty queue', () => {
    expect(buildBulkSyncItems([])).toEqual([]);
  });

  it('bulk builder drops non-positive and non-finite book ids', () => {
    expect(
      buildBulkSyncItems([
        { bookId: '0', page: 1, updatedAt: 'T0' },
        { bookId: '-3', page: 2, updatedAt: 'Tn' },
        { bookId: '', page: 3, updatedAt: 'Te' },
        { bookId: '7', page: 4, updatedAt: 'T7' },
      ]),
    ).toEqual([{ book_id: 7, page: 4, updated_at: 'T7' }]);
  });

  it('bulk builder preserves page 0 and does not leak the title field', () => {
    expect(
      buildBulkSyncItems([{ bookId: '9', page: 0, updatedAt: 'T9', title: 'Nine' }]),
    ).toEqual([{ book_id: 9, page: 0, updated_at: 'T9' }]);
  });

  it('fallback body preserves page 0 and omits the title', () => {
    expect(buildFallbackProgressBody({ bookId: '9', page: 0, updatedAt: 'T9', title: 'Nine' })).toEqual({
      page: 0,
      updated_at: 'T9',
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
    vi.stubGlobal('localStorage', ls);
    vi.stubGlobal('navigator', { onLine: true });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  const queue = (entries: Record<string, unknown>) => ls.setItem(KEY, JSON.stringify(entries));
  const remaining = () => Object.keys(JSON.parse(ls.getItem(KEY) || '{}'));

  // 这两个端点是 POST，服务端 authGate 强制校验 X-CSRF-Token；请求头缺失会被拒绝，
  // 离线进度就永远同步不回服务端。
  it('sends the CSRF token on both the bulk and the per-book sync requests', async () => {
    setCsrfToken('csrf-token-abc');
    queue({ '42': { bookId: '42', page: 5, updatedAt: '2020-01-01T00:00:00Z' } });
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({}) })   // bulk：结果为空 → 逐本回退
      .mockResolvedValueOnce({ ok: true, json: async () => ({}) });  // per-book
    vi.stubGlobal('fetch', fetchMock);

    await syncQueuedOfflineProgress();

    expect(fetchMock).toHaveBeenCalledTimes(2);
    for (const call of fetchMock.mock.calls) {
      const [, opts] = call as [string, { headers: Record<string, string> }];
      expect(opts.headers['X-CSRF-Token']).toBe('csrf-token-abc');
      expect(opts.headers['Content-Type']).toBe('application/json');
    }
    setCsrfToken('');
  });

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

  it('does not touch the network when offline and reports what is still queued', async () => {
    queue({
      '42': { bookId: '42', page: 5, updatedAt: 'T1' },
      '43': { bookId: '43', page: 9, updatedAt: 'T2' },
    });
    vi.stubGlobal('navigator', { onLine: false });
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const res = await syncQueuedOfflineProgress();

    expect(fetchMock).not.toHaveBeenCalled();
    expect(res).toEqual({ synced: 0, failed: 0, remaining: 2 });
    expect(remaining().sort()).toEqual(['42', '43']);
  });

  it.each(['bulk', 'fallback', 'bulk-failure', 'fallback-failure'])('keeps newer queued facts when a late %s response confirms the old fact', async (transport) => {
    queueOfflineProgress('42', 5, 'Book', '2026-10-20T10:00:00Z');
    let finish!: (value: unknown) => void;
    const late = new Promise((resolve) => { finish = resolve; });
    const fetchMock = vi.fn();
    if (transport.startsWith('fallback')) fetchMock.mockResolvedValueOnce({ ok: false, status: 404 });
    fetchMock.mockReturnValueOnce(late);
    vi.stubGlobal('fetch', fetchMock);
    const syncing = syncQueuedOfflineProgress();
    if (transport.startsWith('fallback')) await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    queueOfflineProgress('42', 9, 'Book', '2026-10-20T11:00:00Z');
    queueOfflineProgress('43', 3, 'Other', '2026-10-20T11:30:00Z');
    finish({ ok: !transport.endsWith('failure'), json: async () => ({ results: [{ book_id: 42, status: 'updated' }] }) });
    const result = await syncing;
    expect(getQueuedOfflineProgress('42')?.page).toBe(9);
    expect(getQueuedOfflineProgress('43')?.page).toBe(3);
    expect(result.remaining).toBe(2);
  });
  it.each(['bulk-success', 'bulk-failure', 'fallback-failure', 'logout'])('stops the old owner sync after %s returns late', async (scenario) => {
    reconcileOfflineOwner(1);
    queueOfflineProgress('42', 5, 'Old', '2026-10-20T10:00:00Z');
    queueOfflineProgress('43', 6, 'Old second', '2026-10-20T10:01:00Z');
    let finish!: (value: unknown) => void;
    const late = new Promise((resolve) => { finish = resolve; });
    const fetchMock = vi.fn();
    if (scenario === 'fallback-failure') fetchMock.mockResolvedValueOnce({ ok: false, status: 404 });
    fetchMock.mockReturnValueOnce(late);
    vi.stubGlobal('fetch', fetchMock);
    const syncing = syncQueuedOfflineProgress();
    if (scenario === 'fallback-failure') await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    if (scenario === 'logout') releaseOfflineOwner();
    else {
      reconcileOfflineOwner(2);
      queueOfflineProgress('42', 9, 'New', '2026-10-20T11:00:00Z');
    }
    finish({ ok: scenario === 'bulk-success', json: async () => ({ results: [{ book_id: 42, status: 'updated' }] }) });
    await syncing;
    expect(fetchMock).toHaveBeenCalledTimes(scenario === 'fallback-failure' ? 2 : 1);
    expect(listQueuedOfflineProgress().map((item) => item.page)).toEqual(scenario === 'logout' ? [] : [9]);
  });
  it('retains a new same-tick queue fact and sends one stable identity through fallback and retry', async () => {
    queueOfflineProgress('42', 5, 'Book', '2026-10-20T10:00:00Z');
    let finish!: (value: unknown) => void;
    const late = new Promise((resolve) => { finish = resolve; });
    const fetchMock = vi.fn().mockResolvedValueOnce({ ok: false }).mockReturnValueOnce(late);
    vi.stubGlobal('fetch', fetchMock);
    const syncing = syncQueuedOfflineProgress();
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    const bulk = JSON.parse(fetchMock.mock.calls[0][1].body).items[0];
    const fallback = JSON.parse(fetchMock.mock.calls[1][1].body);
    expect(bulk.fact_id).toEqual(expect.any(String));
    expect(fallback.fact_id).toBe(bulk.fact_id);
    queueOfflineProgress('42', 5, 'Book', '2026-10-20T10:00:00Z');
    finish({ ok: true });
    await syncing;
    expect(getQueuedOfflineProgress('42')).not.toBeNull();
    fetchMock.mockResolvedValue({ ok: false });
    await syncQueuedOfflineProgress();
    const retryBulk = JSON.parse(fetchMock.mock.calls[2][1].body).items[0];
    const retryFallback = JSON.parse(fetchMock.mock.calls[3][1].body);
    expect(retryBulk.fact_id).not.toBe(bulk.fact_id);
    expect(retryFallback.fact_id).toBe(retryBulk.fact_id);
  });
  it('short-circuits without fetching when the queue is empty', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const res = await syncQueuedOfflineProgress();

    expect(fetchMock).not.toHaveBeenCalled();
    expect(res).toEqual({ synced: 0, failed: 0, remaining: 0 });
  });
});

describe('offline progress queue read/write/list', () => {
  let ls: ReturnType<typeof memStorage>;
  beforeEach(() => {
    ls = memStorage();
    vi.stubGlobal('window', { localStorage: ls });
    vi.stubGlobal('localStorage', ls);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  const PROGRESS = 'manga-manager:offline-progress';
  const BOOKS = 'manga-manager:offline-books';
  const readQueue = () => JSON.parse(ls.getItem(PROGRESS) || '{}') as Record<string, { page: number }>;

  it('round-trips a queued entry and reads it back by book id', () => {
    queueOfflineProgress('42', 5, 'Book 42');
    const got = getQueuedOfflineProgress('42');
    expect(got?.bookId).toBe('42');
    expect(got?.page).toBe(5);
    expect(got?.title).toBe('Book 42');
    expect(typeof got?.updatedAt).toBe('string');
  });

  it('returns null for a book id that was never queued', () => {
    expect(getQueuedOfflineProgress('nope')).toBeNull();
  });

  // 去重：同一 bookId 多次入队只保留一条，页码被最新写入覆盖。
  it('dedupes by book id, keeping only the latest page for that book', () => {
    queueOfflineProgress('42', 5);
    queueOfflineProgress('42', 12);
    queueOfflineProgress('43', 1);
    const q = readQueue();
    expect(Object.keys(q).sort()).toEqual(['42', '43']);
    expect(q['42'].page).toBe(12);
    expect(getQueuedOfflineProgress('42')?.page).toBe(12);
  });

  // listQueuedOfflineProgress 按 updatedAt 倒序，并在缺省 title 时回填离线书目元数据里的标题。
  it('lists newest-first and backfills missing titles from cached book meta', () => {
    ls.setItem(
      PROGRESS,
      JSON.stringify({
        '42': { bookId: '42', page: 5, updatedAt: '2020-01-01T00:00:00Z' },
        '43': { bookId: '43', page: 9, updatedAt: '2020-03-01T00:00:00Z', title: 'Explicit' },
      }),
    );
    ls.setItem(BOOKS, JSON.stringify({ '42': { title: 'From Meta' } }));

    const list = listQueuedOfflineProgress();

    expect(list.map((i) => i.bookId)).toEqual(['43', '42']); // 43 is newer -> first
    expect(list[0].title).toBe('Explicit'); // own title wins over meta
    expect(list[1].title).toBe('From Meta'); // missing title backfilled from meta
  });

  it('deletes a single queued entry and clears the whole queue', () => {
    queueOfflineProgress('42', 5);
    queueOfflineProgress('43', 9);

    deleteQueuedOfflineProgress('42');
    expect(Object.keys(readQueue())).toEqual(['43']);

    clearQueuedOfflineProgress();
    expect(readQueue()).toEqual({});
  });
});
