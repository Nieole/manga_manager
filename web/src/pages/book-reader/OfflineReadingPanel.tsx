import { CheckCircle2, Download, Loader2, Trash2, WifiOff } from 'lucide-react';
import type { OfflineBookStatus } from './offlineReader';

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

interface OfflineReadingPanelProps {
  t: Translate;
  isOnline: boolean;
  offlineSupported: boolean;
  offlineStatus: OfflineBookStatus | null;
  offlineCaching: boolean;
  offlineDeleting: boolean;
  offlineCachedPages: number;
  activePageCount: number;
  offlineQueuedPage: number | null;
  offlineCacheError: string | null;
  readerLoading: boolean;
  onCacheBook: () => void;
  onDeleteOfflineBook: () => void;
}

export function OfflineReadingPanel({
  t,
  isOnline,
  offlineSupported,
  offlineStatus,
  offlineCaching,
  offlineDeleting,
  offlineCachedPages,
  activePageCount,
  offlineQueuedPage,
  offlineCacheError,
  readerLoading,
  onCacheBook,
  onDeleteOfflineBook,
}: OfflineReadingPanelProps) {
  const offlinePageCount = activePageCount || offlineStatus?.pageCount || 0;
  const offlineCachedCount = offlineCaching ? offlineCachedPages : offlineStatus?.cachedPages ?? 0;
  const offlineProgressPercent = offlinePageCount > 0 ? Math.round((offlineCachedCount / offlinePageCount) * 100) : 0;
  const offlineCachedAt = offlineStatus?.cachedAt ? new Date(offlineStatus.cachedAt).toLocaleString() : '';

  return (
    <div className="rounded-lg border border-gray-800 bg-gray-900/50 p-3">
      <div className="mb-2 flex items-center justify-between gap-3">
        <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider">{t('reader.offline.title')}</span>
        <span className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-medium ${isOnline ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-500' : 'border-amber-500/25 bg-amber-500/10 text-amber-500'}`}>
          {isOnline ? <CheckCircle2 className="h-3 w-3" /> : <WifiOff className="h-3 w-3" />}
          {isOnline ? t('reader.offline.online') : t('reader.offline.offline')}
        </span>
      </div>
      {!offlineSupported ? (
        <p className="text-[11px] leading-5 text-gray-500">{t('reader.offline.unsupported')}</p>
      ) : (
        <div className="space-y-3">
          <div>
            <div className="mb-1 flex items-center justify-between text-[11px] text-gray-400">
              <span>{offlineStatus ? t('reader.offline.cached') : t('reader.offline.notCached')}</span>
              <span>{offlineCachedCount} / {offlinePageCount}</span>
            </div>
            <div className="h-1.5 overflow-hidden rounded-full bg-gray-800">
              <div className="h-full rounded-full bg-komgaPrimary transition-all" style={{ width: `${offlineProgressPercent}%` }} />
            </div>
            {offlineStatus && (
              <p className="mt-2 text-[11px] leading-5 text-gray-500">
                {t('reader.offline.cachedDetail', { profile: offlineStatus.imageProfile, time: offlineCachedAt })}
              </p>
            )}
            {offlineQueuedPage && (
              <p className="mt-2 text-[11px] leading-5 text-amber-500">
                {t('reader.offline.progressQueued', { page: offlineQueuedPage })}
              </p>
            )}
            {offlineCacheError && (
              <p className="mt-2 break-all text-[11px] leading-5 text-red-300">{offlineCacheError}</p>
            )}
          </div>
          <div className="grid grid-cols-2 gap-2">
            <button
              onClick={onCacheBook}
              disabled={offlineCaching || readerLoading || offlinePageCount === 0}
              className="inline-flex items-center justify-center gap-2 rounded-lg border border-komgaPrimary/25 bg-komgaPrimary/10 px-3 py-2 text-xs text-komgaPrimary hover:bg-komgaPrimary/20 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {offlineCaching ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
              {offlineCaching ? t('reader.offline.caching') : t('reader.offline.cacheBook')}
            </button>
            <button
              onClick={onDeleteOfflineBook}
              disabled={offlineDeleting || !offlineStatus}
              className="inline-flex items-center justify-center gap-2 rounded-lg border border-gray-700 bg-gray-950 px-3 py-2 text-xs text-gray-300 hover:bg-gray-800 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {offlineDeleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
              {t('reader.offline.remove')}
            </button>
          </div>
          <p className="text-[11px] leading-5 text-gray-500">{t('reader.offline.hint')}</p>
        </div>
      )}
    </div>
  );
}
