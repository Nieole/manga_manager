import type { Page } from './types';
import { getCsrfToken } from '../../utils/apiAuth';

// syncRequestHeaders 构造离线进度回传所需的请求头。
//
// 这两个端点是 POST，服务端的 authGate 对改写类方法强制校验 X-CSRF-Token，
// 缺了它离线进度会全部 403 且永远同步不回去。同源 fetch 默认就带 Cookie
//（credentials 默认 same-origin），这里显式写出以免日后被改错。
function syncRequestHeaders(): HeadersInit {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const csrf = getCsrfToken();
  if (csrf) {
    headers['X-CSRF-Token'] = csrf;
  }
  return headers;
}

const OFFLINE_BOOK_CACHE = 'manga-manager-offline-books-v1';
const OFFLINE_BOOKS_KEY = 'manga-manager:offline-books';
const OFFLINE_PROGRESS_KEY = 'manga-manager:offline-progress';
// OFFLINE_OWNER_KEY 记住这台设备上的离线数据属于哪个用户。
// 没有它就无从判断「换人了」——而共享设备上最常见的情形恰恰是上一个人直接关窗口、
// 从不点登出，登出时的清理根本不会被触发。
const OFFLINE_OWNER_KEY = 'manga-manager:offline-owner';

export interface OfflineBookStatus {
  bookId: string;
  title: string;
  pageCount: number;
  cachedPages: number;
  cachedAt: string;
  imageProfile: string;
}

export interface OfflineReaderStorageStats {
  bookCount: number;
  cachedPages: number;
  totalPages: number;
  cacheBytes: number;
  storageUsage?: number;
  storageQuota?: number;
}

interface OfflineBookMeta extends OfflineBookStatus {
  urls: string[];
  // downloadToken 标记「这条索引属于哪一次下载」。下载收尾前比对它，就能认出自己是否
  // 已被作废——用户删了这本书、换用户清空了索引、或另一次下载接管了它。
  downloadToken?: string;
}

export interface QueuedProgress {
  bookId: string;
  page: number;
  updatedAt: string;
  title?: string;
}

export interface OfflineProgressSyncResult {
  synced: number;
  failed: number;
  remaining: number;
}

export interface CacheOfflineBookOptions {
  bookId: string;
  title: string;
  pages: Page[];
  imageProfile: string;
  imageUrlForPage: (page: Page) => string;
  onProgress?: (cachedPages: number, pageCount: number) => void;
}

function readJSON<T>(key: string, fallback: T): T {
  try {
    const raw = window.localStorage.getItem(key);
    return raw ? JSON.parse(raw) as T : fallback;
  } catch {
    return fallback;
  }
}

function writeJSON<T>(key: string, value: T) {
  window.localStorage.setItem(key, JSON.stringify(value));
}

function absoluteURL(path: string) {
  return new URL(path, window.location.origin).toString();
}

function samePath(url: string, path: string) {
  try {
    return new URL(url).pathname === path;
  } catch {
    return false;
  }
}

function pagePathPrefix(bookId: string) {
  return `/api/pages/${bookId}/`;
}

// pageImageCacheKey 把页图地址归一化成不带 query 的绝对地址。
//
// 一页对应一份字节，真相来源是页路径而不是完整 URL：query 里只有画质、格式、滤镜这些
// 渲染选项，同一页的不同渲染彼此可替代，Service Worker 读图时正是按 ignoreSearch 命中的。
// 带 query 落盘就会让同一页存下多份互不可达的字节：计数按页路径去重、读取只取其中一份，
// 多出来的那些永远读不到，纯占磁盘。请求仍按原地址发，用户拿到的是自己选的画质。
function pageImageCacheKey(url: string) {
  const absolute = new URL(url, window.location.origin);
  absolute.search = '';
  absolute.hash = '';
  return absolute.toString();
}

// belongsToBook 判断一个缓存键是不是这本书的字节（页图、页清单、书籍信息、阅读器壳）。
function belongsToBook(url: string, bookId: string) {
  try {
    return new URL(url).pathname.startsWith(pagePathPrefix(bookId))
      || samePath(url, `/api/pages/${bookId}`)
      || samePath(url, `/api/book-info/${bookId}`)
      || samePath(url, `/reader/${bookId}`);
  } catch {
    return false;
  }
}

export function supportsOfflineReaderCache() {
  return typeof window !== 'undefined' && 'caches' in window && 'serviceWorker' in navigator;
}

function readBookMeta(): Record<string, OfflineBookMeta> {
  return readJSON<Record<string, OfflineBookMeta>>(OFFLINE_BOOKS_KEY, {});
}

function writeBookMeta(meta: Record<string, OfflineBookMeta>) {
  writeJSON(OFFLINE_BOOKS_KEY, meta);
}

// hasOfflineOwner 回答「这台设备上有没有人登录过并认领了本地的离线数据」。
export function hasOfflineOwner(): boolean {
  try {
    return localStorage.getItem(OFFLINE_OWNER_KEY) !== null;
  } catch {
    // localStorage 不可用（隐私模式/配额）：认不出归属，一律当作没有。
    return false;
  }
}

// isOfflineBookDownloaded 回答「书目索引里有没有这本书」。索引归当前 owner 所有——
// reconcileOfflineOwner 换人时把它整份清空，所以这同时是「这本书是不是当前这个人下载的」。
export function isOfflineBookDownloaded(bookId: string): boolean {
  return Object.prototype.hasOwnProperty.call(readBookMeta(), bookId);
}

export async function getOfflineBookStatus(bookId: string): Promise<OfflineBookStatus | null> {
  if (!supportsOfflineReaderCache()) return null;
  const meta = readBookMeta()[bookId];
  if (!meta) return null;

  const cache = await caches.open(OFFLINE_BOOK_CACHE);
  const keys = await cache.keys();
  // 按页路径去重：同一页若因旧版本遗留了多个带 query 的键，它仍只算一页已缓存，
  // 否则改过几次画质的书会报出超过总页数的进度。
  const cachedPagePaths = new Set<string>();
  for (const request of keys) {
    try {
      const path = new URL(request.url).pathname;
      if (path.startsWith(pagePathPrefix(bookId))) cachedPagePaths.add(path);
    } catch {
      // 解析不了的键不是这本书的页。
    }
  }

  return {
    bookId,
    title: meta.title,
    pageCount: meta.pageCount,
    cachedPages: cachedPagePaths.size,
    cachedAt: meta.cachedAt,
    imageProfile: meta.imageProfile,
  };
}

export async function listOfflineBooks(): Promise<OfflineBookStatus[]> {
  if (!supportsOfflineReaderCache()) return [];
  const meta = readBookMeta();
  const statuses = await Promise.all(Object.keys(meta).map((bookId) => getOfflineBookStatus(bookId)));
  return statuses
    .filter((item): item is OfflineBookStatus => Boolean(item))
    .sort((a, b) => b.cachedAt.localeCompare(a.cachedAt));
}

export async function getOfflineReaderStorageStats(): Promise<OfflineReaderStorageStats> {
  if (!supportsOfflineReaderCache()) {
    return { bookCount: 0, cachedPages: 0, totalPages: 0, cacheBytes: 0 };
  }

  const books = await listOfflineBooks();
  const cache = await caches.open(OFFLINE_BOOK_CACHE);
  const keys = await cache.keys();
  let cacheBytes = 0;
  for (const request of keys) {
    const response = await cache.match(request);
    if (!response) continue;
    const blob = await response.clone().blob();
    cacheBytes += blob.size;
  }

  const estimate = navigator.storage?.estimate ? await navigator.storage.estimate() : {};
  return {
    bookCount: books.length,
    cachedPages: books.reduce((sum, book) => sum + book.cachedPages, 0),
    totalPages: books.reduce((sum, book) => sum + book.pageCount, 0),
    cacheBytes,
    storageUsage: estimate.usage,
    storageQuota: estimate.quota,
  };
}

// downloadSequence 只用于让同一会话里先后发起的下载拿到互不相同的令牌。
let downloadSequence = 0;

function nextDownloadToken() {
  downloadSequence += 1;
  return `${Math.random().toString(36).slice(2)}-${downloadSequence}`;
}

// stillOwnsDownload 回答「这次下载还算数吗」。索引里这本书的令牌被换掉或整条不见了，
// 就说明它已被作废：用户删了这本书、换用户清空了索引、或另一次下载接管了它。
function stillOwnsDownload(bookId: string, token: string) {
  return readBookMeta()[bookId]?.downloadToken === token;
}

// abandonDownload 丢弃一次已被作废的下载，把它写下的字节清掉。
//
// 只清自己写的、且当前索引没有认领的键：另一次下载接管这本书时，同名的键归它，
// 删掉就等于把别人下好的页挖走。
async function abandonDownload(cache: Cache, bookId: string, writtenKeys: string[]) {
  const claimed = new Set(readBookMeta()[bookId]?.urls ?? []);
  await Promise.all(writtenKeys.filter((key) => !claimed.has(key)).map((key) => cache.delete(key)));
}

// cacheBookForOffline 把一本书整份下载到离线缓存。返回 null 表示这次下载在收尾完成前已被
// 作废（删除、换用户或另一次下载接管），调用方应当据此认为这本书不在本机，而不是显示成已缓存。
export async function cacheBookForOffline({
  bookId,
  title,
  pages,
  imageProfile,
  imageUrlForPage,
  onProgress,
}: CacheOfflineBookOptions): Promise<OfflineBookStatus | null> {
  if (!supportsOfflineReaderCache()) {
    throw new Error('Offline reader cache is not supported by this browser.');
  }

  const cache = await caches.open(OFFLINE_BOOK_CACHE);
  // 静态三件套：页清单、书籍信息、阅读器壳。缺任何一件，离线打开时都进不到阅读界面。
  const downloads = [
    `/api/pages/${bookId}`,
    `/api/book-info/${bookId}`,
    `/reader/${bookId}`,
  ].map((url) => ({ requestUrl: url, cacheKey: absoluteURL(url), isPage: false }));
  for (const page of pages) {
    const requestUrl = imageUrlForPage(page);
    downloads.push({ requestUrl, cacheKey: pageImageCacheKey(requestUrl), isPage: true });
  }
  const cacheKeys = downloads.map((item) => item.cacheKey);
  let cachedPages = 0;

  // 必须先登记索引再下载：若反过来，下载中途断网时已缓存的响应留在 Cache Storage 里，
  // 而书目索引里没有这本书——离线书架看不见它，deleteOfflineBook 也删不掉（它按索引里
  // 的 urls 清理），用户只剩「清空全部」这一个出口。先写索引后，中途失败留下的是一本
  // 「未完成」的书：看得见、能单独删、能重下。
  const startedAt = new Date().toISOString();
  const token = nextDownloadToken();
  const pendingMeta = readBookMeta();
  pendingMeta[bookId] = {
    bookId,
    title,
    pageCount: pages.length,
    cachedPages: 0,
    cachedAt: startedAt,
    imageProfile,
    // 整份键列表先记全：删除按它清理，没下到的那些 cache.delete 返回 false，无副作用。
    urls: cacheKeys,
    downloadToken: token,
  };
  writeBookMeta(pendingMeta);

  const written: string[] = [];
  try {
    for (const item of downloads) {
      // 每下一件之前先确认自己还算数：用户一删就立刻停手，省下剩余流量。
      if (!stillOwnsDownload(bookId, token)) {
        await abandonDownload(cache, bookId, written);
        return null;
      }
      const response = await fetch(new Request(absoluteURL(item.requestUrl), { credentials: 'same-origin' }));
      if (!response.ok) {
        throw new Error(`Failed to cache ${item.requestUrl}: ${response.status}`);
      }
      await cache.put(new Request(item.cacheKey, { credentials: 'same-origin' }), response.clone());
      written.push(item.cacheKey);
      if (item.isPage) {
        cachedPages += 1;
        onProgress?.(cachedPages, pages.length);
      }
    }
  } catch (err) {
    // 作废与下载失败可能同时发生（删除清掉字节，正在飞的请求随后失败）。
    // 先按作废处置：留一本没人认领的半成品比留一堆孤儿字节更糟。
    if (!stillOwnsDownload(bookId, token)) {
      await abandonDownload(cache, bookId, written);
      return null;
    }
    throw err;
  }

  if (!stillOwnsDownload(bookId, token)) {
    await abandonDownload(cache, bookId, written);
    return null;
  }

  // 清掉这本书名下不属于本次下载的键：旧版本按带 query 的地址落盘的页图靠覆盖写清不掉，
  // 页数变少后多出来的页同理。留着它们就是读不到又删不掉的占用。
  await pruneStaleBookEntries(cache, bookId, new Set(cacheKeys));

  // 全部落盘后把计数补齐（cachedAt 沿用开始时刻，离线书架按它排序）。
  //
  // 改的必须是**当前索引里仍带着自己令牌的那条**，而不是整条 pendingMeta 覆盖回去：
  // 上面那段清扫是一整段 await，用户在里面删掉这本书或清空全部，光靠再补一次归属检查
  // 也挡不住——检查与写回之间还隔着一次 readBookMeta。读、判、写连成一段没有 await 的
  // 同步动作才没有窗口，否则删掉的书会连同 urls 与令牌一起复活成一本读不了的僵尸书。
  const allMeta = readBookMeta();
  const owned = allMeta[bookId];
  if (owned?.downloadToken !== token) {
    await abandonDownload(cache, bookId, written);
    return null;
  }
  allMeta[bookId] = { ...owned, cachedPages };
  writeBookMeta(allMeta);

  // 读回状态同样横跨一段 await。这中间被作废就不能再交出状态——调用方会照它把这本书
  // 显示成还在本机，而索引里已经没有它了。
  const status = await getOfflineBookStatus(bookId);
  if (!status || !stillOwnsDownload(bookId, token)) {
    await abandonDownload(cache, bookId, written);
    return null;
  }
  return status;
}

async function pruneStaleBookEntries(cache: Cache, bookId: string, keep: Set<string>) {
  const keys = await cache.keys();
  await Promise.all(keys.map((request) => (
    belongsToBook(request.url, bookId) && !keep.has(request.url)
      ? cache.delete(request)
      : Promise.resolve(false)
  )));
}

export async function deleteOfflineBook(bookId: string) {
  if (!supportsOfflineReaderCache()) return;
  const cache = await caches.open(OFFLINE_BOOK_CACHE);
  const allMeta = readBookMeta();
  const meta = allMeta[bookId];
  if (meta) {
    await Promise.all(meta.urls.map((url) => cache.delete(url)));
  }

  // 再按路径兜底扫一遍：索引里的 urls 可能是旧版本写下的形态，只删它会漏掉字节。
  const keys = await cache.keys();
  await Promise.all(keys.map((request) => (
    belongsToBook(request.url, bookId) ? cache.delete(request) : Promise.resolve(false)
  )));

  delete allMeta[bookId];
  writeBookMeta(allMeta);
}

export async function deleteAllOfflineBooks() {
  if (!supportsOfflineReaderCache()) return;
  await caches.delete(OFFLINE_BOOK_CACHE);
  writeBookMeta({});
}

function readQueuedProgress(): Record<string, QueuedProgress> {
  return readJSON<Record<string, QueuedProgress>>(OFFLINE_PROGRESS_KEY, {});
}

function writeQueuedProgress(progress: Record<string, QueuedProgress>) {
  writeJSON(OFFLINE_PROGRESS_KEY, progress);
}

export function queueOfflineProgress(bookId: string, page: number, title?: string) {
  const progress = readQueuedProgress();
  progress[bookId] = { bookId, page, title, updatedAt: new Date().toISOString() };
  writeQueuedProgress(progress);
}

export function getQueuedOfflineProgress(bookId: string): QueuedProgress | null {
  return readQueuedProgress()[bookId] ?? null;
}

export function listQueuedOfflineProgress(): QueuedProgress[] {
  const books = readBookMeta();
  return Object.values(readQueuedProgress())
    .map((item) => ({
      ...item,
      title: item.title || books[item.bookId]?.title,
    }))
    .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
}

export function deleteQueuedOfflineProgress(bookId: string) {
  const progress = readQueuedProgress();
  delete progress[bookId];
  writeQueuedProgress(progress);
}

export function clearQueuedOfflineProgress() {
  writeQueuedProgress({});
}

// dropOfflineData 清掉本机的离线残留：待回传队列、书目索引、缓存字节。
//
// 三样都要清：Service Worker 在断网时是直接从 Cache Storage 命中的，**不经过任何服务端鉴权**。
// 索引是隔离的判定点——离线放行按它逐本认，见 isOfflineBookDownloaded，清它是同步的、必然生效；
// 字节的删除是尽力而为的清扫，把内容从这台设备上抹掉。
function dropOfflineData() {
  clearQueuedOfflineProgress();
  writeBookMeta({});
  dropOfflineBookBytes();
}

// releaseOfflineOwner 交还这台设备：用户显式登出时把本机离线残留整份清掉。
//
// 只有登出走这里。会话过期（后端答未登录 / 401）与断网都不是「我要离开这台设备」，
// 那时用户还是同一个人，清了就是删他自己的书和进度，见 AuthProvider。
export function releaseOfflineOwner(): void {
  try {
    localStorage.removeItem(OFFLINE_OWNER_KEY);
  } catch {
    // 忽略：清不掉标记不影响下面的清理。
  }
  dropOfflineData();
}

// reconcileOfflineOwner 对账「这台设备上的离线数据属于谁」，换人时清掉上一个人的残留。
// 返回是否真的清理过。判定点是「谁登进来了」，参数因此不收「没有用户」。
//
// 为什么不能只挂在 logout 上：换人有三条路径不经过登出——login/setup 直接建立新会话、
// 刷新页面走状态探测、共享设备上上一个人直接关窗口。而新用户只要打开任意一个阅读器页面，
// useReaderOffline 就会自动把队列里的进度同步上去，于是上一个人的阅读进度被写进了新账号。
export function reconcileOfflineOwner(userId: number): boolean {
  // 不给初值：try 里必然赋值，catch 里直接 return，那个 null 读不到。
  let previous: string | null;
  try {
    previous = localStorage.getItem(OFFLINE_OWNER_KEY);
  } catch {
    // localStorage 不可用（隐私模式/配额）：无从对账，也无从泄露，直接放行。
    return false;
  }

  const next = String(userId);
  try {
    localStorage.setItem(OFFLINE_OWNER_KEY, next);
  } catch {
    // 写不进标记时不做清理：宁可漏清也不要每次登录都把用户自己的离线数据删掉。
    return false;
  }
  // previous 为空是「升级后首次登录」——此时的残留归属未知，按当前用户认领而不是清掉，
  // 否则所有老用户升级后会平白丢一次离线书目。
  if (previous === null || previous === next) return false;

  dropOfflineData();
  return true;
}

// dropOfflineBookBytes 尽力删掉整份离线书缓存，把上一个人下载的内容从这台设备上抹掉。
//
// 只是清扫，不是隔离本身：删除是异步的，浏览器不支持 Cache Storage、页面在删完前关掉、
// 配额层报错都会让字节留下。隔离由书目索引（同步清空）与按索引逐本放行的路由判据保证。
function dropOfflineBookBytes() {
  try {
    if (typeof caches === 'undefined') return;
    void caches.delete(OFFLINE_BOOK_CACHE).catch(() => {});
  } catch {
    // 忽略：清扫失败不影响索引已经清空这件事。
  }
}

export async function syncQueuedOfflineProgress(): Promise<OfflineProgressSyncResult> {
  if (!navigator.onLine) {
    const progress = readQueuedProgress();
    return { synced: 0, failed: 0, remaining: Object.keys(progress).length };
  }
  const progress = readQueuedProgress();
  const entries = Object.entries(progress);
  if (entries.length === 0) {
    return { synced: 0, failed: 0, remaining: 0 };
  }

  const payload = buildBulkSyncItems(entries.map(([, item]) => item));

  let synced = 0;
  let failed = 0;
  let bulkOk = false;

  try {
    const response = await fetch('/api/books/bulk-progress/sync', {
      method: 'POST',
      credentials: 'same-origin',
      headers: syncRequestHeaders(),
      body: JSON.stringify({ items: payload }),
    });
    if (response.ok) {
      const data = await response.json().catch(() => null) as { results?: Array<{ book_id: number; status: string }> } | null;
      const results = data?.results ?? [];
      // 仅当确实拿到逐条结果时才据此增删队列。结果为空（响应解析失败/异常返回）不能证明服务端已处理，
      // 此时不删队列、bulkOk 保持 false，改走逐本回退——避免把未落库的离线进度误当已同步而丢弃。
      if (results.length > 0) {
        bulkOk = true;
        const successIds = new Set<number>();
        for (const row of results) {
          if (SYNC_SUCCESS_STATUSES.has(row.status)) {
            successIds.add(Number(row.book_id));
          }
        }
        for (const [bookId, item] of entries) {
          if (successIds.has(Number(item.bookId))) {
            delete progress[bookId];
            synced += 1;
          } else {
            failed += 1;
          }
        }
      }
    }
  } catch {
    // fall through to per-book fallback
  }

  if (!bulkOk) {
    synced = 0;
    failed = 0;
    for (const [bookId, item] of entries) {
      try {
        const response = await fetch(`/api/books/${bookId}/progress`, {
          method: 'POST',
          credentials: 'same-origin',
          headers: syncRequestHeaders(),
          // 带上 updated_at，让单本端点也能做「服务端已有更新进度则跳过」的陈旧判定，
          // 否则 bulk 端点不可用时逐本回退会把服务端较新的跨设备进度覆盖回退。
          body: JSON.stringify(buildFallbackProgressBody(item)),
        });
        if (response.ok) {
          delete progress[bookId];
          synced += 1;
        } else {
          failed += 1;
        }
      } catch {
        failed += 1;
      }
    }
  }

  writeQueuedProgress(progress);
  return { synced, failed, remaining: Object.keys(progress).length };
}

// 冲突解决中视为「已同步、可从队列移除」的服务端状态。
export const SYNC_SUCCESS_STATUSES = new Set(['updated', 'skipped_stale', 'skipped_unchanged']);

// buildBulkSyncItems 把队列项映射为 bulk 同步请求体（过滤非法 book_id），携带 updated_at 供服务端做陈旧判定。
export function buildBulkSyncItems(items: QueuedProgress[]) {
  return items
    .map((item) => ({ book_id: Number(item.bookId), page: item.page, updated_at: item.updatedAt }))
    .filter((row) => Number.isFinite(row.book_id) && row.book_id > 0);
}

// buildFallbackProgressBody 逐本回退请求体——必须带 updated_at，否则单本端点无从判定陈旧、会覆盖较新进度。
export function buildFallbackProgressBody(item: QueuedProgress) {
  return { page: item.page, updated_at: item.updatedAt };
}
