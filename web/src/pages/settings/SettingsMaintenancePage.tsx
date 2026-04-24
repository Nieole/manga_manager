import { AlertTriangle } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { SettingsPageIntro, sectionClassName } from './shared';

export function SettingsMaintenancePage() {
  const { t } = useI18n();
  const { handleAction } = useSettings();

  return (
    <div className="space-y-6">
      <SettingsPageIntro title={t('settings.maintenance.title')} description={t('settings.maintenance.description')} />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-red-400">
          <AlertTriangle className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.maintenance.tasksTitle')}</h3>
        </div>
        <div className="grid gap-3 md:grid-cols-3">
          <button onClick={() => handleAction('/api/system/rebuild-index', t('settings.maintenance.rebuildIndexSuccess'), t('settings.maintenance.rebuildIndexFailed'))} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">{t('settings.maintenance.rebuildIndex')}</p>
            <p className="mt-1 text-xs text-red-200/80">{t('settings.maintenance.rebuildIndexHint')}</p>
          </button>
          <button onClick={() => handleAction('/api/system/rebuild-thumbnails', t('settings.maintenance.rebuildThumbnailsSuccess'), t('settings.maintenance.rebuildThumbnailsFailed'))} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">{t('settings.maintenance.rebuildThumbnails')}</p>
            <p className="mt-1 text-xs text-red-200/80">{t('settings.maintenance.rebuildThumbnailsHint')}</p>
          </button>
          <button onClick={() => handleAction('/api/system/batch-scrape', t('settings.maintenance.batchScrapeSuccess'), t('settings.maintenance.batchScrapeFailed'))} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">{t('settings.maintenance.batchScrape')}</p>
            <p className="mt-1 text-xs text-red-200/80">{t('settings.maintenance.batchScrapeHint')}</p>
          </button>
        </div>
      </section>
    </div>
  );
}
