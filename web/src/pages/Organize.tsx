import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import axios from 'axios';
import { BookOpen, Fingerprint, FileQuestion, ImageOff, Library, ListChecks, RefreshCw, Search, ShieldAlert, Tags } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';

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
      return 'border-red-500/20 bg-red-500/10 text-red-200';
    case 'warn':
      return 'border-amber-500/20 bg-amber-500/10 text-amber-100';
    default:
      return 'border-sky-500/20 bg-sky-500/10 text-sky-100';
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
  const [toast, setToast] = useState<{ text: string; type: 'success' | 'error' } | null>(null);

  const showToast = (text: string, type: 'success' | 'error' = 'success') => {
    setToast({ text, type });
    window.setTimeout(() => setToast(null), 3000);
  };

  const fetchReport = async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ limit: '80' });
      if (libraryId !== 'ALL') params.set('library_id', libraryId);
      if (issueType !== 'ALL') params.set('type', issueType);
      const [librariesRes, reportRes] = await Promise.all([
        axios.get<LibraryOption[]>('/api/libraries').catch(() => ({ data: [] as LibraryOption[] })),
        axios.get<HealthReport>(`/api/health/report?${params.toString()}`),
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
  }, [libraryId, issueType]);

  const summaryByType = useMemo(() => {
    const map = new Map<string, HealthSummary>();
    report?.summary.forEach((item) => map.set(item.type, item));
    return map;
  }, [report]);

  const filteredIssues = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const items = report?.issues || [];
    if (!needle) return items;
    return items.filter((item) => [
      item.series_name,
      item.book_name,
      item.library_name,
      item.path,
      item.detail,
    ].some((value) => value?.toLowerCase().includes(needle)));
  }, [query, report?.issues]);

  const totalIssueCount = report?.summary.reduce((sum, item) => sum + item.count, 0) ?? 0;
  const missingQuickHashCount = summaryByType.get('missing_quick_hash')?.count ?? 0;
  const duplicateQuickHashCount = summaryByType.get('duplicate_quick_hash')?.count ?? 0;
  const duplicateFileHashCount = summaryByType.get('duplicate_file_hash')?.count ?? 0;

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
    navigate('/logs');
  };

  const runIssueAction = async (issue: HealthIssue) => {
    if (!issue.series_id && !issue.library_id && issue.type !== 'unmatched_koreader') return;
    const key = issue.type === 'missing_quick_hash'
      ? 'rebuild_file_identities'
      : `${issue.type}-${issue.series_id || issue.library_id || issue.path}`;
    setActionKey(key);
    try {
      if (issue.type === 'missing_metadata' && issue.series_id) {
        await axios.post(`/api/series/${issue.series_id}/scrape`);
        showToast(t('organize.toast.scrapeQueued'));
      } else if ((issue.type === 'empty_pages' || issue.type === 'missing_cover') && issue.series_id) {
        await axios.post(`/api/series/${issue.series_id}/rescan?force=true`);
        showToast(t('organize.toast.rescanQueued'));
      } else if (issue.type === 'missing_quick_hash') {
        await axios.post('/api/system/rebuild-file-identities');
        showToast(t('organize.toast.identityQueued'));
      } else if (issue.type === 'unmatched_koreader') {
        await axios.post('/api/system/koreader/reconcile');
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
      await axios.post('/api/system/rebuild-file-identities');
      showToast(t('organize.toast.identityQueued'));
    } catch (error) {
      console.error(error);
      showToast(t('organize.toast.actionFailed'), 'error');
    } finally {
      setActionKey(null);
    }
  };

  return (
    <div className="mx-auto max-w-7xl space-y-6 p-4 sm:p-8">
      {toast && (
        <div className={`fixed right-6 top-20 z-50 rounded-xl border px-4 py-3 text-sm shadow-xl ${toast.type === 'error' ? 'border-red-500/20 bg-red-500/15 text-red-100' : 'border-emerald-500/20 bg-emerald-500/15 text-emerald-100'}`}>
          {toast.text}
        </div>
      )}

      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <div className="inline-flex items-center gap-2 rounded-full border border-emerald-500/20 bg-emerald-500/10 px-3 py-1 text-xs font-semibold uppercase tracking-wide text-emerald-200">
            <ListChecks className="h-4 w-4" />
            {t('organize.badge')}
          </div>
          <h1 className="mt-4 text-3xl font-bold tracking-tight text-white">{t('organize.title')}</h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-gray-400">{t('organize.description')}</p>
        </div>
        <button
          onClick={fetchReport}
          disabled={loading}
          className="inline-flex items-center justify-center gap-2 rounded-xl border border-gray-700 bg-gray-900 px-4 py-2.5 text-sm text-gray-200 hover:bg-gray-800 disabled:opacity-60"
        >
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          {t('common.refresh')}
        </button>
      </div>

      <div className="grid gap-3 md:grid-cols-3">
        <div className="rounded-2xl border border-gray-800 bg-gray-900 p-4">
          <p className="text-xs uppercase tracking-wide text-gray-500">{t('organize.metric.totalIssues')}</p>
          <p className="mt-2 text-3xl font-semibold text-white">{formatNumber(totalIssueCount)}</p>
        </div>
        <div className="rounded-2xl border border-gray-800 bg-gray-900 p-4">
          <p className="text-xs uppercase tracking-wide text-gray-500">{t('organize.metric.visibleIssues')}</p>
          <p className="mt-2 text-3xl font-semibold text-white">{formatNumber(filteredIssues.length)}</p>
        </div>
        <div className="rounded-2xl border border-gray-800 bg-gray-900 p-4">
          <p className="text-xs uppercase tracking-wide text-gray-500">{t('organize.metric.libraries')}</p>
          <p className="mt-2 text-3xl font-semibold text-white">{formatNumber(libraries.length)}</p>
        </div>
      </div>

      <section className="rounded-2xl border border-gray-800 bg-gray-900 p-4">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <p className="text-xs uppercase tracking-wide text-gray-500">{t('organize.identity.badge')}</p>
            <h2 className="mt-2 text-lg font-semibold text-white">{t('organize.identity.title')}</h2>
            <p className="mt-1 max-w-3xl text-sm leading-6 text-gray-400">{t('organize.identity.description')}</p>
          </div>
          <button
            onClick={rebuildFileIdentities}
            disabled={actionKey === 'rebuild_file_identities'}
            className="inline-flex items-center justify-center gap-2 rounded-xl border border-komgaPrimary/30 bg-komgaPrimary/10 px-4 py-2.5 text-sm text-komgaPrimary hover:bg-komgaPrimary/20 disabled:opacity-60"
          >
            <RefreshCw className={`h-4 w-4 ${actionKey === 'rebuild_file_identities' ? 'animate-spin' : ''}`} />
            {t('organize.identity.rebuild')}
          </button>
        </div>
        <div className="mt-4 grid gap-3 md:grid-cols-3">
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-gray-500">{t('organize.identity.missingQuickHash')}</p>
            <p className="mt-2 text-2xl font-semibold text-white">{formatNumber(missingQuickHashCount)}</p>
          </div>
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-gray-500">{t('organize.identity.duplicateQuickHash')}</p>
            <p className="mt-2 text-2xl font-semibold text-white">{formatNumber(duplicateQuickHashCount)}</p>
          </div>
          <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-gray-500">{t('organize.identity.duplicateFileHash')}</p>
            <p className="mt-2 text-2xl font-semibold text-white">{formatNumber(duplicateFileHashCount)}</p>
          </div>
        </div>
      </section>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {ISSUE_TYPES.map((type) => {
          const summary = summaryByType.get(type);
          const active = issueType === type;
          return (
            <button
              key={type}
              onClick={() => setIssueType(active ? 'ALL' : type)}
              className={`rounded-2xl border p-4 text-left transition ${active ? 'border-komgaPrimary/50 bg-komgaPrimary/10' : 'border-gray-800 bg-gray-900 hover:bg-gray-800/70'}`}
            >
              <div className="flex items-center justify-between gap-3">
                <div className={`rounded-xl border p-2 ${severityClass(summary?.severity || 'info')}`}>
                  {issueIcon(type)}
                </div>
                <span className="text-2xl font-semibold text-white">{formatNumber(summary?.count ?? 0)}</span>
              </div>
              <p className="mt-3 text-sm font-semibold text-white">{t(`organize.issue.${type}`)}</p>
              <p className="mt-1 text-xs text-gray-500">{t(`organize.issue.${type}.hint`)}</p>
            </button>
          );
        })}
      </div>

      <section className="rounded-2xl border border-gray-800 bg-gray-900">
        <div className="grid gap-3 border-b border-gray-800 p-4 lg:grid-cols-[220px_220px_1fr]">
          <select
            value={libraryId}
            onChange={(e) => setLibraryId(e.target.value)}
            className="rounded-xl border border-gray-700 bg-gray-950 px-3 py-2 text-sm text-gray-200"
          >
            <option value="ALL">{t('organize.filters.allLibraries')}</option>
            {libraries.map((library) => (
              <option key={library.id} value={library.id}>{library.name}</option>
            ))}
          </select>
          <select
            value={issueType}
            onChange={(e) => setIssueType(e.target.value)}
            className="rounded-xl border border-gray-700 bg-gray-950 px-3 py-2 text-sm text-gray-200"
          >
            <option value="ALL">{t('organize.filters.allIssues')}</option>
            {ISSUE_TYPES.map((type) => (
              <option key={type} value={type}>{t(`organize.issue.${type}`)}</option>
            ))}
          </select>
          <div className="relative">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('organize.filters.search')}
              className="w-full rounded-xl border border-gray-700 bg-gray-950 py-2 pl-10 pr-3 text-sm text-gray-200"
            />
          </div>
        </div>

        <div className="divide-y divide-gray-800">
          {loading ? (
            <div className="flex h-56 items-center justify-center text-gray-500">
              <RefreshCw className="h-7 w-7 animate-spin" />
            </div>
          ) : filteredIssues.length === 0 ? (
            <div className="flex h-56 flex-col items-center justify-center gap-3 text-gray-500">
              <Library className="h-8 w-8" />
              <p>{t('organize.empty')}</p>
            </div>
          ) : filteredIssues.map((issue, index) => {
            const actionKeyForIssue = issue.type === 'missing_quick_hash'
              ? 'rebuild_file_identities'
              : `${issue.type}-${issue.series_id || issue.library_id || issue.path}`;
            return (
              <div key={`${issue.type}-${issue.book_id || issue.series_id || issue.path}-${index}`} className="p-4">
                <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
                  <button onClick={() => openIssue(issue)} className="min-w-0 text-left">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs ${severityClass(issue.severity)}`}>
                        {issueIcon(issue.type)}
                        {t(`organize.issue.${issue.type}`)}
                      </span>
                      {issue.count && issue.count > 1 && (
                        <span className="rounded-full border border-gray-700 px-2.5 py-1 text-xs text-gray-400">
                          {t('organize.duplicateCount', { count: issue.count })}
                        </span>
                      )}
                    </div>
                    <p className="mt-3 text-base font-semibold text-white">
                      {issue.book_name || issue.series_name || issue.path || t('common.unknown')}
                    </p>
                    <p className="mt-1 text-sm text-gray-400">
                      {[issue.library_name, issue.series_name, issue.detail].filter(Boolean).join(' / ')}
                    </p>
                    {issue.path && <p className="mt-2 truncate text-xs text-gray-600">{issue.path}</p>}
                  </button>
                  <div className="flex shrink-0 flex-wrap gap-2">
                    <button
                      onClick={() => openIssue(issue)}
                      className="rounded-lg border border-gray-700 px-3 py-2 text-xs text-gray-200 hover:bg-gray-800"
                    >
                      {issue.series_id ? t('organize.openSeries') : issue.book_id ? t('organize.openReader') : t('organize.openTarget')}
                    </button>
                    {canRunAction(issue) && (
                      <button
                        onClick={() => runIssueAction(issue)}
                        disabled={actionKey === actionKeyForIssue}
                        className="inline-flex items-center gap-2 rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 px-3 py-2 text-xs text-komgaPrimary hover:bg-komgaPrimary/20 disabled:opacity-60"
                      >
                        <RefreshCw className={`h-3.5 w-3.5 ${actionKey === actionKeyForIssue ? 'animate-spin' : ''}`} />
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
  );
}
