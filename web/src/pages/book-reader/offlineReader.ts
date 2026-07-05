/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import type { Page } from './types';

const OFFLINE_BOOK_CACHE = 'manga-manager-offline-books-v1';
const OFFLINE_BOOKS_KEY = 'manga-manager:offline-books';
const OFFLINE_PROGRESS_KEY = 'manga-manager:offline-progress';

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

export function supportsOfflineReaderCache() {
  return typeof window !== 'undefined' && 'caches' in window && 'serviceWorker' in navigator;
}

function readBookMeta(): Record<string, OfflineBookMeta> {
  return readJSON<Record<string, OfflineBookMeta>>(OFFLINE_BOOKS_KEY, {});
}

function writeBookMeta(meta: Record<string, OfflineBookMeta>) {
  writeJSON(OFFLINE_BOOKS_KEY, meta);
}

export async function getOfflineBookStatus(bookId: string): Promise<OfflineBookStatus | null> {
  if (!supportsOfflineReaderCache()) return null;
  const meta = readBookMeta()[bookId];
  if (!meta) return null;

  const cache = await caches.open(OFFLINE_BOOK_CACHE);
  const keys = await cache.keys();
  const cachedPages = keys.filter((request) => {
    try {
      return new URL(request.url).pathname.startsWith(pagePathPrefix(bookId));
    } catch {
      return false;
    }
  }).length;

  return {
    bookId,
    title: meta.title,
    pageCount: meta.pageCount,
    cachedPages,
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

export async function cacheBookForOffline({
  bookId,
  title,
  pages,
  imageProfile,
  imageUrlForPage,
  onProgress,
}: CacheOfflineBookOptions): Promise<OfflineBookStatus> {
  if (!supportsOfflineReaderCache()) {
    throw new Error('Offline reader cache is not supported by this browser.');
  }

  const cache = await caches.open(OFFLINE_BOOK_CACHE);
  const staticUrls = [
    `/api/pages/${bookId}`,
    `/api/book-info/${bookId}`,
    `/reader/${bookId}`,
  ];
  const pageUrls = pages.map(imageUrlForPage);
  const urls = [...staticUrls, ...pageUrls];
  let cachedPages = 0;

  for (const url of urls) {
    const request = new Request(absoluteURL(url), { credentials: 'same-origin' });
    const response = await fetch(request);
    if (!response.ok) {
      throw new Error(`Failed to cache ${url}: ${response.status}`);
    }
    await cache.put(request, response.clone());
    if (pageUrls.includes(url)) {
      cachedPages += 1;
      onProgress?.(cachedPages, pages.length);
    }
  }

  const allMeta = readBookMeta();
  allMeta[bookId] = {
    bookId,
    title,
    pageCount: pages.length,
    cachedPages,
    cachedAt: new Date().toISOString(),
    imageProfile,
    urls: urls.map(absoluteURL),
  };
  writeBookMeta(allMeta);

  return await getOfflineBookStatus(bookId) ?? {
    bookId,
    title,
    pageCount: pages.length,
    cachedPages,
    cachedAt: allMeta[bookId].cachedAt,
    imageProfile,
  };
}

export async function deleteOfflineBook(bookId: string) {
  if (!supportsOfflineReaderCache()) return;
  const cache = await caches.open(OFFLINE_BOOK_CACHE);
  const allMeta = readBookMeta();
  const meta = allMeta[bookId];
  if (meta) {
    await Promise.all(meta.urls.map((url) => cache.delete(url)));
  }

  const keys = await cache.keys();
  await Promise.all(keys.map((request) => {
    try {
      const url = new URL(request.url);
      if (
        url.pathname.startsWith(pagePathPrefix(bookId)) ||
        samePath(request.url, `/api/pages/${bookId}`) ||
        samePath(request.url, `/api/book-info/${bookId}`) ||
        samePath(request.url, `/reader/${bookId}`)
      ) {
        return cache.delete(request);
      }
    } catch {
      return Promise.resolve(false);
    }
    return Promise.resolve(false);
  }));

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
      headers: { 'Content-Type': 'application/json' },
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
          headers: { 'Content-Type': 'application/json' },
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
