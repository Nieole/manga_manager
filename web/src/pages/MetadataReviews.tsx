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

interface MetadataReviewsProps {
  embedded?: boolean;
  onReviewChange?: () => void;
}

export default function MetadataReviews({ embedded, onReviewChange }: MetadataReviewsProps = {}) {
  const { t, formatDateTime } = useI18n();
  const { showToast } = useToast();

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
  const [activeReviewId, setActiveReviewId] = useState<number | null>(null);
  const [libraryId, setLibraryId] = useState('0');
  const [provider, setProvider] = useState('');
  const [query, setQuery] = useState('');
  const [mode, setMode] = useState<BulkMode>('fill_empty');
  const [page, setPage] = useState(1);

  const limit = 30;
  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds]);
  const currentPageSelected = items.length > 0 && items.every((item) => selectedSet.has(item.id));
  const providers = useMemo(() => Array.from(new Set(items.map((item) => item.provider).filter(Boolean))).sort(), [items]);
  const activeReview = useMemo(
    () => (activeReviewId == null ? null : items.find((item) => item.id === activeReviewId) || null),
    [activeReviewId, items],
  );


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
      setActiveReviewId((current) => {
        const next = res.data.items || [];
        if (current != null && next.some((item) => item.id === current)) return current;
        return next[0]?.id ?? null;
      });
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
      onReviewChange?.();
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

      <div className="mb-5 flex flex-col gap-3 rounded-2xl border border-white/10 bg-gray-950/45 p-4 md:flex-row md:items-center md:justify-between select-none">
        <div className="flex flex-wrap items-center gap-3">
          <button onClick={toggleCurrentPage} disabled={items.length === 0} className="rounded-xl border border-white/10 bg-white/5 px-4 py-2 text-sm text-gray-200 hover:bg-white/10 disabled:opacity-40 active:scale-95 transition-all font-medium">
            {currentPageSelected ? t('metadataReviews.unselectPage') : t('metadataReviews.selectPage')}
          </button>
          {selectedIds.length > 0 && (
            <span className="text-xs text-gray-500">
              已选定 <span className="text-komgaPrimary font-semibold">{selectedIds.length}</span> 项元数据
            </span>
          )}
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
        <div className="grid gap-4 lg:grid-cols-[minmax(300px,30%)_1fr]">
          {/* 左：高密度 inbox 列表 */}
          <div className="rounded-2xl border border-white/10 bg-gray-950/40 overflow-hidden">
            <ul className="max-h-[70vh] overflow-y-auto divide-y divide-white/5">
              {items.map((item) => {
                const checked = selectedSet.has(item.id);
                const isActive = activeReviewId === item.id;
                const changedFields = item.fields.filter((f) => f.current !== f.proposed).length;
                return (
                  <li key={item.id}>
                    <button
                      type="button"
                      onClick={() => setActiveReviewId(item.id)}
                      className={`flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors ${isActive ? 'bg-komgaPrimary/15 ring-1 ring-inset ring-komgaPrimary/40' : 'hover:bg-white/5'}`}
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onClick={(event) => event.stopPropagation()}
                        onChange={() => toggleSelected(item.id)}
                        className="h-4 w-4 shrink-0 rounded border-gray-700 bg-gray-950 text-komgaPrimary focus:ring-komgaPrimary"
                      />
                      <div className="h-12 w-9 shrink-0 overflow-hidden rounded border border-white/10 bg-gray-900">
                        {item.cover_book_id > 0 ? (
                          <img src={`/api/covers/${item.cover_book_id}`} alt="" className="h-full w-full object-cover" loading="lazy" />
                        ) : (
                          <div className="flex h-full w-full items-center justify-center text-[10px] text-gray-600">?</div>
                        )}
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className={`truncate text-sm font-medium ${isActive ? 'text-white' : 'text-gray-200'}`}>{displaySeriesTitle(item)}</p>
                        <div className="mt-0.5 flex items-center gap-2 text-[11px] text-gray-500">
                          <span className="truncate">{item.library_name}</span>
                          <span className="shrink-0 text-cyan-300/80">{item.provider}</span>
                        </div>
                        <div className="mt-1 flex items-center gap-1.5 text-[11px]">
                          <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-emerald-300">{t('metadataReviews.diffChanged', { count: changedFields })}</span>
                          {item.locked_field_count > 0 && (
                            <span className="inline-flex items-center gap-0.5 rounded bg-amber-500/15 px-1.5 py-0.5 text-amber-300">
                              <ShieldCheck className="h-3 w-3" />
                              {item.locked_field_count}
                            </span>
                          )}
                        </div>
                      </div>
                    </button>
                  </li>
                );
              })}
            </ul>
          </div>

          {/* 右：当前选中条目详情 diff */}
          <div className="rounded-2xl border border-white/10 bg-komgaSurface/70 p-4">
            {activeReview ? (
              <div className="space-y-4">
                <div className="flex flex-col gap-3 border-b border-white/10 pb-4 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0 flex-1">
                    <Link to={`/series/${activeReview.series_id}`} className="text-lg font-semibold text-white hover:text-komgaPrimary">
                      {displaySeriesTitle(activeReview)}
                    </Link>
                    {activeReview.series_title && activeReview.series_title !== activeReview.series_name && (
                      <p className="mt-0.5 truncate text-sm text-gray-500">{activeReview.series_name}</p>
                    )}
                    <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-gray-400">
                      <span className="rounded-lg border border-white/10 bg-white/5 px-2 py-1">{activeReview.library_name}</span>
                      <span className="rounded-lg border border-cyan-400/20 bg-cyan-400/10 px-2 py-1 text-cyan-200">{activeReview.provider}</span>
                      <span>{formatDateTime(activeReview.created_at)}</span>
                      <span>{t('metadataReviews.confidence', { value: percent(activeReview.confidence) })}</span>
                      <span>{t('metadataReviews.fieldCount', { count: activeReview.field_count })}</span>
                      {activeReview.locked_field_count > 0 && (
                        <span className="inline-flex items-center gap-1 rounded-lg border border-amber-400/20 bg-amber-400/10 px-2 py-1 text-amber-500">
                          <ShieldCheck className="h-3 w-3" />
                          {t('metadataReviews.lockedCount', { count: activeReview.locked_field_count })}
                        </span>
                      )}
                    </div>
                    {activeReview.source_url && (
                      <a href={activeReview.source_url} target="_blank" rel="noreferrer" className="mt-2 inline-flex max-w-full items-center gap-1 truncate text-xs text-cyan-300 hover:text-cyan-200">
                        <ExternalLink className="h-3.5 w-3.5 shrink-0" />
                        <span className="truncate">{activeReview.source_url}</span>
                      </a>
                    )}
                  </div>
                  <label className="flex shrink-0 items-center gap-2 text-xs text-gray-400">
                    <input
                      type="checkbox"
                      checked={selectedSet.has(activeReview.id)}
                      onChange={() => toggleSelected(activeReview.id)}
                      className="h-4 w-4 rounded border-gray-700 bg-gray-950 text-komgaPrimary focus:ring-komgaPrimary"
                    />
                    {t('metadataReviews.selectThis')}
                  </label>
                </div>
                <div className="grid gap-3 xl:grid-cols-2">
                  {activeReview.fields.map((field) => (
                    <div key={field.name} className="rounded-xl border border-white/10 bg-gray-950/70 p-3">
                      <div className="mb-2 flex items-center justify-between gap-2">
                        <span className="text-sm font-semibold text-white">{field.label}</span>
                        {field.locked && <span className="rounded-full border border-amber-400/20 bg-amber-400/10 px-2 py-1 text-[11px] text-amber-500">{t('metadataReviews.locked')}</span>}
                      </div>
                      <div className="grid gap-2 sm:grid-cols-2">
                        <div className="min-w-0 rounded-lg border border-red-500/10 bg-red-500/[0.01] px-3 py-2 text-xs text-gray-400/80 whitespace-pre-wrap break-words">{field.current || t('common.none')}</div>
                        <div className={`min-w-0 rounded-lg border px-3 py-2 text-xs whitespace-pre-wrap break-words ${field.current !== field.proposed ? 'border-emerald-500/30 bg-emerald-500/[0.04] text-emerald-500 font-medium ring-1 ring-emerald-500/10' : 'border-white/5 bg-white/[0.01] text-gray-300'}`}>{field.proposed || t('common.none')}</div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            ) : (
              <div className="flex min-h-[320px] items-center justify-center text-sm text-gray-500">
                {t('metadataReviews.detail.empty')}
              </div>
            )}
          </div>
        </div>
      )}

      <div className="mt-8 flex flex-col items-center justify-between gap-4 border-t border-white/10 pt-6 sm:flex-row">
        <p className="text-sm text-gray-500">{t('metadataReviews.pageSummary', { page, total: totalPages })}</p>
        <div className="flex gap-2">
          <button onClick={() => setPage((value) => Math.max(1, value - 1))} disabled={page <= 1} className="rounded-xl border border-white/10 bg-gray-950 px-4 py-2 text-sm text-gray-300 hover:bg-white/5 disabled:opacity-40">{t('home.pagination.prev')}</button>
          <button onClick={() => setPage((value) => Math.min(totalPages, value + 1))} disabled={page >= totalPages} className="rounded-xl border border-white/10 bg-gray-950 px-4 py-2 text-sm text-gray-300 hover:bg-white/5 disabled:opacity-40">{t('home.pagination.next')}</button>
        </div>
      </div>

      {/* 底部浮动控制 Dock (选定项数 > 0 时呼出) */}
      {selectedIds.length > 0 && (
        <div className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50 bg-gray-900/90 backdrop-blur-xl border border-white/10 rounded-2xl px-6 py-4 shadow-[0_20px_50px_rgba(0,0,0,0.6)] flex flex-col sm:flex-row items-center gap-4 animate-in slide-in-from-bottom duration-300 select-none">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold text-white">
              已选定 <span className="text-komgaPrimary font-bold text-base">{selectedIds.length}</span> 项元数据建议
            </span>
          </div>
          <div className="flex items-center gap-3">
            <select value={mode} onChange={(event) => setMode(event.target.value as BulkMode)} className="rounded-xl border border-white/10 bg-black px-3 py-1.5 text-xs text-white focus:outline-none focus:ring-1 focus:ring-komgaPrimary">
              <option value="fill_empty">{t('metadataReviews.mode.fillEmpty')}</option>
              <option value="all">{t('metadataReviews.mode.all')}</option>
            </select>
            <button onClick={() => runBulkAction('reject')} disabled={acting} className="inline-flex items-center justify-center gap-1.5 rounded-xl border border-red-500/30 bg-red-950/40 px-4 py-2 text-xs font-semibold text-red-200 hover:bg-red-950 transition-all active:scale-95">
              {acting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <XCircle className="h-3.5 w-3.5" />}
              {t('metadataReviews.bulkReject')}
            </button>
            <button onClick={() => runBulkAction('apply')} disabled={acting} className="inline-flex items-center justify-center gap-1.5 rounded-xl bg-komgaPrimary px-5 py-2 text-xs font-semibold text-white hover:bg-komgaPrimaryHover shadow-lg shadow-komgaPrimary/20 transition-all active:scale-95">
              {acting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CheckCircle2 className="h-3.5 w-3.5" />}
              {t('metadataReviews.bulkApply')}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
