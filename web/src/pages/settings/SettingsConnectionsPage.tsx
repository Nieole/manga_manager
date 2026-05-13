import { useEffect, useMemo, useState } from 'react';
import axios from 'axios';
import { Activity, AlertTriangle, CheckCircle2, Clock3, Copy, ExternalLink, Layers3, Link2, QrCode, RefreshCw, Server, TabletSmartphone, Wifi, XCircle } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { SettingsPageIntro, sectionClassName } from './shared';

interface ClientEndpointRequestSnapshot {
  time: string;
  method: string;
  path: string;
  status: number;
  duration_ms: number;
  remote_ip: string;
}

interface ClientEndpointRequestDiagnostics {
  total: number;
  success: number;
  warnings: number;
  errors: number;
  slow: number;
  last_seen?: string;
  last_status: number;
  last_duration_ms: number;
  last_path: string;
  recent: ClientEndpointRequestSnapshot[];
}

interface ClientConnectionEndpoint {
  key: string;
  category: 'catalog' | 'collections' | 'sync' | string;
  client_type: 'opds' | 'mihon' | 'koreader' | string;
  label: string;
  url: string;
  path: string;
  description: string;
  enabled: boolean;
  health: 'ready' | 'needs_account' | 'disabled' | string;
  auth_note: string;
  diagnostics: string[];
  requests: ClientEndpointRequestDiagnostics;
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
    { label: t('settings.connections.metric.endpoints'), value: data?.endpoints.filter((endpoint) => endpoint.enabled).length ?? 0 },
    { label: t('settings.connections.metric.koreader'), value: data?.status.koreader_enabled ? t('settings.connections.enabled') : t('settings.connections.disabled') },
  ], [data, t]);

  const healthCounts = useMemo(() => {
    const endpoints = data?.endpoints || [];
    return {
      ready: endpoints.filter((endpoint) => endpoint.health === 'ready').length,
      attention: endpoints.filter((endpoint) => endpoint.health !== 'ready').length,
    };
  }, [data]);

  const endpointGroups = useMemo(() => {
    const endpoints = data?.endpoints || [];
    return [
      {
        key: 'catalog',
        title: t('settings.connections.group.catalog'),
        description: t('settings.connections.group.catalogDescription'),
        icon: TabletSmartphone,
        items: endpoints.filter((endpoint) => endpoint.category === 'catalog'),
      },
      {
        key: 'collections',
        title: t('settings.connections.group.collections'),
        description: t('settings.connections.group.collectionsDescription'),
        icon: Layers3,
        items: endpoints.filter((endpoint) => endpoint.category === 'collections'),
      },
      {
        key: 'sync',
        title: t('settings.connections.group.sync'),
        description: t('settings.connections.group.syncDescription'),
        icon: Wifi,
        items: endpoints.filter((endpoint) => endpoint.category === 'sync'),
      },
    ].filter((group) => group.items.length > 0);
  }, [data, t]);

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

  const copyBaseURL = async () => {
    if (!data?.base_url) return;
    try {
      await navigator.clipboard.writeText(data.base_url);
      showToast(t('settings.connections.baseCopied'), 'success');
    } catch (error) {
      console.error(error);
      showToast(t('settings.connections.copyFailed'), 'error');
    }
  };

  const healthMeta = (health: string) => {
    if (health === 'ready') {
      return {
        label: t('settings.connections.health.ready'),
        className: 'border-emerald-500/20 bg-emerald-500/10 text-emerald-200',
        icon: CheckCircle2,
      };
    }
    if (health === 'needs_account') {
      return {
        label: t('settings.connections.health.needsAccount'),
        className: 'border-amber-500/25 bg-amber-500/10 text-amber-200',
        icon: AlertTriangle,
      };
    }
    return {
      label: t('settings.connections.health.disabled'),
      className: 'border-gray-700 bg-gray-900 text-gray-400',
      icon: XCircle,
    };
  };

  const requestStatusClass = (status: number) => {
    if (status >= 500) return 'border-red-500/25 bg-red-500/10 text-red-200';
    if (status >= 400) return 'border-amber-500/25 bg-amber-500/10 text-amber-200';
    return 'border-emerald-500/20 bg-emerald-500/10 text-emerald-200';
  };

  const formatRequestTime = (value?: string) => {
    if (!value) return t('settings.connections.requests.never');
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return t('settings.connections.requests.never');
    return date.toLocaleString();
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

        <div className="grid gap-3 lg:grid-cols-[1.2fr_0.8fr]">
          <div className="rounded-xl border border-gray-800 bg-black/20 p-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div>
                <p className="text-sm font-semibold text-white">{t('settings.connections.baseUrlTitle')}</p>
                <p className="mt-1 break-all font-mono text-sm text-komgaPrimary">{data?.base_url || t('common.none')}</p>
              </div>
              <button onClick={copyBaseURL} disabled={!data?.base_url} className="inline-flex w-fit items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30 disabled:opacity-60">
                <Copy className="h-3.5 w-3.5" />
                {t('settings.connections.copyBase')}
              </button>
            </div>
            <p className="mt-3 text-xs leading-5 text-gray-500">{t('settings.connections.baseUrlHint')}</p>
          </div>

          <div className="rounded-xl border border-gray-800 bg-black/20 p-4">
            <p className="text-sm font-semibold text-white">{t('settings.connections.healthTitle')}</p>
            <div className="mt-3 grid grid-cols-2 gap-3">
              <div className="rounded-lg border border-emerald-500/15 bg-emerald-500/10 p-3">
                <p className="text-xs text-emerald-200/80">{t('settings.connections.health.ready')}</p>
                <p className="mt-1 text-2xl font-semibold text-white">{healthCounts.ready}</p>
              </div>
              <div className="rounded-lg border border-amber-500/15 bg-amber-500/10 p-3">
                <p className="text-xs text-amber-200/80">{t('settings.connections.health.attention')}</p>
                <p className="mt-1 text-2xl font-semibold text-white">{healthCounts.attention}</p>
              </div>
            </div>
          </div>
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
          <div className="space-y-5">
            {endpointGroups.map((group) => {
              const Icon = group.icon;
              return (
                <div key={group.key} className="rounded-xl border border-gray-800 bg-black/20 p-4">
                  <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                    <div className="flex items-start gap-3">
                      <div className="rounded-lg border border-gray-800 bg-gray-950 p-2 text-komgaPrimary">
                        <Icon className="h-4 w-4" />
                      </div>
                      <div>
                        <h4 className="text-sm font-semibold text-white">{group.title}</h4>
                        <p className="mt-1 text-sm text-gray-400">{group.description}</p>
                      </div>
                    </div>
                    <span className="w-fit rounded-full border border-gray-700 bg-gray-950 px-2.5 py-1 text-xs text-gray-400">
                      {t('settings.connections.group.count', { count: group.items.length })}
                    </span>
                  </div>

                  <div className="mt-4 grid gap-3 xl:grid-cols-2">
                    {group.items.map((endpoint) => (
                      <div key={endpoint.key} className="rounded-lg border border-gray-800 bg-gray-950/60 p-4">
                        <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_132px]">
                          <div className="min-w-0">
                            <div className="flex flex-wrap items-center gap-2">
                              <p className="text-base font-semibold text-white">{endpoint.label}</p>
                              <span className="rounded-full border border-gray-700 bg-black/20 px-2 py-0.5 text-[11px] uppercase text-gray-400">
                                {endpoint.client_type}
                              </span>
                              {(() => {
                                const meta = healthMeta(endpoint.health);
                                const HealthIcon = meta.icon;
                                return (
                                  <span className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] ${meta.className}`}>
                                    <HealthIcon className="h-3 w-3" />
                                    {meta.label}
                                  </span>
                                );
                              })()}
                              <span className={`rounded-full border px-2 py-0.5 text-[11px] ${endpoint.enabled ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-200' : 'border-gray-700 bg-gray-900 text-gray-400'}`}>
                                {endpoint.enabled ? t('settings.connections.enabled') : t('settings.connections.disabled')}
                              </span>
                            </div>
                            <p className="mt-1 text-sm text-gray-400">{endpoint.description}</p>
                            <p className="mt-2 text-xs leading-5 text-gray-500">{endpoint.auth_note}</p>
                            <div className="mt-3 rounded-lg border border-gray-800 bg-black/30 px-3 py-2">
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

                          <div className="space-y-3">
                            <EndpointQRCode value={endpoint.url} label={endpoint.label} />
                            <div className="break-all rounded-lg border border-gray-800 bg-black/30 px-3 py-2 text-xs text-gray-400">
                              {endpoint.path}
                            </div>
                          </div>
                        </div>

                        {endpoint.diagnostics.length > 0 && (
                          <div className="mt-4 grid gap-2 sm:grid-cols-2">
                            {endpoint.diagnostics.map((item) => (
                              <div key={item} className="rounded-lg border border-gray-800 bg-black/20 px-3 py-2 text-xs leading-5 text-gray-400">
                                {item}
                              </div>
                            ))}
                          </div>
                        )}

                        <div className="mt-4 rounded-lg border border-gray-800 bg-black/25 p-3">
                          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                            <div className="flex items-center gap-2 text-gray-200">
                              <Activity className="h-4 w-4 text-komgaPrimary" />
                              <span className="text-sm font-semibold">{t('settings.connections.requests.title')}</span>
                            </div>
                            <div className="flex flex-wrap gap-2 text-[11px]">
                              <span className="rounded-full border border-gray-700 bg-gray-950 px-2 py-0.5 text-gray-400">
                                {t('settings.connections.requests.total', { count: endpoint.requests?.total ?? 0 })}
                              </span>
                              <span className="rounded-full border border-red-500/20 bg-red-500/10 px-2 py-0.5 text-red-200">
                                {t('settings.connections.requests.errors', { count: endpoint.requests?.errors ?? 0 })}
                              </span>
                              <span className="rounded-full border border-amber-500/20 bg-amber-500/10 px-2 py-0.5 text-amber-200">
                                {t('settings.connections.requests.slow', { count: endpoint.requests?.slow ?? 0 })}
                              </span>
                            </div>
                          </div>

                          <div className="mt-3 grid gap-2 md:grid-cols-[0.9fr_1.1fr]">
                            <div className="rounded-lg border border-gray-800 bg-gray-950/70 p-3">
                              <div className="flex items-center gap-2 text-xs text-gray-500">
                                <Clock3 className="h-3.5 w-3.5" />
                                {t('settings.connections.requests.lastSeen')}
                              </div>
                              <p className="mt-1 text-sm text-white">{formatRequestTime(endpoint.requests?.last_seen)}</p>
                              {endpoint.requests?.last_seen && (
                                <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px]">
                                  <span className={`rounded-full border px-2 py-0.5 ${requestStatusClass(endpoint.requests.last_status)}`}>
                                    HTTP {endpoint.requests.last_status}
                                  </span>
                                  <span className="rounded-full border border-gray-700 bg-black/20 px-2 py-0.5 text-gray-400">
                                    {endpoint.requests.last_duration_ms}ms
                                  </span>
                                </div>
                              )}
                              {endpoint.requests?.last_path && (
                                <p className="mt-2 break-all font-mono text-[11px] text-gray-500">{endpoint.requests.last_path}</p>
                              )}
                            </div>

                            <div className="space-y-2">
                              {(endpoint.requests?.recent?.length ?? 0) === 0 ? (
                                <div className="flex h-full min-h-24 items-center rounded-lg border border-dashed border-gray-800 bg-gray-950/40 px-3 text-xs text-gray-500">
                                  {t('settings.connections.requests.empty')}
                                </div>
                              ) : (
                                endpoint.requests.recent.map((item) => (
                                  <div key={`${item.time}-${item.method}-${item.path}-${item.status}`} className="grid gap-2 rounded-lg border border-gray-800 bg-gray-950/70 px-3 py-2 text-xs sm:grid-cols-[auto_auto_minmax(0,1fr)_auto] sm:items-center">
                                    <span className="font-semibold text-gray-300">{item.method}</span>
                                    <span className={`w-fit rounded-full border px-2 py-0.5 ${requestStatusClass(item.status)}`}>{item.status}</span>
                                    <span className="break-all font-mono text-gray-500">{item.path}</span>
                                    <span className="text-right text-gray-500">{item.duration_ms}ms</span>
                                  </div>
                                ))
                              )}
                            </div>
                          </div>
                        </div>
                      </div>
                    ))}
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

function EndpointQRCode({ value, label }: { value: string; label: string }) {
  const { t } = useI18n();
  const qr = useMemo(() => createQRCode(value), [value]);

  if (!qr) {
    return (
      <div className="flex h-36 w-32 flex-col items-center justify-center rounded-lg border border-gray-800 bg-black/20 px-3 text-center text-[11px] leading-4 text-gray-500">
        <QrCode className="mb-2 h-4 w-4" />
        {t('settings.connections.qrTooLong')}
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-gray-800 bg-white p-2" aria-label={`${label} QR code`}>
      <svg viewBox={`0 0 ${qr.size} ${qr.size}`} className="h-28 w-28" role="img">
        <title>{label}</title>
        <rect width={qr.size} height={qr.size} fill="#ffffff" />
        {qr.cells.map(([x, y]) => (
          <rect key={`${x}-${y}`} x={x} y={y} width="1" height="1" fill="#111827" />
        ))}
      </svg>
      <div className="mt-2 flex items-center justify-center gap-1 text-[10px] font-semibold uppercase tracking-wide text-gray-700">
        <QrCode className="h-3 w-3" />
        QR
      </div>
    </div>
  );
}

function createQRCode(value: string): { size: number; cells: Array<[number, number]> } | null {
  const version = 6;
  const size = 17 + version * 4;
  const dataCodewords = 136;
  const eccCodewordsPerBlock = 18;
  const bytes = new TextEncoder().encode(value);
  const maxBytes = Math.floor((dataCodewords * 8 - 12) / 8);
  if (bytes.length > maxBytes) {
    return null;
  }
  const modules: boolean[][] = Array.from({ length: size }, () => Array(size).fill(false));
  const reserved: boolean[][] = Array.from({ length: size }, () => Array(size).fill(false));
  const setFunction = (x: number, y: number, dark: boolean) => {
    if (x < 0 || y < 0 || x >= size || y >= size) return;
    modules[y][x] = dark;
    reserved[y][x] = true;
  };
  const finder = (offsetX: number, offsetY: number) => {
    for (let y = -1; y <= 7; y += 1) {
      for (let x = -1; x <= 7; x += 1) {
        const xx = offsetX + x;
        const yy = offsetY + y;
        if (xx < 0 || yy < 0 || xx >= size || yy >= size) continue;
        const dark = x >= 0 && x <= 6 && y >= 0 && y <= 6 && (x === 0 || x === 6 || y === 0 || y === 6 || (x >= 2 && x <= 4 && y >= 2 && y <= 4));
        setFunction(xx, yy, dark);
      }
    }
  };

  finder(0, 0);
  finder(size - 7, 0);
  finder(0, size - 7);
  for (let i = 8; i < size - 8; i += 1) {
    setFunction(i, 6, i % 2 === 0);
    setFunction(6, i, i % 2 === 0);
  }
  drawAlignment(modules, reserved, 34, 34);
  setFunction(8, 4 * version + 9, true);
  drawFormatBits(modules, reserved, size);

  const data = encodeQRData(bytes, dataCodewords);
  const blocks = [data.slice(0, 68), data.slice(68, 136)];
  const eccBlocks = blocks.map((block) => reedSolomonRemainder(block, eccCodewordsPerBlock));
  const codewords: number[] = [];
  for (let i = 0; i < 68; i += 1) {
    codewords.push(blocks[0][i], blocks[1][i]);
  }
  for (let i = 0; i < eccCodewordsPerBlock; i += 1) {
    codewords.push(eccBlocks[0][i], eccBlocks[1][i]);
  }

  const bits = codewords.flatMap((codeword) => Array.from({ length: 8 }, (_, index) => ((codeword >>> (7 - index)) & 1) === 1));
  let bitIndex = 0;
  let upward = true;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right -= 1;
    for (let vertical = 0; vertical < size; vertical += 1) {
      const y = upward ? size - 1 - vertical : vertical;
      for (let dx = 0; dx < 2; dx += 1) {
        const x = right - dx;
        if (reserved[y][x]) continue;
        const mask = (x + y) % 2 === 0;
        modules[y][x] = (bits[bitIndex] ?? false) !== mask;
        bitIndex += 1;
      }
    }
    upward = !upward;
  }

  const cells: Array<[number, number]> = [];
  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      if (modules[y][x]) cells.push([x, y]);
    }
  }
  return { size, cells };
}

function drawAlignment(modules: boolean[][], reserved: boolean[][], centerX: number, centerY: number) {
  for (let y = -2; y <= 2; y += 1) {
    for (let x = -2; x <= 2; x += 1) {
      const xx = centerX + x;
      const yy = centerY + y;
      modules[yy][xx] = Math.max(Math.abs(x), Math.abs(y)) !== 1;
      reserved[yy][xx] = true;
    }
  }
}

function drawFormatBits(modules: boolean[][], reserved: boolean[][], size: number) {
  const bits = 0x77c4;
  const getBit = (index: number) => ((bits >>> index) & 1) === 1;
  const set = (x: number, y: number, dark: boolean) => {
    modules[y][x] = dark;
    reserved[y][x] = true;
  };
  for (let i = 0; i <= 5; i += 1) set(8, i, getBit(i));
  set(8, 7, getBit(6));
  set(8, 8, getBit(7));
  set(7, 8, getBit(8));
  for (let i = 9; i < 15; i += 1) set(14 - i, 8, getBit(i));
  for (let i = 0; i < 8; i += 1) set(size - 1 - i, 8, getBit(i));
  for (let i = 8; i < 15; i += 1) set(8, size - 15 + i, getBit(i));
}

function encodeQRData(bytes: Uint8Array, capacity: number): number[] {
  const bits: boolean[] = [];
  const append = (value: number, length: number) => {
    for (let i = length - 1; i >= 0; i -= 1) bits.push(((value >>> i) & 1) === 1);
  };

  append(0b0100, 4);
  append(bytes.length, 8);
  bytes.forEach((byte) => append(byte, 8));
  const terminator = Math.min(4, capacity * 8 - bits.length);
  append(0, terminator);
  while (bits.length % 8 !== 0) bits.push(false);

  const result: number[] = [];
  for (let i = 0; i < bits.length; i += 8) {
    let codeword = 0;
    for (let j = 0; j < 8; j += 1) codeword = (codeword << 1) | (bits[i + j] ? 1 : 0);
    result.push(codeword);
  }
  for (let pad = 0; result.length < capacity; pad += 1) {
    result.push(pad % 2 === 0 ? 0xec : 0x11);
  }
  return result;
}

function reedSolomonRemainder(data: number[], degree: number): number[] {
  const generator = reedSolomonGenerator(degree);
  const result = Array(degree).fill(0);
  data.forEach((byte) => {
    const factor = byte ^ result.shift()!;
    result.push(0);
    generator.forEach((coefficient, index) => {
      result[index] ^= gfMultiply(coefficient, factor);
    });
  });
  return result;
}

function reedSolomonGenerator(degree: number): number[] {
  let result = [1];
  for (let i = 0; i < degree; i += 1) {
    const next = Array(result.length + 1).fill(0);
    result.forEach((coefficient, index) => {
      next[index] ^= gfMultiply(coefficient, 1);
      next[index + 1] ^= gfMultiply(coefficient, gfPow(2, i));
    });
    result = next;
  }
  return result.slice(1);
}

function gfMultiply(x: number, y: number): number {
  let result = 0;
  for (let i = 0; i < 8; i += 1) {
    if ((y & 1) !== 0) result ^= x;
    const carry = (x & 0x80) !== 0;
    x = (x << 1) & 0xff;
    if (carry) x ^= 0x1d;
    y >>>= 1;
  }
  return result;
}

function gfPow(x: number, power: number): number {
  let result = 1;
  for (let i = 0; i < power; i += 1) result = gfMultiply(result, x);
  return result;
}
