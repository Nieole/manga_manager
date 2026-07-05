/**
 * 业务说明：本文件是业务实现，属于项目源码的一部分，负责支撑漫画管理器在资料库、阅读器、扫描、元数据或系统设置中的具体业务能力。
 * 它与相邻模块共同组成前后端业务链路，修改时需要结合调用方理解数据流和用户可见行为。
 * 维护时应关注输入输出契约、错误处理、状态同步和与既有业务语义的一致性。
 */

import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiClient } from '../api/client';
import { BookOpen, ChevronDown, ChevronRight, Fingerprint, FileQuestion, ImageOff, Library, ListChecks, RefreshCw, Search, ShieldAlert, Tags } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';
import { useToast } from '../components/ToastProvider';
import { DuplicatesPanel } from '../components/DuplicatesPanel';

interface LibraryOption {
  id: number;
  name: string;
}

interface HealthSummary {
  type: string;
  severity: string;
  count: number;
}

interface HealthIssue {
  type: string;
  severity: string;
  library_id?: number;
  library_name?: string;
  series_id?: number;
  series_name?: string;
  book_id?: number;
  book_name?: string;
  path?: string;
  detail?: string;
  count?: number;
  last_task_key?: string;
}

interface HealthReport {
  summary: HealthSummary[];
  issues: HealthIssue[];
  limit: number;
}

const ISSUE_TYPES = [
  'empty_pages',
  'missing_cover',
  'missing_metadata',
  'missing_quick_hash',
  'duplicate_file_hash',
  'duplicate_quick_hash',
  'unmatched_koreader',
];

function issueIcon(type: string) {
  switch (type) {
    case 'empty_pages':
      return <FileQuestion className="h-5 w-5" />;
    case 'missing_cover':
      return <ImageOff className="h-5 w-5" />;
    case 'missing_metadata':
      return <Tags className="h-5 w-5" />;
    case 'missing_quick_hash':
      return <Fingerprint className="h-5 w-5" />;
    case 'duplicate_file_hash':
    case 'duplicate_quick_hash':
      return <ShieldAlert className="h-5 w-5" />;
    default:
      return <BookOpen className="h-5 w-5" />;
  }
}

function severityClass(severity: string) {
  switch (severity) {
    case 'error':
      return 'border-red-300/60 bg-red-50 text-red-600 dark:border-red-500/20 dark:bg-red-500/10 dark:text-red-200';
    case 'warn':
      return 'border-amber-300/60 bg-amber-50 text-amber-700 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-100';
    default:
      return 'border-sky-300/60 bg-sky-50 text-sky-700 dark:border-sky-500/20 dark:bg-sky-500/10 dark:text-sky-100';
  }
}

export default function Organize() {
  const { t, formatNumber } = useI18n();
  const navigate = useNavigate();
  const [libraries, setLibraries] = useState<LibraryOption[]>([]);
  const [report, setReport] = useState<HealthReport | null>(null);
  const [libraryId, setLibraryId] = useState('ALL');
  const [issueType, setIssueType] = useState('ALL');
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(true);
  const [actionKey, setActionKey] = useState<string | null>(null);
  const [severityExpanded, setSeverityExpanded] = useState<Record<string, boolean>>({ error: true, warn: true, info: false });
  const { showToast } = useToast();

  const fetchReport = async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ limit: '80' });
      if (libraryId !== 'ALL') params.set('library_id', libraryId);
      const [librariesRes, reportRes] = await Promise.all([
        apiClient.get<LibraryOption[]>('/api/libraries').catch(() => ({ data: [] as LibraryOption[] })),
        apiClient.get<HealthReport>(`/api/health/report?${params.toString()}`),
      ]);
      setLibraries(Array.isArray(librariesRes.data) ? librariesRes.data : []);
      setReport(reportRes.data);
    } catch (error) {
      console.error(error);
      showToast(t('organize.toast.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchReport();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [libraryId]);

  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === 'visible') {
        fetchReport();
      }
    };
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('focus', onVisible);
    return () => {
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('focus', onVisible);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [libraryId]);

  const summaryByType = useMemo(() => {
    const map = new Map<string, HealthSummary>();
    report?.summary.forEach((item) => map.set(item.type, item));
    return map;
  }, [report]);

  const filteredIssues = useMemo(() => {
    const needle = query.trim().toLowerCase();
    let items = report?.issues || [];
    if (issueType !== 'ALL') {
      items = items.filter((item) => item.type === issueType);
    }
    if (!needle) return items;
    return items.filter((item) => [
      item.series_name,
      item.book_name,
      item.library_name,
      item.path,
      item.detail,
    ].some((value) => value?.toLowerCase().includes(needle)));
  }, [query, report?.issues, issueType]);

  const totalIssueCount = report?.summary.reduce((sum, item) => sum + item.count, 0) ?? 0;
  const missingQuickHashCount = summaryByType.get('missing_quick_hash')?.count ?? 0;
  const duplicateQuickHashCount = summaryByType.get('duplicate_quick_hash')?.count ?? 0;
  const duplicateFileHashCount = summaryByType.get('duplicate_file_hash')?.count ?? 0;

  const severityCounts = useMemo(() => {
    const out: Record<string, number> = { error: 0, warn: 0, info: 0 };
    report?.summary.forEach((item) => {
      const key = item.severity === 'error' || item.severity === 'warn' ? item.severity : 'info';
      out[key] += item.count;
    });
    return out;
  }, [report?.summary]);

  const groupedTypes = useMemo(() => {
    const groups: Record<string, string[]> = { error: [], warn: [], info: [] };
    ISSUE_TYPES.forEach((type) => {
      const sev = summaryByType.get(type)?.severity || 'info';
      const bucket = sev === 'error' || sev === 'warn' ? sev : 'info';
      groups[bucket].push(type);
    });
    return groups;
  }, [summaryByType]);

  const openIssue = (issue: HealthIssue) => {
    if (issue.series_id) {
      navigate(`/series/${issue.series_id}`);
      return;
    }
    if (issue.book_id) {
      navigate(`/reader/${issue.book_id}`);
      return;
    }
    if (issue.type === 'unmatched_koreader') {
      navigate('/settings/koreader');
      return;
    }
    navigate('/ops?tab=logs');
  };

  const runIssueAction = async (issue: HealthIssue) => {
    if (!issue.series_id && !issue.library_id && issue.type !== 'unmatched_koreader') return;
    const key = issue.type === 'missing_quick_hash'
      ? 'rebuild_file_identities'
      : `${issue.type}-${issue.series_id || issue.library_id || issue.path}`;
    setActionKey(key);
    try {
      if (issue.type === 'missing_metadata' && issue.series_id) {
        await apiClient.post(`/api/series/${issue.series_id}/scrape`);
        showToast(t('organize.toast.scrapeQueued'));
      } else if ((issue.type === 'empty_pages' || issue.type === 'missing_cover') && issue.series_id) {
        await apiClient.post(`/api/series/${issue.series_id}/rescan?force=true`);
        showToast(t('organize.toast.rescanQueued'));
      } else if (issue.type === 'missing_quick_hash') {
        await apiClient.post('/api/system/rebuild-file-identities');
        showToast(t('organize.toast.identityQueued'));
      } else if (issue.type === 'unmatched_koreader') {
        await apiClient.post('/api/system/koreader/reconcile');
        showToast(t('organize.toast.koreaderQueued'));
      }
      await fetchReport();
    } catch (error) {
      console.error(error);
      showToast(t('organize.toast.actionFailed'), 'error');
    } finally {
      setActionKey(null);
    }
  };

  const canRunAction = (issue: HealthIssue) =>
    issue.type === 'missing_metadata' ||
    issue.type === 'empty_pages' ||
    issue.type === 'missing_cover' ||
    issue.type === 'missing_quick_hash' ||
    issue.type === 'unmatched_koreader';

  const rebuildFileIdentities = async () => {
    setActionKey('rebuild_file_identities');
    try {
      await apiClient.post('/api/system/rebuild-file-identities');
      showToast(t('organize.toast.identityQueued'));
    } catch (error) {
      console.error(error);
      showToast(t('organize.toast.actionFailed'), 'error');
    } finally {
      setActionKey(null);
    }
  };

  return (
    <div className="mx-auto max-w-[1600px] space-y-6 p-4 sm:p-8 select-none">
      {/* 顶栏 */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between border-b border-gray-800/60 pb-6">
        <div>
          <div className="inline-flex items-center gap-2 rounded-full border border-emerald-500/20 bg-emerald-500/10 px-3 py-1 text-xs font-semibold uppercase tracking-wide text-emerald-300">
            <ListChecks className="h-4 w-4" />
            {t('organize.badge')}
          </div>
          <h1 className="mt-3 text-3xl font-bold tracking-tight text-white">{t('organize.title')}</h1>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-gray-400">{t('organize.description')}</p>
        </div>
        <button
          onClick={fetchReport}
          disabled={loading}
          className="inline-flex items-center justify-center gap-2 rounded-xl border border-gray-700 bg-gray-900 px-4 py-2.5 text-sm text-gray-200 hover:bg-gray-800 disabled:opacity-60 transition-all active:scale-95 shrink-0"
        >
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          {t('common.refresh')}
        </button>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-[340px_1fr] gap-6 items-start">
        {/* 左侧：健康指标看板与全局哈希重建 */}
        <div className="space-y-6">
          {/* 指标总览 */}
          <div className="rounded-2xl border border-gray-800/80 bg-gray-950/50 backdrop-blur-md p-4 space-y-3">
            <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-gray-500 px-1">{t('organize.metric.title')}</h3>
            <div className="grid grid-cols-2 gap-2">
              <div className="rounded-xl border border-white/5 bg-white/2 p-3">
                <p className="text-[10px] uppercase tracking-wide text-gray-500">{t('organize.metric.totalIssues')}</p>
                <p className="mt-1 text-2xl font-bold text-white">{formatNumber(totalIssueCount)}</p>
              </div>
              <div className="rounded-xl border border-white/5 bg-white/2 p-3">
                <p className="text-[10px] uppercase tracking-wide text-gray-500">{t('organize.metric.visibleIssues')}</p>
                <p className="mt-1 text-2xl font-bold text-emerald-400">{formatNumber(filteredIssues.length)}</p>
              </div>
            </div>
          </div>

          {/* 7 大健康过滤卡片 */}
          <div className="space-y-2">
            <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-gray-500 px-1">{t('organize.metric.diagnostics')}</h3>
            <div className="rounded-xl border border-gray-800 bg-gray-950/50 px-2 py-2 grid grid-cols-3 gap-1 text-center">
              {(['error', 'warn', 'info'] as const).map((sev) => (
                <div key={sev} className={`rounded-lg px-2 py-1.5 ${severityClass(sev)}`}>
                  <p className="text-[10px] uppercase tracking-wide opacity-80">{t(`organize.severity.${sev}`)}</p>
                  <p className="mt-0.5 text-base font-bold">{formatNumber(severityCounts[sev] || 0)}</p>
                </div>
              ))}
            </div>
            <button
              onClick={() => setIssueType('ALL')}
              className={`w-full flex items-center justify-between rounded-xl border p-3 text-left transition-all ${issueType === 'ALL' ? 'border-komgaPrimary/40 bg-komgaPrimary/10 text-white font-medium shadow-[0_0_15px_rgba(147,51,234,0.05)]' : 'border-gray-800/60 bg-gray-900/40 hover:bg-gray-800/50 text-gray-400'}`}
            >
              <span className="text-xs font-semibold">{t('organize.filters.allIssues')}</span>
              <span className={`text-xs font-bold px-2 py-0.5 rounded-full ${issueType === 'ALL' ? 'bg-komgaPrimary/20 text-white' : 'bg-gray-700 text-white'}`}>{formatNumber(totalIssueCount)}</span>
            </button>

            {(['error', 'warn', 'info'] as const).map((sev) => {
              const types = groupedTypes[sev] || [];
              if (types.length === 0) return null;
              const expanded = severityExpanded[sev];
              const groupCount = types.reduce((sum, type) => sum + (summaryByType.get(type)?.count ?? 0), 0);
              return (
                <div key={sev} className="space-y-1">
                  <button
                    type="button"
                    onClick={() => setSeverityExpanded((prev) => ({ ...prev, [sev]: !prev[sev] }))}
                    className={`w-full flex items-center justify-between rounded-lg border px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wide ${severityClass(sev)}`}
                  >
                    <span className="flex items-center gap-1.5">
                      {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                      {t(`organize.severity.${sev}`)}
                    </span>
                    <span className="text-xs font-bold opacity-90">{formatNumber(groupCount)}</span>
                  </button>
                  {expanded && types.map((type) => {
                    const summary = summaryByType.get(type);
                    const active = issueType === type;
                    return (
                      <button
                        key={type}
                        onClick={() => setIssueType(active ? 'ALL' : type)}
                        className={`w-full rounded-xl border p-3 text-left transition-all hover:scale-[1.01] ${active ? 'border-komgaPrimary/50 bg-komgaPrimary/10 shadow-[0_0_15px_rgba(147,51,234,0.05)]' : 'border-gray-800 bg-gray-900/60 hover:bg-gray-800/40'}`}
                      >
                        <div className="flex items-center justify-between gap-3">
                          <div className="flex items-center gap-3">
                            <div className={`rounded-lg border p-1.5 shrink-0 ${severityClass(summary?.severity || 'info')}`}>
                              {issueIcon(type)}
                            </div>
                            <div>
                              <p className="text-xs font-semibold text-white">{t(`organize.issue.${type}`)}</p>
                              <p className="text-[10px] text-gray-500 line-clamp-1 mt-0.5">{t(`organize.issue.${type}.hint`)}</p>
                            </div>
                          </div>
                          <span className={`text-sm font-bold shrink-0 px-2 py-0.5 rounded-full ${active ? 'bg-komgaPrimary/20 text-white' : 'bg-gray-700 text-white'}`}>{formatNumber(summary?.count ?? 0)}</span>
                        </div>
                      </button>
                    );
                  })}
                </div>
              );
            })}
          </div>

          {/* 哈希重建工具 */}
          <section className="rounded-2xl border border-gray-800 bg-gray-900 p-4 space-y-4">
            <div>
              <p className="text-[10px] uppercase tracking-wide text-purple-400 font-bold">{t('organize.identity.badge')}</p>
              <h2 className="mt-1 text-sm font-semibold text-white">{t('organize.identity.title')}</h2>
              <p className="mt-1 text-xs leading-relaxed text-gray-500">{t('organize.identity.description')}</p>
            </div>
            <button
              onClick={rebuildFileIdentities}
              disabled={actionKey === 'rebuild_file_identities'}
              className="w-full inline-flex items-center justify-center gap-2 rounded-xl border border-komgaPrimary/30 bg-komgaPrimary/10 px-4 py-2.5 text-xs text-komgaPrimary hover:bg-komgaPrimary/20 disabled:opacity-60 transition-all font-medium"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${actionKey === 'rebuild_file_identities' ? 'animate-spin' : ''}`} />
              {t('organize.identity.rebuild')}
            </button>
            <div className="grid gap-2">
              <div className="flex items-center justify-between rounded-lg border border-white/5 bg-white/1 px-3 py-2">
                <span className="text-[11px] text-gray-500">{t('organize.identity.missingQuickHash')}</span>
                <span className="text-xs font-semibold text-white">{formatNumber(missingQuickHashCount)}</span>
              </div>
              <div className="flex items-center justify-between rounded-lg border border-white/5 bg-white/1 px-3 py-2">
                <span className="text-[11px] text-gray-500">{t('organize.identity.duplicateQuickHash')}</span>
                <span className="text-xs font-semibold text-white">{formatNumber(duplicateQuickHashCount)}</span>
              </div>
              <div className="flex items-center justify-between rounded-lg border border-white/5 bg-white/1 px-3 py-2">
                <span className="text-[11px] text-gray-500">{t('organize.identity.duplicateFileHash')}</span>
                <span className="text-xs font-semibold text-white">{formatNumber(duplicateFileHashCount)}</span>
              </div>
            </div>
          </section>
        </div>

        {/* 右侧：错误大列表与原地快捷修复台 */}
        <div className="space-y-4">
          <section className="rounded-2xl border border-gray-800 bg-gray-950/40 backdrop-blur-md overflow-hidden">
            {/* 顶栏过滤器 */}
            <div className="grid gap-3 border-b border-gray-800/80 p-4 md:grid-cols-[200px_1fr]">
              <select
                value={libraryId}
                onChange={(e) => setLibraryId(e.target.value)}
                className="rounded-xl border border-gray-800 bg-gray-900 px-3 py-2 text-xs text-gray-300 focus:outline-hidden focus:ring-1 focus:ring-komgaPrimary/40"
              >
                <option value="ALL">{t('organize.filters.allLibraries')}</option>
                {libraries.map((library) => (
                  <option key={library.id} value={library.id}>{library.name}</option>
                ))}
              </select>
              <div className="relative">
                <Search className="absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-500" />
                <input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder={t('organize.filters.search')}
                  className="w-full rounded-xl border border-gray-800 bg-gray-900 py-2 pl-10 pr-3 text-xs text-gray-300 focus:outline-hidden focus:ring-1 focus:ring-komgaPrimary/40"
                />
              </div>
            </div>

            {/* 修复项目列表 */}
            <div className="divide-y divide-gray-900/60 max-h-[80vh] overflow-y-auto">
              {loading ? (
                <div className="flex h-64 items-center justify-center text-gray-500">
                  <RefreshCw className="h-6 w-6 animate-spin text-komgaPrimary" />
                </div>
              ) : filteredIssues.length === 0 ? (
                <div className="flex h-64 flex-col items-center justify-center gap-2 text-gray-600">
                  <Library className="h-8 w-8 opacity-40" />
                  <p className="text-sm">{t('organize.empty')}</p>
                </div>
              ) : filteredIssues.map((issue, index) => {
                const actionKeyForIssue = issue.type === 'missing_quick_hash'
                  ? 'rebuild_file_identities'
                  : `${issue.type}-${issue.series_id || issue.library_id || issue.path}`;
                return (
                  <div key={`${issue.type}-${issue.book_id || issue.series_id || issue.path}-${index}`} className="p-4 hover:bg-white/1 transition-all group">
                    <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
                      <button onClick={() => openIssue(issue)} className="min-w-0 text-left flex-1 space-y-2 focus:outline-hidden">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] font-medium ${severityClass(issue.severity)}`}>
                            {issueIcon(issue.type)}
                            {t(`organize.issue.${issue.type}`)}
                          </span>
                          {issue.count && issue.count > 1 && (
                            <span className="rounded-full border border-gray-800 px-2 py-0.5 text-[10px] text-gray-500">
                              {t('organize.duplicateCount', { count: issue.count })}
                            </span>
                          )}
                        </div>
                        <p className="text-sm font-semibold text-white group-hover:text-komgaPrimary transition-colors">
                          {issue.book_name || issue.series_name || issue.path || t('common.unknown')}
                        </p>
                        <p className="text-xs text-gray-500 truncate">
                          {[issue.library_name, issue.series_name, issue.detail].filter(Boolean).join(' / ')}
                        </p>
                        {issue.path && <p className="truncate text-[10px] text-gray-600">{issue.path}</p>}
                      </button>

                      {/* 行尾悬浮动作组 */}
                      <div className="flex shrink-0 flex-wrap gap-2 md:opacity-0 group-hover:opacity-100 transition-opacity duration-200">
                        {issue.last_task_key && (
                          <button
                            onClick={() => navigate(`/ops?tab=tasks&task=${encodeURIComponent(issue.last_task_key!)}`)}
                            className="rounded-lg border border-cyan-500/30 bg-cyan-500/5 px-3 py-1.5 text-xs text-cyan-200/90 hover:bg-cyan-500/15 transition-colors"
                            title={issue.last_task_key}
                          >
                            {t('organize.openSourceTask')}
                          </button>
                        )}
                        <button
                          onClick={() => openIssue(issue)}
                          className="rounded-lg border border-gray-800 px-3 py-1.5 text-xs text-gray-400 hover:text-white hover:bg-gray-800/60 transition-colors"
                        >
                          {issue.series_id ? t('organize.openSeries') : issue.book_id ? t('organize.openReader') : t('organize.openTarget')}
                        </button>
                        {canRunAction(issue) && (
                          <button
                            onClick={() => runIssueAction(issue)}
                            disabled={actionKey === actionKeyForIssue}
                            className="inline-flex items-center gap-1.5 rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 px-3 py-1.5 text-xs text-komgaPrimary hover:bg-komgaPrimary/20 disabled:opacity-60 transition-all"
                          >
                            <RefreshCw className={`h-3 w-3 ${actionKey === actionKeyForIssue ? 'animate-spin' : ''}`} />
                            {issue.type === 'missing_metadata'
                              ? t('organize.action.scrape')
                              : issue.type === 'unmatched_koreader'
                                ? t('organize.action.reconcile')
                                : issue.type === 'missing_quick_hash'
                                  ? t('organize.action.rebuildIdentity')
                                  : t('organize.action.rescan')}
                          </button>
                        )}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </section>
        </div>
      </div>

      <DuplicatesPanel />
    </div>
  );
}
