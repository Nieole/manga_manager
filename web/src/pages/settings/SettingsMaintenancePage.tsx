import { useCallback, useEffect, useState } from 'react';
import axios from 'axios';
import { AlertTriangle, RefreshCw, Trash2 } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { SettingsPageIntro, sectionClassName } from './shared';

interface PageCacheStats {
  path: string;
  file_size: number;
  file_count: number;
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

export function SettingsMaintenancePage() {
  const { t } = useI18n();
  const { handleAction, showToast } = useSettings();
  const [pageCacheStats, setPageCacheStats] = useState<PageCacheStats | null>(null);
  const [loadingPageCache, setLoadingPageCache] = useState(false);
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

  useEffect(() => {
    fetchPageCacheStats();
  }, [fetchPageCacheStats]);

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
          <button onClick={() => handleAction('/api/system/rebuild-thumbnails', t('settings.maintenance.rebuildThumbnailsSuccess'), t('settings.maintenance.rebuildThumbnailsFailed'))} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">{t('settings.maintenance.rebuildThumbnails')}</p>
            <p className="mt-1 text-xs text-red-200/80">{t('settings.maintenance.rebuildThumbnailsHint')}</p>
          </button>
          <button onClick={() => handleAction('/api/system/rebuild-file-identities', t('settings.maintenance.rebuildFileIdentitiesSuccess'), t('settings.maintenance.rebuildFileIdentitiesFailed'))} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
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
