import { AlertTriangle, CheckCircle2, FolderOpen, HardDrive, Palette, Settings as SettingsIcon, Sparkles, TabletSmartphone, Wrench } from 'lucide-react';
import { useOutletContext } from 'react-router-dom';
import { useTheme } from '../../theme/ThemeProvider';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { SettingsPageIntro } from './shared';

export function SettingsOverviewPage() {
  const { t } = useI18n();
  const { validation, config, koreaderStatus, capabilities } = useSettings();
  const { resolvedTheme } = useTheme();
  const { navigateSettingsSection } = useOutletContext<{ navigateSettingsSection: (path: string) => void }>();

  const cards = [
    {
      title: t('settings.nav.appearance'),
      description: t('settings.overview.themeCard', { theme: t(resolvedTheme.nameKey) }),
      icon: <Palette className="h-5 w-5 text-komgaPrimary" />,
      action: () => navigateSettingsSection('/settings/appearance'),
    },
    {
      title: t('settings.nav.library'),
      description: t('settings.overview.libraryCard', { count: config?.library.paths?.length ?? 0, formats: capabilities?.default_scan_formats ?? t('common.loading') }),
      icon: <FolderOpen className="h-5 w-5 text-komgaPrimary" />,
      action: () => navigateSettingsSection('/settings/library'),
    },
    {
      title: t('settings.nav.media'),
      description: t('settings.overview.mediaCard', { path: config?.cache.dir || t('settings.overview.notConfigured') }),
      icon: <HardDrive className="h-5 w-5 text-komgaPrimary" />,
      action: () => navigateSettingsSection('/settings/media'),
    },
    {
      title: t('settings.nav.ai'),
      description: `${config?.llm.provider || t('settings.overview.notConfigured')} · ${config?.llm.model || t('settings.overview.modelUnset')}`,
      icon: <Sparkles className="h-5 w-5 text-komgaPrimary" />,
      action: () => navigateSettingsSection('/settings/ai'),
    },
    {
      title: 'KOReader',
      description: koreaderStatus?.enabled ? t('settings.overview.koreaderEnabled', { enabled: koreaderStatus.enabled_account_count, total: koreaderStatus.account_count }) : t('settings.overview.koreaderDisabled'),
      icon: <TabletSmartphone className="h-5 w-5 text-sky-400" />,
      action: () => navigateSettingsSection('/settings/koreader'),
    },
    {
      title: t('settings.nav.maintenance'),
      description: t('settings.overview.maintenanceCard'),
      icon: <Wrench className="h-5 w-5 text-red-400" />,
      action: () => navigateSettingsSection('/settings/maintenance'),
    },
  ];

  return (
    <div className="space-y-6">
      <SettingsPageIntro
        title={t('settings.overview.title')}
        description={t('settings.overview.description')}
        badge={
          <div
            className={`inline-flex items-center gap-2 rounded-full px-3 py-1.5 text-sm ${
              validation.valid ? 'border border-emerald-500/20 bg-emerald-500/10 text-emerald-300' : 'border border-amber-500/20 bg-amber-500/10 text-amber-300'
            }`}
          >
            {validation.valid ? <CheckCircle2 className="h-4 w-4" /> : <AlertTriangle className="h-4 w-4" />}
            {validation.valid ? t('settings.overview.healthy') : t('settings.overview.needFix', { count: validation.issues.length })}
          </div>
        }
      />

      {!validation.valid && (
        <div className="rounded-2xl border border-amber-500/20 bg-amber-500/10 p-4">
          <p className="text-sm font-medium text-amber-100">{t('settings.overview.blockingIssues')}</p>
          <div className="mt-2 space-y-1">
            {validation.issues.slice(0, 5).map((issue) => (
              <p key={`${issue.field}-${issue.message}`} className="text-sm text-amber-200/90">
                {issue.field}: {issue.message}
              </p>
            ))}
          </div>
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {cards.map((card) => (
          <button
            key={card.title}
            type="button"
            onClick={card.action}
            className="rounded-2xl border border-gray-800 bg-komgaSurface/80 p-5 text-left transition-all hover:border-gray-700 hover:bg-gray-900/70"
          >
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-2xl border border-white/10 bg-white/[0.03]">
                {card.icon}
              </div>
              <div>
                <p className="text-base font-semibold text-white">{card.title}</p>
                <p className="mt-1 text-sm leading-6 text-gray-400">{card.description}</p>
              </div>
            </div>
          </button>
        ))}
      </div>

      <div className="rounded-2xl border border-gray-800 bg-komgaSurface p-6">
        <div className="flex items-center gap-2 text-komgaPrimary">
          <SettingsIcon className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.overview.globalStatus')}</h3>
        </div>
        <div className="mt-4 grid gap-4 md:grid-cols-3">
          <div className="rounded-xl border border-gray-800 bg-gray-900/40 p-4">
            <p className="text-sm text-gray-400">{t('settings.overview.boundDirs')}</p>
            <p className="mt-2 text-2xl font-bold text-white">{config?.library.paths?.length ?? 0}</p>
          </div>
          <div className="rounded-xl border border-gray-800 bg-gray-900/40 p-4">
            <p className="text-sm text-gray-400">{t('settings.overview.unmatchedRecords')}</p>
            <p className="mt-2 text-2xl font-bold text-white">{koreaderStatus?.stats.unmatched_progress_count ?? 0}</p>
          </div>
          <div className="rounded-xl border border-gray-800 bg-gray-900/40 p-4">
            <p className="text-sm text-gray-400">{t('settings.overview.currentTheme')}</p>
            <p className="mt-2 text-2xl font-bold text-white">{t(resolvedTheme.nameKey)}</p>
          </div>
        </div>
      </div>
    </div>
  );
}
