import { useCallback, useEffect, useState } from 'react';
import axios from 'axios';
import { AlertTriangle, HardDrive, RefreshCw, Trash2 } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { SettingsPageIntro, sectionClassName } from './shared';

interface PageCacheStats {
  path: string;
  file_size: number;
  file_count: number;
}

interface StorageIODiagnostics {
  cache_dir: string;
  cache_volume: string;
  same_disk_caches: number;
  paused: boolean;
  recent_scan_archive_open_rate: number;
  recent_cover_archive_open_rate: number;
  recent_thumbnail_write_ms: number;
  scheduler: Array<{
    volume_key: string;
    active: number;
    limit: number;
    reader_active: number;
    reader_waiting: number;
    background_waiting: number;
    background_paused: boolean;
    pause_reason?: string;
  }>;
  libraries: Array<{
    id: number;
    name: string;
    path: string;
    volume_key: string;
    storage_profile: string;
    cache_on_same_volume: boolean;
    disable_same_disk_page_cache: boolean;
    heavy_background_concurrency: number;
  }>;
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B';
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

function formatRate(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0/min';
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)}/min`;
}

export function SettingsMaintenancePage() {
  const { t } = useI18n();
  const { handleAction, showToast } = useSettings();
  const [pageCacheStats, setPageCacheStats] = useState<PageCacheStats | null>(null);
  const [storageIO, setStorageIO] = useState<StorageIODiagnostics | null>(null);
  const [loadingPageCache, setLoadingPageCache] = useState(false);
  const [loadingStorageIO, setLoadingStorageIO] = useState(false);
  const [clearingPageCache, setClearingPageCache] = useState(false);

  const fetchPageCacheStats = useCallback(async () => {
    setLoadingPageCache(true);
    try {
      const res = await axios.get<PageCacheStats>('/api/system/page-cache');
      setPageCacheStats(res.data);
    } catch (error) {
      console.error(error);
      showToast(t('settings.maintenance.pageCacheStatsFailed'), 'error');
    } finally {
      setLoadingPageCache(false);
    }
  }, [showToast, t]);

  const fetchStorageIO = useCallback(async () => {
    setLoadingStorageIO(true);
    try {
      const res = await axios.get<StorageIODiagnostics>('/api/system/storage-io');
      setStorageIO(res.data);
    } catch (error) {
      console.error(error);
      showToast(t('settings.maintenance.storageIOFailed'), 'error');
    } finally {
      setLoadingStorageIO(false);
    }
  }, [showToast, t]);

  useEffect(() => {
    fetchPageCacheStats();
    fetchStorageIO();
  }, [fetchPageCacheStats, fetchStorageIO]);

  const handleClearPageCache = useCallback(async () => {
    setClearingPageCache(true);
    try {
      await axios.delete('/api/system/page-cache');
      showToast(t('settings.maintenance.pageCacheClearSuccess'), 'success');
      await fetchPageCacheStats();
    } catch (error) {
      console.error(error);
      showToast(t('settings.maintenance.pageCacheClearFailed'), 'error');
    } finally {
      setClearingPageCache(false);
    }
  }, [fetchPageCacheStats, showToast, t]);

  const setStorageIOPaused = useCallback(async (paused: boolean) => {
    try {
      await axios.post(`/api/system/storage-io/${paused ? 'pause' : 'resume'}`);
      showToast(t(paused ? 'settings.maintenance.storageIOPaused' : 'settings.maintenance.storageIOResumed'), 'success');
      await fetchStorageIO();
    } catch (error) {
      console.error(error);
      showToast(t('settings.maintenance.storageIOPauseFailed'), 'error');
    }
  }, [fetchStorageIO, showToast, t]);

  const handleRiskyAction = useCallback((url: string, successMessage: string, errorMessage: string, confirmMessage: string) => {
    if (!window.confirm(confirmMessage)) return;
    handleAction(url, successMessage, errorMessage);
  }, [handleAction]);

  return (
    <div className="space-y-6">
      <SettingsPageIntro title={t('settings.maintenance.title')} description={t('settings.maintenance.description')} />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-red-400">
          <AlertTriangle className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.maintenance.tasksTitle')}</h3>
        </div>
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <button onClick={() => handleAction('/api/system/rebuild-index', t('settings.maintenance.rebuildIndexSuccess'), t('settings.maintenance.rebuildIndexFailed'))} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">{t('settings.maintenance.rebuildIndex')}</p>
            <p className="mt-1 text-xs text-red-200/80">{t('settings.maintenance.rebuildIndexHint')}</p>
          </button>
          <button onClick={() => handleRiskyAction('/api/system/rebuild-thumbnails', t('settings.maintenance.rebuildThumbnailsSuccess'), t('settings.maintenance.rebuildThumbnailsFailed'), t('settings.maintenance.rebuildThumbnailsConfirm'))} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">{t('settings.maintenance.rebuildThumbnails')}</p>
            <p className="mt-1 text-xs text-red-200/80">{t('settings.maintenance.rebuildThumbnailsHint')}</p>
          </button>
          <button onClick={() => handleRiskyAction('/api/system/rebuild-file-identities', t('settings.maintenance.rebuildFileIdentitiesSuccess'), t('settings.maintenance.rebuildFileIdentitiesFailed'), t('settings.maintenance.rebuildFileIdentitiesConfirm'))} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">{t('settings.maintenance.rebuildFileIdentities')}</p>
            <p className="mt-1 text-xs text-red-200/80">{t('settings.maintenance.rebuildFileIdentitiesHint')}</p>
          </button>
          <button onClick={() => handleAction('/api/system/batch-scrape', t('settings.maintenance.batchScrapeSuccess'), t('settings.maintenance.batchScrapeFailed'))} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">{t('settings.maintenance.batchScrape')}</p>
            <p className="mt-1 text-xs text-red-200/80">{t('settings.maintenance.batchScrapeHint')}</p>
          </button>
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-komgaPrimary">
            <HardDrive className="h-5 w-5" />
            <h3 className="text-lg font-semibold text-white">{t('settings.maintenance.storageIOTitle')}</h3>
          </div>
          <button
            type="button"
            onClick={fetchStorageIO}
            disabled={loadingStorageIO}
            className="inline-flex items-center gap-2 rounded-lg border border-white/10 px-3 py-2 text-sm text-white/70 hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${loadingStorageIO ? 'animate-spin' : ''}`} />
            {t('settings.maintenance.refreshStorageIO')}
          </button>
          <button
            type="button"
            onClick={() => setStorageIOPaused(!storageIO?.paused)}
            className={`inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-sm ${storageIO?.paused ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-200 hover:bg-emerald-500/15' : 'border-amber-500/30 bg-amber-500/10 text-amber-200 hover:bg-amber-500/15'}`}
          >
            {storageIO?.paused ? t('settings.maintenance.resumeStorageIO') : t('settings.maintenance.pauseStorageIO')}
          </button>
        </div>

        <div className="grid gap-3 md:grid-cols-3 xl:grid-cols-6">
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-white/40">{t('settings.maintenance.cacheVolume')}</p>
            <p className="mt-2 truncate text-lg font-semibold text-white">{storageIO?.cache_volume || '-'}</p>
          </div>
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-white/40">{t('settings.maintenance.libraryCount')}</p>
            <p className="mt-2 text-lg font-semibold text-white">{storageIO?.libraries?.length ?? 0}</p>
          </div>
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-white/40">{t('settings.maintenance.sameDiskProtected')}</p>
            <p className="mt-2 text-lg font-semibold text-white">{storageIO?.same_disk_caches ?? 0}</p>
          </div>
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-white/40">{t('settings.maintenance.scanArchiveRate')}</p>
            <p className="mt-2 text-lg font-semibold text-white">{formatRate(storageIO?.recent_scan_archive_open_rate ?? 0)}</p>
          </div>
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-white/40">{t('settings.maintenance.coverArchiveRate')}</p>
            <p className="mt-2 text-lg font-semibold text-white">{formatRate(storageIO?.recent_cover_archive_open_rate ?? 0)}</p>
          </div>
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-white/40">{t('settings.maintenance.thumbnailWriteTime')}</p>
            <p className="mt-2 text-lg font-semibold text-white">{storageIO?.recent_thumbnail_write_ms ?? 0} ms</p>
          </div>
        </div>

        {(storageIO?.scheduler?.length ?? 0) > 0 && (
          <div className="grid gap-3 lg:grid-cols-2">
            {storageIO?.scheduler.map((item) => (
              <div key={item.volume_key} className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-sm font-semibold text-white">{item.volume_key}</p>
                  <span className={`rounded-full border px-2.5 py-1 text-xs ${item.background_paused ? 'border-amber-500/30 bg-amber-500/10 text-amber-200' : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-200'}`}>
                    {item.background_paused ? t('settings.maintenance.backgroundPaused') : t('settings.maintenance.backgroundRunning')}
                  </span>
                </div>
                <div className="mt-3 grid grid-cols-4 gap-2 text-xs">
                  <p className="rounded-lg bg-gray-950/70 px-2 py-2 text-white/60">{t('settings.maintenance.schedulerActive')}<span className="mt-1 block text-white">{item.active}</span></p>
                  <p className="rounded-lg bg-gray-950/70 px-2 py-2 text-white/60">{t('settings.maintenance.schedulerLimit')}<span className="mt-1 block text-white">{item.limit}</span></p>
                  <p className="rounded-lg bg-gray-950/70 px-2 py-2 text-white/60">{t('settings.maintenance.readerActive')}<span className="mt-1 block text-white">{item.reader_active}</span></p>
                  <p className="rounded-lg bg-gray-950/70 px-2 py-2 text-white/60">{t('settings.maintenance.readerWaiting')}<span className="mt-1 block text-white">{item.reader_waiting}</span></p>
                </div>
                <div className="mt-2 grid gap-2 sm:grid-cols-2 text-xs">
                  <p className="rounded-lg bg-gray-950/70 px-2 py-2 text-white/60">
                    {t('settings.maintenance.backgroundWaiting')}<span className="mt-1 block text-white">{item.background_waiting}</span>
                  </p>
                  <p className="rounded-lg bg-gray-950/70 px-2 py-2 text-white/60">
                    {t('settings.maintenance.pauseReason')}<span className="mt-1 block text-white">{item.pause_reason ? t(`settings.maintenance.pauseReason.${item.pause_reason}`) : '-'}</span>
                  </p>
                </div>
              </div>
            ))}
          </div>
        )}

        <div className="grid gap-3 lg:grid-cols-2">
          {(storageIO?.libraries || []).map((library) => (
            <div key={library.id} className="rounded-xl border border-white/10 bg-gray-950/50 p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-white">{library.name}</p>
                  <p className="mt-1 truncate text-xs text-white/40" title={library.path}>{library.path}</p>
                </div>
                <span className={`rounded-full border px-2.5 py-1 text-xs ${library.storage_profile === 'hdd_external' ? 'border-amber-500/30 bg-amber-500/10 text-amber-200' : 'border-gray-700 bg-gray-900 text-gray-300'}`}>
                  {t(`settings.library.storageProfile.${library.storage_profile}`)}
                </span>
              </div>
              <div className="mt-3 grid gap-2 sm:grid-cols-3">
                <p className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-xs text-white/60">
                  {t('settings.maintenance.volume')}: <span className="text-white">{library.volume_key || '-'}</span>
                </p>
                <p className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-xs text-white/60">
                  {t('settings.maintenance.heavyConcurrency')}: <span className="text-white">{library.heavy_background_concurrency || t('settings.maintenance.unlimited')}</span>
                </p>
                <p className={`rounded-lg border px-3 py-2 text-xs ${library.cache_on_same_volume && library.disable_same_disk_page_cache ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-200' : 'border-white/10 bg-white/[0.03] text-white/60'}`}>
                  {library.cache_on_same_volume
                    ? t(library.disable_same_disk_page_cache ? 'settings.maintenance.sameDiskCacheBlocked' : 'settings.maintenance.sameDiskCacheEnabled')
                    : t('settings.maintenance.cacheOnDifferentDisk')}
                </p>
              </div>
            </div>
          ))}
          {storageIO && storageIO.libraries.length === 0 && (
            <p className="rounded-xl border border-white/10 bg-white/[0.03] p-4 text-sm text-white/50">{t('settings.maintenance.noStorageIOLibraries')}</p>
          )}
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-lg font-semibold text-white">{t('settings.maintenance.cacheTitle')}</h3>
            <p className="mt-1 text-sm text-white/55">{t('settings.maintenance.cacheDescription')}</p>
          </div>
          <button
            type="button"
            onClick={fetchPageCacheStats}
            disabled={loadingPageCache}
            className="inline-flex items-center gap-2 rounded-lg border border-white/10 px-3 py-2 text-sm text-white/70 hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${loadingPageCache ? 'animate-spin' : ''}`} />
            {t('settings.maintenance.refreshCacheStats')}
          </button>
        </div>
        <div className="grid gap-3 md:grid-cols-[1fr_1fr_auto]">
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-white/40">{t('settings.maintenance.pageCacheSize')}</p>
            <p className="mt-2 text-2xl font-semibold text-white">{formatBytes(pageCacheStats?.file_size ?? 0)}</p>
          </div>
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-white/40">{t('settings.maintenance.pageCacheFiles')}</p>
            <p className="mt-2 text-2xl font-semibold text-white">{pageCacheStats?.file_count ?? 0}</p>
          </div>
          <button
            type="button"
            onClick={handleClearPageCache}
            disabled={clearingPageCache || (pageCacheStats?.file_count ?? 0) === 0}
            className="inline-flex min-h-[82px] items-center justify-center gap-2 rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm font-medium text-red-200 hover:bg-red-500/15 disabled:cursor-not-allowed disabled:opacity-50 md:min-w-44"
          >
            <Trash2 className="h-4 w-4" />
            {clearingPageCache ? t('settings.maintenance.clearingPageCache') : t('settings.maintenance.clearPageCache')}
          </button>
        </div>
        <p className="truncate text-xs text-white/35" title={pageCacheStats?.path || undefined}>
          {pageCacheStats?.path || t('settings.maintenance.pageCachePathPending')}
        </p>
      </section>
    </div>
  );
}
