import { useEffect, useMemo, useState } from 'react';
import axios from 'axios';
import { CheckCircle2, Copy, ExternalLink, Link2, RefreshCw, Server, TabletSmartphone } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { SettingsPageIntro, sectionClassName } from './shared';

interface ClientConnectionEndpoint {
  key: string;
  label: string;
  url: string;
  path: string;
  description: string;
  enabled: boolean;
}

interface ClientConnectionsResponse {
  base_url: string;
  endpoints: ClientConnectionEndpoint[];
  status: {
    koreader_enabled: boolean;
    koreader_account_count: number;
    koreader_enabled_accounts: number;
    koreader_match_mode: string;
  };
}

export function SettingsConnectionsPage() {
  const { t } = useI18n();
  const { showToast } = useSettings();
  const [loading, setLoading] = useState(true);
  const [copying, setCopying] = useState<string | null>(null);
  const [data, setData] = useState<ClientConnectionsResponse | null>(null);

  const loadConnections = async () => {
    setLoading(true);
    try {
      const res = await axios.get<ClientConnectionsResponse>('/api/system/client-connections');
      setData(res.data);
    } catch (error) {
      console.error(error);
      showToast(t('settings.connections.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadConnections();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const metrics = useMemo(() => [
    { label: t('settings.connections.metric.baseUrl'), value: data?.base_url || t('common.none') },
    { label: t('settings.connections.metric.endpoints'), value: data?.endpoints.length ?? 0 },
    { label: t('settings.connections.metric.koreader'), value: data?.status.koreader_enabled ? t('settings.connections.enabled') : t('settings.connections.disabled') },
  ], [data, t]);

  const handleCopy = async (endpoint: ClientConnectionEndpoint) => {
    setCopying(endpoint.key);
    try {
      await navigator.clipboard.writeText(endpoint.url);
      showToast(t('settings.connections.copied', { label: endpoint.label }), 'success');
    } catch (error) {
      console.error(error);
      showToast(t('settings.connections.copyFailed'), 'error');
    } finally {
      setCopying(null);
    }
  };

  return (
    <div className="space-y-6">
      <SettingsPageIntro
        title={t('settings.connections.title')}
        description={t('settings.connections.description')}
        badge={
          <div className="inline-flex items-center gap-2 rounded-full border border-sky-500/20 bg-sky-500/10 px-3 py-1.5 text-sm text-sky-200">
            <Link2 className="h-4 w-4" />
            {t('settings.connections.badge')}
          </div>
        }
      />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <Server className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.connections.summaryTitle')}</h3>
        </div>

        <div className="grid gap-4 md:grid-cols-3">
          {metrics.map((metric) => (
            <div key={metric.label} className="rounded-xl border border-gray-800 bg-gray-900/50 p-4">
              <p className="text-xs uppercase tracking-wide text-gray-500">{metric.label}</p>
              <p className="mt-2 break-all text-base font-semibold text-white">{String(metric.value)}</p>
            </div>
          ))}
        </div>

        {data?.status.koreader_enabled && (
          <div className="rounded-xl border border-emerald-500/20 bg-emerald-500/10 p-4 text-sm text-emerald-100">
            <div className="flex items-center gap-2 font-medium">
              <CheckCircle2 className="h-4 w-4" />
              {t('settings.connections.koreaderEnabled')}
            </div>
            <p className="mt-1 text-emerald-100/80">
              {t('settings.connections.koreaderStatus', {
                enabled: data.status.koreader_enabled_accounts,
                total: data.status.koreader_account_count,
                mode: data.status.koreader_match_mode,
              })}
            </p>
          </div>
        )}
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-komgaPrimary">
            <TabletSmartphone className="h-5 w-5" />
            <h3 className="text-lg font-semibold text-white">{t('settings.connections.endpointsTitle')}</h3>
          </div>
          <button onClick={loadConnections} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30">
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
            {t('common.refresh')}
          </button>
        </div>

        {loading ? (
          <div className="flex h-40 items-center justify-center text-gray-500">
            <RefreshCw className="h-6 w-6 animate-spin" />
          </div>
        ) : (
          <div className="grid gap-4 xl:grid-cols-2">
            {(data?.endpoints || []).map((endpoint) => (
              <div key={endpoint.key} className="rounded-xl border border-gray-800 bg-black/20 p-4">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="text-base font-semibold text-white">{endpoint.label}</p>
                      <span className={`rounded-full border px-2 py-0.5 text-[11px] ${endpoint.enabled ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-200' : 'border-gray-700 bg-gray-900 text-gray-400'}`}>
                        {endpoint.enabled ? t('settings.connections.enabled') : t('settings.connections.disabled')}
                      </span>
                    </div>
                    <p className="mt-1 text-sm text-gray-400">{endpoint.description}</p>
                  </div>
                  <div className="rounded-lg border border-gray-800 bg-gray-950 px-3 py-2 text-xs text-gray-400">
                    {endpoint.path}
                  </div>
                </div>

                <div className="mt-4 rounded-lg border border-gray-800 bg-gray-950 px-3 py-2">
                  <p className="break-all font-mono text-sm text-komgaPrimary">{endpoint.url}</p>
                </div>

                <div className="mt-3 flex flex-wrap gap-2">
                  <button
                    onClick={() => handleCopy(endpoint)}
                    className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30 disabled:opacity-60"
                    disabled={copying === endpoint.key}
                  >
                    {copying === endpoint.key ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Copy className="h-3.5 w-3.5" />}
                    {t('settings.connections.copy')}
                  </button>
                  <a
                    href={endpoint.url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30"
                  >
                    <ExternalLink className="h-3.5 w-3.5" />
                    {t('settings.connections.open')}
                  </a>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
