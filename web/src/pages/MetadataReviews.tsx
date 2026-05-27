import { useEffect, useMemo, useState } from 'react';
import axios from 'axios';
import { Link, useOutletContext } from 'react-router-dom';
import { CheckCircle2, ExternalLink, Filter, GitCompareArrows, Loader2, Search, ShieldCheck, XCircle } from 'lucide-react';
import type { MetadataReviewInboxItem, MetadataReviewInboxResponse } from './series-detail/types';
import { useI18n } from '../i18n/LocaleProvider';
import { useToast } from '../components/ToastProvider';

interface LibraryOption {
  id: number | string;
  name: string;
}

type BulkMode = 'fill_empty' | 'all';

function getApiErrorMessage(error: unknown, fallback: string) {
  if (axios.isAxiosError(error)) {
    return error.response?.data?.error || error.message || fallback;
  }
  if (error instanceof Error) return error.message;
  return fallback;
}

function percent(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0%';
  return `${Math.round(Math.min(1, value) * 100)}%`;
}

function displaySeriesTitle(item: MetadataReviewInboxItem) {
  return item.series_title || item.series_name;
}

export default function MetadataReviews({ embedded }: { embedded?: boolean } = {}) {
  const { t, formatDateTime } = useI18n();
  const globalToast = useToast();

  // When embedded, we don't have an outlet context, so we use defaults
  let refreshTrigger = 0;
  let libraries: LibraryOption[] = [];
  try {
    if (!embedded) {
      // eslint-disable-next-line react-hooks/rules-of-hooks
      const ctx = useOutletContext<{ refreshTrigger: number; libraries?: LibraryOption[] }>();
      refreshTrigger = ctx?.refreshTrigger ?? 0;
      libraries = ctx?.libraries ?? [];
    }
  } catch {
    // Fallback if not in outlet context
  }
  const [items, setItems] = useState<MetadataReviewInboxItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [acting, setActing] = useState(false);
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  const [libraryId, setLibraryId] = useState('0');
  const [provider, setProvider] = useState('');
  const [query, setQuery] = useState('');
  const [mode, setMode] = useState<BulkMode>('fill_empty');
  const [page, setPage] = useState(1);
  const [toastMsg, setToastMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);

  const limit = 30;
  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds]);
  const currentPageSelected = items.length > 0 && items.every((item) => selectedSet.has(item.id));
  const providers = useMemo(() => Array.from(new Set(items.map((item) => item.provider).filter(Boolean))).sort(), [items]);

  const showToast = (text: string, type: 'success' | 'error' = 'success') => {
    if (embedded) {
      globalToast.showToast(text, type);
      return;
    }
    setToastMsg({ text, type });
    window.setTimeout(() => setToastMsg(null), 3200);
  };

  const loadReviews = async () => {
    setLoading(true);
    try {
      const offset = (page - 1) * limit;
      const res = await axios.get<MetadataReviewInboxResponse>('/api/metadata/reviews', {
        params: {
          library_id: libraryId,
          provider,
          q: query.trim(),
          limit,
          offset,
        },
      });
      setItems(res.data.items || []);
      setTotal(res.data.total || 0);
      setSelectedIds((current) => current.filter((id) => (res.data.items || []).some((item) => item.id === id)));
    } catch (err: unknown) {
      showToast(getApiErrorMessage(err, t('metadataReviews.toast.loadFailed')), 'error');
      setItems([]);
      setTotal(0);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadReviews();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [libraryId, provider, page, refreshTrigger]);

  const submitSearch = (event: React.FormEvent) => {
    event.preventDefault();
    setPage(1);
    void loadReviews();
  };

  const toggleSelected = (id: number) => {
    setSelectedIds((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id]);
  };

  const toggleCurrentPage = () => {
    if (currentPageSelected) {
      const pageIds = new Set(items.map((item) => item.id));
      setSelectedIds((current) => current.filter((id) => !pageIds.has(id)));
      return;
    }
    setSelectedIds((current) => Array.from(new Set([...current, ...items.map((item) => item.id)])));
  };

  const runBulkAction = async (action: 'apply' | 'reject') => {
    if (selectedIds.length === 0) return;
    setActing(true);
    try {
      const endpoint = action === 'apply' ? '/api/metadata/reviews/bulk-apply' : '/api/metadata/reviews/bulk-reject';
      const res = await axios.post(endpoint, { review_ids: selectedIds, mode });
      const doneCount = action === 'apply' ? (res.data.applied?.length || 0) : (res.data.rejected?.length || 0);
      const skippedCount = res.data.skipped?.length || 0;
      const failedCount = res.data.failed?.length || 0;
      showToast(t(action === 'apply' ? 'metadataReviews.toast.applied' : 'metadataReviews.toast.rejected', {
        count: doneCount,
        skipped: skippedCount,
        failed: failedCount,
      }), failedCount > 0 ? 'error' : 'success');
      setSelectedIds([]);
      await loadReviews();
    } catch (err: unknown) {
      showToast(getApiErrorMessage(err, t('metadataReviews.toast.actionFailed')), 'error');
    } finally {
      setActing(false);
    }
  };

  const totalPages = Math.max(1, Math.ceil(total / limit));

  return (
    <div className={embedded ? '' : 'min-h-screen bg-komgaDark p-6 lg:p-10'}>
      {!embedded && (
      <div className="mb-8 flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <div className="inline-flex items-center gap-2 rounded-full border border-cyan-400/20 bg-cyan-400/10 px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-cyan-200">
            <GitCompareArrows className="h-3.5 w-3.5" />
            {t('metadataReviews.badge')}
          </div>
          <h1 className="mt-4 text-3xl font-bold tracking-tight text-white">{t('metadataReviews.title')}</h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-gray-400">{t('metadataReviews.description')}</p>
        </div>
        <div className="grid grid-cols-2 gap-3 sm:flex sm:items-center">
          <div className="rounded-2xl border border-white/10 bg-gray-950/60 px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-gray-500">{t('metadataReviews.metric.pending')}</p>
            <p className="mt-1 text-2xl font-semibold text-white">{total}</p>
          </div>
          <div className="rounded-2xl border border-white/10 bg-gray-950/60 px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-gray-500">{t('metadataReviews.metric.selected')}</p>
            <p className="mt-1 text-2xl font-semibold text-white">{selectedIds.length}</p>
          </div>
        </div>
      </div>
      )}

      <form onSubmit={submitSearch} className="mb-6 grid gap-3 rounded-2xl border border-white/10 bg-komgaSurface/70 p-4 backdrop-blur md:grid-cols-[1fr_180px_180px_auto]">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t('metadataReviews.searchPlaceholder')}
            className="w-full rounded-xl border border-white/10 bg-gray-950 px-10 py-2.5 text-sm text-white outline-none focus:border-komgaPrimary"
          />
        </div>
        <select value={libraryId} onChange={(event) => { setLibraryId(event.target.value); setPage(1); }} className="rounded-xl border border-white/10 bg-gray-950 px-3 py-2.5 text-sm text-white outline-none focus:border-komgaPrimary">
          <option value="0">{t('metadataReviews.allLibraries')}</option>
          {(libraries || []).map((library) => (
            <option key={library.id} value={library.id}>{library.name}</option>
          ))}
        </select>
        <select value={provider} onChange={(event) => { setProvider(event.target.value); setPage(1); }} className="rounded-xl border border-white/10 bg-gray-950 px-3 py-2.5 text-sm text-white outline-none focus:border-komgaPrimary">
          <option value="">{t('metadataReviews.allProviders')}</option>
          {providers.map((item) => (
            <option key={item} value={item}>{item}</option>
          ))}
          {provider && !providers.includes(provider) && <option value={provider}>{provider}</option>}
        </select>
        <button type="submit" className="inline-flex items-center justify-center gap-2 rounded-xl bg-komgaPrimary px-4 py-2.5 text-sm font-semibold text-white shadow-lg shadow-komgaPrimary/20 hover:bg-komgaPrimaryHover">
          <Filter className="h-4 w-4" />
          {t('common.search')}
        </button>
      </form>

      <div className="mb-5 flex flex-col gap-3 rounded-2xl border border-white/10 bg-gray-950/50 p-4 md:flex-row md:items-center md:justify-between">
        <div className="flex flex-wrap items-center gap-3">
          <button onClick={toggleCurrentPage} disabled={items.length === 0} className="rounded-xl border border-white/10 bg-white/5 px-4 py-2 text-sm text-gray-200 hover:bg-white/10 disabled:opacity-40">
            {currentPageSelected ? t('metadataReviews.unselectPage') : t('metadataReviews.selectPage')}
          </button>
          <select value={mode} onChange={(event) => setMode(event.target.value as BulkMode)} className="rounded-xl border border-white/10 bg-gray-950 px-3 py-2 text-sm text-white outline-none focus:border-komgaPrimary">
            <option value="fill_empty">{t('metadataReviews.mode.fillEmpty')}</option>
            <option value="all">{t('metadataReviews.mode.all')}</option>
          </select>
          <span className="text-xs text-gray-500">{t('metadataReviews.modeHint')}</span>
        </div>
        <div className="flex flex-wrap gap-2">
          <button onClick={() => runBulkAction('reject')} disabled={acting || selectedIds.length === 0} className="inline-flex items-center justify-center gap-2 rounded-xl border border-red-400/20 bg-red-500/10 px-4 py-2 text-sm font-medium text-red-200 hover:bg-red-500/15 disabled:opacity-40">
            {acting ? <Loader2 className="h-4 w-4 animate-spin" /> : <XCircle className="h-4 w-4" />}
            {t('metadataReviews.bulkReject')}
          </button>
          <button onClick={() => runBulkAction('apply')} disabled={acting || selectedIds.length === 0} className="inline-flex items-center justify-center gap-2 rounded-xl bg-komgaPrimary px-4 py-2 text-sm font-semibold text-white hover:bg-komgaPrimaryHover disabled:opacity-40">
            {acting ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
            {t('metadataReviews.bulkApply')}
          </button>
        </div>
      </div>

      {loading ? (
        <div className="flex min-h-[320px] items-center justify-center rounded-2xl border border-white/10 bg-gray-950/40 text-gray-400">
          <Loader2 className="mr-2 h-5 w-5 animate-spin text-komgaPrimary" />
          {t('common.loading')}
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-2xl border border-dashed border-white/10 bg-gray-950/40 p-12 text-center text-gray-500">
          {t('metadataReviews.empty')}
        </div>
      ) : (
        <div className="space-y-4">
          {items.map((item) => {
            const checked = selectedSet.has(item.id);
            return (
              <article key={item.id} className={`rounded-2xl border p-4 transition-colors ${checked ? 'border-komgaPrimary/40 bg-komgaPrimary/10' : 'border-white/10 bg-komgaSurface/70'}`}>
                <div className="flex flex-col gap-4 lg:flex-row lg:items-start">
                  <label className="flex cursor-pointer items-center gap-3">
                    <input type="checkbox" checked={checked} onChange={() => toggleSelected(item.id)} className="h-4 w-4 rounded border-gray-700 bg-gray-950 text-komgaPrimary focus:ring-komgaPrimary" />
                    <div className="h-24 w-16 overflow-hidden rounded-lg border border-white/10 bg-gray-900">
                      {item.cover_book_id > 0 ? (
                        <img src={`/api/covers/${item.cover_book_id}`} alt="" className="h-full w-full object-cover" loading="lazy" />
                      ) : (
                        <div className="flex h-full w-full items-center justify-center text-xs text-gray-600">?</div>
                      )}
                    </div>
                  </label>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                      <div className="min-w-0">
                        <Link to={`/series/${item.series_id}`} className="text-lg font-semibold text-white hover:text-komgaPrimary">
                          {displaySeriesTitle(item)}
                        </Link>
                        {item.series_title && item.series_title !== item.series_name && (
                          <p className="mt-0.5 truncate text-sm text-gray-500">{item.series_name}</p>
                        )}
                        <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-gray-400">
                          <span className="rounded-lg border border-white/10 bg-white/5 px-2 py-1">{item.library_name}</span>
                          <span className="rounded-lg border border-cyan-400/20 bg-cyan-400/10 px-2 py-1 text-cyan-200">{item.provider}</span>
                          <span>{formatDateTime(item.created_at)}</span>
                          <span>{t('metadataReviews.confidence', { value: percent(item.confidence) })}</span>
                          <span>{t('metadataReviews.fieldCount', { count: item.field_count })}</span>
                          {item.locked_field_count > 0 && (
                            <span className="inline-flex items-center gap-1 rounded-lg border border-amber-400/20 bg-amber-400/10 px-2 py-1 text-amber-200">
                              <ShieldCheck className="h-3 w-3" />
                              {t('metadataReviews.lockedCount', { count: item.locked_field_count })}
                            </span>
                          )}
                        </div>
                        {item.source_url && (
                          <a href={item.source_url} target="_blank" rel="noreferrer" className="mt-2 inline-flex max-w-full items-center gap-1 truncate text-xs text-cyan-300 hover:text-cyan-200">
                            <ExternalLink className="h-3.5 w-3.5 shrink-0" />
                            <span className="truncate">{item.source_url}</span>
                          </a>
                        )}
                      </div>
                    </div>
                    <div className="mt-4 grid gap-3 xl:grid-cols-2">
                      {item.fields.map((field) => (
                        <div key={field.name} className="rounded-xl border border-white/10 bg-gray-950/70 p-3">
                          <div className="mb-2 flex items-center justify-between gap-2">
                            <span className="text-sm font-semibold text-white">{field.label}</span>
                            {field.locked && <span className="rounded-full border border-amber-400/20 bg-amber-400/10 px-2 py-1 text-[11px] text-amber-200">{t('metadataReviews.locked')}</span>}
                          </div>
                          <div className="grid gap-2 sm:grid-cols-2">
                            <div className="min-w-0 rounded-lg border border-white/10 bg-black/20 px-3 py-2 text-xs text-gray-400 whitespace-pre-wrap break-words">{field.current || t('common.none')}</div>
                            <div className="min-w-0 rounded-lg border border-cyan-400/15 bg-cyan-400/5 px-3 py-2 text-xs text-gray-100 whitespace-pre-wrap break-words">{field.proposed || t('common.none')}</div>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </article>
            );
          })}
        </div>
      )}

      <div className="mt-8 flex flex-col items-center justify-between gap-4 border-t border-white/10 pt-6 sm:flex-row">
        <p className="text-sm text-gray-500">{t('metadataReviews.pageSummary', { page, total: totalPages })}</p>
        <div className="flex gap-2">
          <button onClick={() => setPage((value) => Math.max(1, value - 1))} disabled={page <= 1} className="rounded-xl border border-white/10 bg-gray-950 px-4 py-2 text-sm text-gray-300 hover:bg-white/5 disabled:opacity-40">{t('home.pagination.prev')}</button>
          <button onClick={() => setPage((value) => Math.min(totalPages, value + 1))} disabled={page >= totalPages} className="rounded-xl border border-white/10 bg-gray-950 px-4 py-2 text-sm text-gray-300 hover:bg-white/5 disabled:opacity-40">{t('home.pagination.next')}</button>
        </div>
      </div>

      {!embedded && toastMsg && (
        <div className="fixed bottom-6 right-6 z-50 animate-in slide-in-from-bottom-5 fade-in duration-300">
          <div className={`flex items-center gap-3 rounded-lg border px-4 py-3 shadow-lg ${toastMsg.type === 'success' ? 'border-green-700 bg-green-900 text-green-100' : 'border-red-700 bg-red-900 text-red-100'}`}>
            <span className="text-sm font-medium">{toastMsg.text}</span>
            <button onClick={() => setToastMsg(null)} className="ml-2 text-white/50 hover:text-white">x</button>
          </div>
        </div>
      )}
    </div>
  );
}
