import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, Copy, Info, RefreshCw, Search, Terminal } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';

interface LogEntry {
  time: string;
  level: string;
  msg: string;
  raw: string;
}

interface LogsResponse {
  items: LogEntry[];
  summary: {
    total: number;
    by_level: Record<string, number>;
  };
}

function logLevelBadgeClass(level: string) {
  switch (level) {
    case 'ERROR':
      return 'border-red-500/30 bg-red-500/10 text-red-500';
    case 'WARN':
      return 'border-amber-500/30 bg-amber-500/10 text-amber-600';
    case 'DEBUG':
      return 'border-violet-500/30 bg-violet-500/10 text-violet-500';
    default:
      return 'border-blue-500/30 bg-blue-500/10 text-blue-500';
  }
}

interface LogsPerformanceSummary {
  average_ms: number;
  p95_ms: number;
  page_image_requests: number;
  page_image_cache_hits: number;
  page_image_io_wait_ms: number;
  page_image_archive_opens: number;
  total_bytes: number;
}

interface LogsProps {
  embedded?: boolean;
  taskKey?: string;
  onClearTaskKey?: () => void;
}

export default function Logs({ embedded = false, taskKey, onClearTaskKey }: LogsProps = {}) {
  const { t, formatDateTime } = useI18n();
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [summary, setSummary] = useState<LogsResponse['summary']>({ total: 0, by_level: { DEBUG: 0, ERROR: 0, WARN: 0, INFO: 0 } });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filterLevel, setFilterLevel] = useState('ALL');
  const [query, setQuery] = useState('');
  const [copiedIndex, setCopiedIndex] = useState<number | null>(null);
  const [performance, setPerformance] = useState<LogsPerformanceSummary | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({ limit: '300', level: filterLevel });
      if (query) params.set('q', query);
      if (taskKey) params.set('task_key', taskKey);
      const [logsResp, perfResp] = await Promise.all([
        fetch(`/api/system/logs?${params.toString()}`),
        fetch('/api/system/performance').catch(() => null),
      ]);

      if (!logsResp.ok) {
        throw new Error(t('logs.error.load'));
      }

      const logsData: LogsResponse = await logsResp.json();
      setLogs(logsData.items || []);
      setSummary(logsData.summary || { total: 0, by_level: { DEBUG: 0, ERROR: 0, WARN: 0, INFO: 0 } });

      if (perfResp && perfResp.ok) {
        const perfData = await perfResp.json().catch(() => null);
        setPerformance(perfData);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t('logs.error.unknown'));
    } finally {
      setLoading(false);
    }
  }, [filterLevel, query, taskKey, t]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const copyRawLog = async (raw: string, index: number) => {
    try {
      await navigator.clipboard.writeText(raw);
      setCopiedIndex(index);
      window.setTimeout(() => setCopiedIndex(null), 1500);
    } catch (err) {
      console.error('copy failed', err);
    }
  };

  return (
    <div className={embedded ? 'space-y-6' : 'p-6 max-w-[1600px] mx-auto space-y-6'}>
      {!embedded && (
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white">{t('logs.title')}</h1>
          <p className="text-gray-500 mt-1">{t('logs.subtitle')}</p>
        </div>

        <div className="flex flex-col gap-3 sm:flex-row">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && fetchData()}
              placeholder={t('logs.searchPlaceholder')}
              className="w-full sm:w-64 rounded-lg border border-gray-700 bg-gray-900 pl-10 pr-4 py-2 text-sm text-white focus:outline-hidden focus:ring-2 focus:ring-komgaPrimary/40"
            />
          </div>
          <select
            value={filterLevel}
            onChange={(e) => setFilterLevel(e.target.value)}
            className="rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white focus:outline-hidden focus:ring-2 focus:ring-komgaPrimary/40"
          >
            <option value="ALL">{t('logs.level.all')}</option>
            <option value="DEBUG">{t('logs.level.debug')}</option>
            <option value="ERROR">{t('logs.level.error')}</option>
            <option value="WARN">{t('logs.level.warn')}</option>
            <option value="INFO">{t('logs.level.info')}</option>
          </select>
          <button
            onClick={fetchData}
            disabled={loading}
            className="inline-flex items-center justify-center gap-2 rounded-lg bg-komgaPrimary px-4 py-2 text-sm text-gray-950 font-medium hover:bg-komgaPrimaryHover disabled:opacity-60 transition-colors"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            {t('common.refresh')}
          </button>
        </div>
      </div>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label={t('logs.metric.logs')} value={summary.total} tone="blue" />
        <MetricCard label={t('logs.metric.debug')} value={summary.by_level.DEBUG || 0} tone="purple" />
        <MetricCard label={t('logs.metric.error')} value={summary.by_level.ERROR || 0} tone="red" />
        <MetricCard label={t('logs.metric.warn')} value={summary.by_level.WARN || 0} tone="amber" />
      </div>

      {taskKey && (
        <div className="flex items-center gap-2 rounded-xl border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-200">
          <span className="font-mono text-xs">task_key = {taskKey}</span>
          {onClearTaskKey && (
            <button
              type="button"
              onClick={onClearTaskKey}
              className="ml-auto rounded-sm border border-amber-500/40 px-2 py-0.5 text-xs hover:bg-amber-500/20"
            >
              {t('logs.taskFilter.clear')}
            </button>
          )}
        </div>
      )}

      <div className="grid gap-6 xl:grid-cols-[1.55fr_1fr]">
        <div className="rounded-2xl border border-gray-700 bg-gray-950 overflow-hidden">
          <div className="flex items-center gap-2 border-b border-gray-700 px-4 py-3">
            <Terminal className="w-4 h-4 text-gray-500" />
            <span className="text-sm text-gray-400">{t('logs.systemLogs')}</span>
          </div>
          <div className="max-h-[70vh] overflow-auto p-4 font-mono text-sm">
            {loading && logs.length === 0 ? (
              <div className="flex h-56 items-center justify-center text-gray-500">
                <RefreshCw className="w-6 h-6 animate-spin" />
              </div>
            ) : error ? (
              <div className="flex h-56 flex-col items-center justify-center gap-2 text-red-500">
                <AlertTriangle className="w-8 h-8" />
                <span>{error}</span>
              </div>
            ) : logs.length === 0 ? (
              <div className="flex h-56 flex-col items-center justify-center gap-2 text-gray-500">
                <Info className="w-8 h-8" />
                <span>{t('logs.empty')}</span>
              </div>
            ) : (
              <div className="space-y-2">
                {logs.map((log, index) => (
                  <div key={`${log.time}-${index}`} className="rounded-xl border border-gray-700 bg-gray-900/70 p-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-gray-500">{formatDateTime(log.time)}</span>
                      <span className={`rounded-full border px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wide ${logLevelBadgeClass(log.level)}`}>
                        {log.level}
                      </span>
                      <button
                        onClick={() => copyRawLog(log.raw, index)}
                        className="ml-auto inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-gray-500 hover:bg-gray-800 hover:text-white"
                      >
                        <Copy className="w-3 h-3" />
                        {copiedIndex === index ? t('logs.copied') : t('logs.copyRaw')}
                      </button>
                    </div>
                    <p className="mt-2 whitespace-pre-wrap wrap-break-word text-white">{log.msg || log.raw}</p>
                    {log.msg !== log.raw && (
                      <p className="mt-2 whitespace-pre-wrap wrap-break-word text-xs text-gray-500">{log.raw}</p>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        <div className="space-y-6">
          {/* 系统运行性能迷你看板 */}
          {performance && (
            <div className="rounded-2xl border border-gray-700 bg-gray-900 p-4 space-y-3 shadow-lg shadow-black/5 select-none">
              <div className="flex items-center gap-2 border-b border-gray-700 pb-2">
                <span className="w-1.5 h-1.5 rounded-full bg-komgaPrimary animate-ping shrink-0"></span>
                <h2 className="text-xs font-semibold uppercase tracking-wider text-komgaPrimary">系统性能 Ops Dashboard</h2>
              </div>
              <div className="grid grid-cols-2 gap-2">
                <div className="rounded-xl border border-gray-700 bg-gray-950 p-3 text-center transition-all hover:bg-gray-800">
                  <p className="text-[10px] text-gray-500 font-semibold tracking-wider">平均响应时间</p>
                  <p className="mt-1 text-base font-bold text-komgaPrimary">{performance.average_ms ?? 0}ms</p>
                </div>
                <div className="rounded-xl border border-gray-700 bg-gray-950 p-3 text-center transition-all hover:bg-gray-800">
                  <p className="text-[10px] text-gray-500 font-semibold tracking-wider">P95 响应延迟</p>
                  <p className="mt-1 text-base font-bold text-komgaSecondary">{performance.p95_ms ?? 0}ms</p>
                </div>
                <div className="rounded-xl border border-gray-700 bg-gray-950 p-3 text-center transition-all hover:bg-gray-800">
                  <p className="text-[10px] text-gray-500 font-semibold tracking-wider">图片缓存率</p>
                  <p className="mt-1 text-base font-bold text-komgaPrimary">
                    {performance.page_image_requests > 0 ? Math.round((performance.page_image_cache_hits / performance.page_image_requests) * 100) : 0}%
                  </p>
                </div>
                <div className="rounded-xl border border-gray-700 bg-gray-950 p-3 text-center transition-all hover:bg-gray-800">
                  <p className="text-[10px] text-gray-500 font-semibold tracking-wider">IO 等待时间</p>
                  <p className="mt-1 text-base font-bold text-komgaSecondary">{performance.page_image_io_wait_ms ?? 0}ms</p>
                </div>
              </div>
              <div className="text-[10px] text-gray-500 flex justify-between px-1">
                <span>并发处理归档: {performance.page_image_archive_opens ?? 0} 个</span>
                <span>总处理流量: {performance.total_bytes ? `${Math.round(performance.total_bytes / 1024 / 1024)} MB` : '0 B'}</span>
              </div>
            </div>
          )}

          <div className="rounded-2xl border border-gray-700 bg-gray-900 p-4">
            <div className="mb-3 flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-gray-500" />
              <h2 className="text-sm font-semibold text-white">{t('logs.tips.title')}</h2>
            </div>
            <ul className="space-y-2 text-sm text-gray-500">
              <li>{t('logs.tips.one')}</li>
              <li>{t('logs.tips.two')}</li>
              <li>{t('logs.tips.three')}</li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  );
}

function MetricCard({ label, value, tone }: { label: string; value: number; tone: 'blue' | 'red' | 'amber' | 'purple' }) {
  const toneClass = {
    blue: 'border-komgaPrimary/20 bg-komgaPrimary/10 text-komgaPrimary',
    red: 'border-red-500/20 bg-red-500/10 text-red-500',
    amber: 'border-amber-500/20 bg-amber-500/10 text-amber-500',
    purple: 'border-komgaSecondary/20 bg-komgaSecondary/10 text-komgaSecondary',
  }[tone];

  return (
    <div className={`rounded-2xl border p-4 ${toneClass}`}>
      <p className="text-sm opacity-80">{label}</p>
      <p className="mt-2 text-3xl font-semibold text-white">{value}</p>
    </div>
  );
}
