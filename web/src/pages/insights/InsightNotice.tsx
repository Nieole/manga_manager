import { useI18n } from '../../i18n/LocaleProvider';

export function InsightNotice({ panel, label }: {
  panel: { data?: unknown; failed: boolean; pending: boolean; enabled: boolean; retry: () => void };
  label: string;
}) {
  const { t } = useI18n();
  if (!panel.enabled || (!panel.failed && !panel.pending)) return null;
  return <div role="status" className="rounded-xl border border-gray-800 bg-komgaSurface p-4 text-sm text-gray-400 flex flex-wrap items-center justify-between gap-3">
    <span>{t(panel.data !== undefined ? 'insights.stale' : panel.failed ? 'insights.unavailable' : 'insights.loading', { panel: label })}</span>
    {panel.failed && <button type="button" onClick={panel.retry} aria-label={t('insights.retryPanel', { panel: label })} className="rounded-lg border border-gray-700 px-3 py-1.5 text-gray-200 hover:bg-gray-800 focus-visible:outline-2 focus-visible:outline-komgaPrimary">{t('insights.retry')}</button>}
  </div>;
}
