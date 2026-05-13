import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { AlertTriangle, BookOpen, CheckCircle2, Clock3, Database, HardDriveDownload, Loader2, RefreshCw, Send, Trash2, WifiOff, XCircle } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';
import {
  clearQueuedOfflineProgress,
  deleteAllOfflineBooks,
  deleteOfflineBook,
  deleteQueuedOfflineProgress,
  getOfflineReaderStorageStats,
  listQueuedOfflineProgress,
  listOfflineBooks,
  supportsOfflineReaderCache,
  syncQueuedOfflineProgress,
  type OfflineBookStatus,
  type OfflineProgressSyncResult,
  type OfflineReaderStorageStats,
  type QueuedProgress,
} from './book-reader/offlineReader';

function formatBytes(value: number | undefined) {
  if (!value || !Number.isFinite(value) || value <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  const digits = unitIndex === 0 ? 0 : size >= 10 ? 1 : 2;
  return `${size.toFixed(digits)} ${units[unitIndex]}`;
}

function formatDateTime(value: string) {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

export default function OfflineShelf() {
  const { t } = useI18n();
  const [books, setBooks] = useState<OfflineBookStatus[]>([]);
  const [queuedProgress, setQueuedProgress] = useState<QueuedProgress[]>([]);
  const [stats, setStats] = useState<OfflineReaderStorageStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deletingProgressId, setDeletingProgressId] = useState<string | null>(null);
  const [clearingAll, setClearingAll] = useState(false);
  const [clearingProgress, setClearingProgress] = useState(false);
  const [syncingProgress, setSyncingProgress] = useState(false);
  const [syncResult, setSyncResult] = useState<OfflineProgressSyncResult | null>(null);
  const [isOnline, setIsOnline] = useState(() => typeof navigator === 'undefined' ? true : navigator.onLine);
  const offlineSupported = supportsOfflineReaderCache();

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [nextBooks, nextStats] = await Promise.all([
        listOfflineBooks(),
        getOfflineReaderStorageStats(),
      ]);
      setBooks(nextBooks);
      setStats(nextStats);
      setQueuedProgress(listQueuedOfflineProgress());
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    const handleOnline = () => setIsOnline(true);
    const handleOffline = () => setIsOnline(false);
    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);
    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, []);

  const handleDeleteBook = useCallback(async (bookId: string) => {
    setDeletingId(bookId);
    try {
      await deleteOfflineBook(bookId);
      await refresh();
    } finally {
      setDeletingId(null);
    }
  }, [refresh]);

  const handleClearAll = useCallback(async () => {
    setClearingAll(true);
    try {
      await deleteAllOfflineBooks();
      await refresh();
    } finally {
      setClearingAll(false);
    }
  }, [refresh]);

  const handleSyncProgress = useCallback(async () => {
    setSyncingProgress(true);
    try {
      const result = await syncQueuedOfflineProgress();
      setSyncResult(result);
      await refresh();
    } finally {
      setSyncingProgress(false);
    }
  }, [refresh]);

  const handleDeleteQueuedProgress = useCallback(async (bookId: string) => {
    setDeletingProgressId(bookId);
    try {
      deleteQueuedOfflineProgress(bookId);
      setSyncResult(null);
      await refresh();
    } finally {
      setDeletingProgressId(null);
    }
  }, [refresh]);

  const handleClearQueuedProgress = useCallback(async () => {
    setClearingProgress(true);
    try {
      clearQueuedOfflineProgress();
      setSyncResult(null);
      await refresh();
    } finally {
      setClearingProgress(false);
    }
  }, [refresh]);

  const quotaPercent = stats?.storageQuota && stats.storageQuota > 0
    ? Math.min(100, Math.round(((stats.storageUsage ?? 0) / stats.storageQuota) * 100))
    : 0;

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-4 py-6 sm:px-6 lg:px-8">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <div className="inline-flex items-center gap-2 rounded-full border border-komgaPrimary/20 bg-komgaPrimary/10 px-3 py-1 text-xs font-medium text-komgaPrimary">
            <HardDriveDownload className="h-3.5 w-3.5" />
            {t('offlineShelf.badge')}
          </div>
          <h1 className="mt-3 text-3xl font-bold text-white">{t('offlineShelf.title')}</h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-gray-400">{t('offlineShelf.description')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <span className={`inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-sm ${isOnline ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-200' : 'border-amber-500/25 bg-amber-500/10 text-amber-200'}`}>
            {isOnline ? <CheckCircle2 className="h-4 w-4" /> : <WifiOff className="h-4 w-4" />}
            {isOnline ? t('offlineShelf.online') : t('offlineShelf.offline')}
          </span>
          <button
            type="button"
            onClick={refresh}
            disabled={loading}
            className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-900/70 px-3 py-2 text-sm text-gray-200 hover:bg-gray-800 disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
            {t('common.refresh')}
          </button>
          <button
            type="button"
            onClick={handleClearAll}
            disabled={clearingAll || books.length === 0}
            className="inline-flex items-center gap-2 rounded-lg border border-red-500/25 bg-red-500/10 px-3 py-2 text-sm text-red-200 hover:bg-red-500/15 disabled:opacity-50"
          >
            {clearingAll ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
            {t('offlineShelf.clearAll')}
          </button>
        </div>
      </div>

      {!offlineSupported && (
        <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 p-4 text-sm text-amber-100">
          {t('offlineShelf.unsupported')}
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
        <div className="rounded-lg border border-gray-800 bg-gray-900/60 p-4">
          <p className="text-xs uppercase tracking-wide text-gray-500">{t('offlineShelf.metric.books')}</p>
          <p className="mt-2 text-2xl font-semibold text-white">{stats?.bookCount ?? 0}</p>
        </div>
        <div className="rounded-lg border border-gray-800 bg-gray-900/60 p-4">
          <p className="text-xs uppercase tracking-wide text-gray-500">{t('offlineShelf.metric.pages')}</p>
          <p className="mt-2 text-2xl font-semibold text-white">{stats?.cachedPages ?? 0} / {stats?.totalPages ?? 0}</p>
        </div>
        <div className="rounded-lg border border-gray-800 bg-gray-900/60 p-4">
          <p className="text-xs uppercase tracking-wide text-gray-500">{t('offlineShelf.metric.cacheSize')}</p>
          <p className="mt-2 text-2xl font-semibold text-white">{formatBytes(stats?.cacheBytes)}</p>
        </div>
        <div className="rounded-lg border border-gray-800 bg-gray-900/60 p-4">
          <p className="text-xs uppercase tracking-wide text-gray-500">{t('offlineShelf.metric.storage')}</p>
          <p className="mt-2 text-2xl font-semibold text-white">{quotaPercent}%</p>
          <p className="mt-1 text-xs text-gray-500">
            {formatBytes(stats?.storageUsage)} / {formatBytes(stats?.storageQuota)}
          </p>
        </div>
        <div className="rounded-lg border border-amber-500/15 bg-amber-500/5 p-4">
          <p className="text-xs uppercase tracking-wide text-amber-200/70">{t('offlineShelf.metric.pendingSync')}</p>
          <p className="mt-2 text-2xl font-semibold text-amber-100">{queuedProgress.length}</p>
        </div>
      </div>

      <section className="rounded-lg border border-gray-800 bg-gray-950/50">
        <div className="flex flex-col gap-3 border-b border-gray-800 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-2 text-gray-200">
            <Clock3 className="h-4 w-4 text-amber-200" />
            <div>
              <h2 className="text-sm font-semibold">{t('offlineShelf.queueTitle')}</h2>
              <p className="mt-1 text-xs text-gray-500">{t('offlineShelf.queueDescription')}</p>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={handleSyncProgress}
              disabled={syncingProgress || queuedProgress.length === 0 || !isOnline}
              className="inline-flex items-center gap-2 rounded-lg border border-komgaPrimary/25 bg-komgaPrimary/10 px-3 py-2 text-sm text-komgaPrimary hover:bg-komgaPrimary/20 disabled:opacity-50"
            >
              {syncingProgress ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
              {t('offlineShelf.syncNow')}
            </button>
            <button
              type="button"
              onClick={handleClearQueuedProgress}
              disabled={clearingProgress || queuedProgress.length === 0}
              className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-gray-300 hover:bg-gray-800 disabled:opacity-50"
            >
              {clearingProgress ? <Loader2 className="h-4 w-4 animate-spin" /> : <XCircle className="h-4 w-4" />}
              {t('offlineShelf.clearQueue')}
            </button>
          </div>
        </div>
        {syncResult && (
          <div className="border-b border-gray-800 px-4 py-3 text-xs text-gray-400">
            {t('offlineShelf.syncResult', {
              synced: syncResult.synced,
              failed: syncResult.failed,
              remaining: syncResult.remaining,
            })}
          </div>
        )}
        {queuedProgress.length === 0 ? (
          <div className="flex min-h-28 items-center gap-3 px-4 py-6 text-sm text-gray-500">
            <CheckCircle2 className="h-5 w-5 text-emerald-300" />
            {t('offlineShelf.queueEmpty')}
          </div>
        ) : (
          <div className="divide-y divide-gray-800">
            {queuedProgress.map((item) => (
              <div key={item.bookId} className="grid gap-3 px-4 py-4 md:grid-cols-[1fr_auto] md:items-center">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <AlertTriangle className="h-4 w-4 shrink-0 text-amber-200" />
                    <h3 className="truncate text-sm font-medium text-white">{item.title || t('offlineShelf.unknownBook', { id: item.bookId })}</h3>
                  </div>
                  <p className="mt-2 text-xs text-gray-500">
                    {t('offlineShelf.queueItem', {
                      page: item.page,
                      time: formatDateTime(item.updatedAt),
                    })}
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <Link
                    to={`/reader/${item.bookId}`}
                    className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-gray-300 hover:bg-gray-800"
                  >
                    <BookOpen className="h-4 w-4" />
                    {t('offlineShelf.openReader')}
                  </Link>
                  <button
                    type="button"
                    onClick={() => handleDeleteQueuedProgress(item.bookId)}
                    disabled={deletingProgressId === item.bookId}
                    className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-gray-300 hover:bg-gray-800 disabled:opacity-50"
                  >
                    {deletingProgressId === item.bookId ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
                    {t('offlineShelf.dropQueued')}
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="rounded-lg border border-gray-800 bg-gray-950/50">
        <div className="flex items-center justify-between border-b border-gray-800 px-4 py-3">
          <div className="flex items-center gap-2 text-gray-200">
            <Database className="h-4 w-4 text-komgaPrimary" />
            <h2 className="text-sm font-semibold">{t('offlineShelf.listTitle')}</h2>
          </div>
          <span className="text-xs text-gray-500">{t('offlineShelf.listCount', { count: books.length })}</span>
        </div>
        {loading ? (
          <div className="flex min-h-56 items-center justify-center">
            <Loader2 className="h-8 w-8 animate-spin text-komgaPrimary" />
          </div>
        ) : books.length === 0 ? (
          <div className="flex min-h-56 flex-col items-center justify-center px-6 text-center">
            <HardDriveDownload className="h-10 w-10 text-gray-600" />
            <p className="mt-3 text-sm font-medium text-white">{t('offlineShelf.emptyTitle')}</p>
            <p className="mt-2 max-w-md text-sm leading-6 text-gray-500">{t('offlineShelf.emptyDescription')}</p>
          </div>
        ) : (
          <div className="divide-y divide-gray-800">
            {books.map((book) => {
              const percent = book.pageCount > 0 ? Math.round((book.cachedPages / book.pageCount) * 100) : 0;
              return (
                <div key={book.bookId} className="grid gap-4 px-4 py-4 lg:grid-cols-[1fr_auto] lg:items-center">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="truncate text-base font-semibold text-white">{book.title}</h3>
                      <span className="rounded-full border border-emerald-500/20 bg-emerald-500/10 px-2 py-0.5 text-[11px] text-emerald-200">
                        {percent}%
                      </span>
                    </div>
                    <div className="mt-2 grid gap-2 text-xs text-gray-500 sm:grid-cols-3">
                      <span>{t('offlineShelf.bookPages', { cached: book.cachedPages, total: book.pageCount })}</span>
                      <span>{book.imageProfile}</span>
                      <span>{t('offlineShelf.cachedAt', { time: formatDateTime(book.cachedAt) })}</span>
                    </div>
                    <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-gray-800">
                      <div className="h-full rounded-full bg-komgaPrimary" style={{ width: `${percent}%` }} />
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Link
                      to={`/reader/${book.bookId}`}
                      className="inline-flex items-center gap-2 rounded-lg border border-komgaPrimary/25 bg-komgaPrimary/10 px-3 py-2 text-sm text-komgaPrimary hover:bg-komgaPrimary/20"
                    >
                      <BookOpen className="h-4 w-4" />
                      {t('offlineShelf.openReader')}
                    </Link>
                    <button
                      type="button"
                      onClick={() => handleDeleteBook(book.bookId)}
                      disabled={deletingId === book.bookId}
                      className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-gray-300 hover:bg-gray-800 disabled:opacity-50"
                    >
                      {deletingId === book.bookId ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
                      {t('offlineShelf.remove')}
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </section>
    </div>
  );
}
