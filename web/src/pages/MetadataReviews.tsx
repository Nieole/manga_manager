/**
 * 业务说明：本文件是业务实现，属于项目源码的一部分，负责支撑漫画管理器在资料库、阅读器、扫描、元数据或系统设置中的具体业务能力。
 * 它与相邻模块共同组成前后端业务链路，修改时需要结合调用方理解数据流和用户可见行为。
 * 维护时应关注输入输出契约、错误处理、状态同步和与既有业务语义的一致性。
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { apiClient } from '../api/client';
import { getApiErrorMessage } from '../api/client';
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
type ReviewMark = 'apply' | 'reject';


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
  const [loadingMore, setLoadingMore] = useState(false);
  const [acting, setActing] = useState(false);
  const [markedActions, setMarkedActions] = useState<Record<number, ReviewMark>>({});
  const [activeReviewId, setActiveReviewId] = useState<number | null>(null);
  const [libraryId, setLibraryId] = useState('0');
  const [provider, setProvider] = useState('');
  const [query, setQuery] = useState('');
  const [appliedQuery, setAppliedQuery] = useState('');
  const [mode, setMode] = useState<BulkMode>('fill_empty');
  const listScrollRef = useRef<HTMLUListElement | null>(null);
  const loadMoreRef = useRef<HTMLLIElement | null>(null);
  const itemsLengthRef = useRef(0);
  const hasMoreRef = useRef(true);
  const requestBusyRef = useRef(false);
  const requestSeqRef = useRef(0);

  const limit = 30;
  const providers = useMemo(() => Array.from(new Set(items.map((item) => item.provider).filter(Boolean))).sort(), [items]);
  const activeReview = useMemo(
    () => (activeReviewId == null ? null : items.find((item) => item.id === activeReviewId) || null),
    [activeReviewId, items],
  );
  const markedEntries = useMemo(() => Object.entries(markedActions).map(([id, action]) => ({ id: Number(id), action })), [markedActions]);
  const applyIds = useMemo(() => markedEntries.filter((item) => item.action === 'apply').map((item) => item.id), [markedEntries]);
  const rejectIds = useMemo(() => markedEntries.filter((item) => item.action === 'reject').map((item) => item.id), [markedEntries]);
  const markedCount = markedEntries.length;
  const hasMore = items.length < total;

  useEffect(() => {
    itemsLengthRef.current = items.length;
    hasMoreRef.current = hasMore;
  }, [hasMore, items.length]);

  const loadReviews = useCallback(async (reset = false) => {
    if (!reset && requestBusyRef.current) return [];
    if (reset) {
      setLoading(true);
    } else {
      if (!hasMoreRef.current) return [];
      setLoadingMore(true);
    }
    const requestSeq = requestSeqRef.current + 1;
    requestSeqRef.current = requestSeq;
    requestBusyRef.current = true;
    try {
      const offset = reset ? 0 : itemsLengthRef.current;
      const res = await apiClient.get<MetadataReviewInboxResponse>('/api/metadata/reviews', {
        params: {
          library_id: libraryId,
          provider,
          q: appliedQuery,
          limit,
          offset,
        },
      });
      const nextItems = res.data.items || [];
      if (requestSeq !== requestSeqRef.current) return [];
      setItems((current) => reset ? nextItems : [...current, ...nextItems.filter((item) => !current.some((existing) => existing.id === item.id))]);
      setTotal(res.data.total || 0);
      setActiveReviewId((current) => {
        if (!reset && current != null) return current;
        if (current != null && nextItems.some((item) => item.id === current)) return current;
        return nextItems[0]?.id ?? null;
      });
      return nextItems;
    } catch (err: unknown) {
      showToast(getApiErrorMessage(err, t('metadataReviews.toast.loadFailed')), 'error');
      if (reset) {
        setItems([]);
        setTotal(0);
      }
      return [];
    } finally {
      if (requestSeq === requestSeqRef.current) {
        requestBusyRef.current = false;
        setLoading(false);
        setLoadingMore(false);
      }
    }
  }, [appliedQuery, libraryId, provider, showToast, t]);

  useEffect(() => {
    setMarkedActions({});
    void loadReviews(true);
  }, [libraryId, provider, appliedQuery, refreshTrigger, loadReviews]);

  useEffect(() => {
    const node = loadMoreRef.current;
    const root = listScrollRef.current;
    if (!node || !root) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        void loadReviews(false);
      }
    }, { root, rootMargin: '160px 0px' });
    observer.observe(node);
    return () => observer.disconnect();
  }, [items.length, loadReviews, loading]);

  const submitSearch = (event: React.FormEvent) => {
    event.preventDefault();
    setAppliedQuery(query.trim());
  };

  const focusNextReview = (id: number, nextMarks: Record<number, ReviewMark>) => {
    const index = items.findIndex((item) => item.id === id);
    if (index < 0) return;
    const nextItem = items.slice(index + 1).find((item) => !nextMarks[item.id]);
    if (nextItem) {
      setActiveReviewId(nextItem.id);
      return;
    }
    if (hasMoreRef.current) {
      void loadReviews(false).then((loadedItems) => {
        const loadedNext = loadedItems.find((item) => !nextMarks[item.id]);
        if (loadedNext) {
          setActiveReviewId(loadedNext.id);
        }
      });
    }
  };

  const markReview = (id: number, action: ReviewMark) => {
    const next = { ...markedActions };
    const isClearing = next[id] === action;
    if (isClearing) {
      delete next[id];
    } else {
      next[id] = action;
    }
    setMarkedActions(next);
    if (!isClearing) {
      focusNextReview(id, next);
    }
  };

  const clearMarks = () => setMarkedActions({});

  const postMetadataBulk = async (endpoint: string, ids: number[]) => {
    let done = 0;
    let skipped = 0;
    let failed = 0;
    for (let index = 0; index < ids.length; index += 100) {
      const batch = ids.slice(index, index + 100);
      const res = await apiClient.post(endpoint, { review_ids: batch, mode });
      done += (res.data.applied?.length || 0) + (res.data.rejected?.length || 0);
      skipped += res.data.skipped?.length || 0;
      failed += res.data.failed?.length || 0;
    }
    return { done, skipped, failed };
  };

  const runMarkedActions = async () => {
    if (markedCount === 0) return;
    setActing(true);
    try {
      const applied = applyIds.length > 0 ? await postMetadataBulk('/api/metadata/reviews/bulk-apply', applyIds) : { done: 0, skipped: 0, failed: 0 };
      const rejected = rejectIds.length > 0 ? await postMetadataBulk('/api/metadata/reviews/bulk-reject', rejectIds) : { done: 0, skipped: 0, failed: 0 };
      const failedCount = applied.failed + rejected.failed;
      const skippedCount = applied.skipped + rejected.skipped;
      showToast(t('reviewInbox.toast.markedApplied', {
        applied: applied.done,
        rejected: rejected.done,
        skipped: skippedCount,
        failed: failedCount,
      }), failedCount > 0 ? 'error' : 'success');
      setMarkedActions({});
      await loadReviews(true);
      onReviewChange?.();
    } catch (err: unknown) {
      showToast(getApiErrorMessage(err, t('metadataReviews.toast.actionFailed')), 'error');
    } finally {
      setActing(false);
    }
  };

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
            <p className="text-xs uppercase tracking-wide text-gray-500">{t('reviewInbox.metric.marked')}</p>
            <p className="mt-1 text-2xl font-semibold text-white">{markedCount}</p>
          </div>
        </div>
      </div>
      )}

      <form onSubmit={submitSearch} className="mb-6 grid gap-3 rounded-2xl border border-white/10 bg-komgaSurface/70 p-4 backdrop-blur-sm md:grid-cols-[1fr_180px_180px_auto]">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t('metadataReviews.searchPlaceholder')}
            className="w-full rounded-xl border border-white/10 bg-gray-950 px-10 py-2.5 text-sm text-white outline-hidden focus:border-komgaPrimary"
          />
        </div>
        <select value={libraryId} onChange={(event) => setLibraryId(event.target.value)} className="rounded-xl border border-white/10 bg-gray-950 px-3 py-2.5 text-sm text-white outline-hidden focus:border-komgaPrimary">
          <option value="0">{t('metadataReviews.allLibraries')}</option>
          {(libraries || []).map((library) => (
            <option key={library.id} value={library.id}>{library.name}</option>
          ))}
        </select>
        <select value={provider} onChange={(event) => setProvider(event.target.value)} className="rounded-xl border border-white/10 bg-gray-950 px-3 py-2.5 text-sm text-white outline-hidden focus:border-komgaPrimary">
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
          <span className="text-xs text-gray-500">{t('reviewInbox.loadedSummary', { loaded: items.length, total })}</span>
          {markedCount > 0 && (
            <span className="text-xs text-gray-500">
              {t('reviewInbox.markedSummary', { apply: applyIds.length, reject: rejectIds.length })}
            </span>
          )}
        </div>
        {markedCount > 0 && (
          <button onClick={clearMarks} className="self-start rounded-xl border border-white/10 bg-white/5 px-3 py-1.5 text-xs text-gray-300 hover:bg-white/10 md:self-auto">
            {t('reviewInbox.clearMarks')}
          </button>
        )}
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
            <ul ref={listScrollRef} className="max-h-[70vh] overflow-y-auto divide-y divide-white/5">
              {items.map((item) => {
                const mark = markedActions[item.id];
                const isActive = activeReviewId === item.id;
                const changedFields = item.fields.filter((f) => f.current !== f.proposed).length;
                return (
                  <li key={item.id}>
                    <button
                      type="button"
                      onClick={() => setActiveReviewId(item.id)}
                      className={`flex w-full items-center gap-3 border-l-2 px-3 py-2.5 text-left transition-colors ${mark === 'apply' ? 'border-emerald-400' : mark === 'reject' ? 'border-red-400' : 'border-transparent'} ${isActive ? 'bg-komgaPrimary/15 ring-1 ring-inset ring-komgaPrimary/40' : 'hover:bg-white/5'}`}
                    >
                      <span className="flex shrink-0 flex-col gap-1" onClick={(event) => event.stopPropagation()}>
                        <span
                          role="button"
                          tabIndex={0}
                          onClick={() => markReview(item.id, 'apply')}
                          className={`inline-flex h-7 w-7 items-center justify-center rounded-lg border transition-colors ${mark === 'apply' ? 'border-emerald-400/50 bg-emerald-500/20 text-emerald-200' : 'border-white/10 bg-white/5 text-gray-500 hover:text-emerald-200'}`}
                          title={t('reviewInbox.markApply')}
                        >
                          <CheckCircle2 className="h-3.5 w-3.5" />
                        </span>
                        <span
                          role="button"
                          tabIndex={0}
                          onClick={() => markReview(item.id, 'reject')}
                          className={`inline-flex h-7 w-7 items-center justify-center rounded-lg border transition-colors ${mark === 'reject' ? 'border-red-400/50 bg-red-500/20 text-red-200' : 'border-white/10 bg-white/5 text-gray-500 hover:text-red-200'}`}
                          title={t('reviewInbox.markReject')}
                        >
                          <XCircle className="h-3.5 w-3.5" />
                        </span>
                      </span>
                      <div className="h-12 w-9 shrink-0 overflow-hidden rounded-sm border border-white/10 bg-gray-900">
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
                          <span className="rounded-sm bg-emerald-500/15 px-1.5 py-0.5 text-emerald-300">{t('metadataReviews.diffChanged', { count: changedFields })}</span>
                          {item.locked_field_count > 0 && (
                            <span className="inline-flex items-center gap-0.5 rounded-sm bg-amber-500/15 px-1.5 py-0.5 text-amber-300">
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
              <li ref={loadMoreRef} className="flex min-h-12 items-center justify-center px-3 py-3 text-xs text-gray-500">
                {loadingMore ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin text-komgaPrimary" />
                    {t('reviewInbox.loadingMore')}
                  </>
                ) : hasMore ? (
                  t('reviewInbox.scrollForMore')
                ) : (
                  t('reviewInbox.allLoaded', { total })
                )}
              </li>
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
                  <div className="flex shrink-0 items-center gap-2">
                    <button
                      onClick={() => markReview(activeReview.id, 'reject')}
                      className={`inline-flex items-center gap-1.5 rounded-xl border px-3 py-2 text-xs font-medium transition-colors ${markedActions[activeReview.id] === 'reject' ? 'border-red-400/50 bg-red-500/20 text-red-100' : 'border-red-400/20 bg-red-500/10 text-red-200 hover:bg-red-500/15'}`}
                    >
                      <XCircle className="h-3.5 w-3.5" />
                      {t('reviewInbox.markReject')}
                    </button>
                    <button
                      onClick={() => markReview(activeReview.id, 'apply')}
                      className={`inline-flex items-center gap-1.5 rounded-xl px-3 py-2 text-xs font-semibold transition-colors ${markedActions[activeReview.id] === 'apply' ? 'bg-emerald-500 text-white' : 'bg-komgaPrimary text-white hover:bg-komgaPrimaryHover'}`}
                    >
                      <CheckCircle2 className="h-3.5 w-3.5" />
                      {t('reviewInbox.markApply')}
                    </button>
                  </div>
                </div>
                <div className="grid gap-3 xl:grid-cols-2">
                  {activeReview.fields.map((field) => (
                    <div key={field.name} className="rounded-xl border border-white/10 bg-gray-950/70 p-3">
                      <div className="mb-2 flex items-center justify-between gap-2">
                        <span className="text-sm font-semibold text-white">{field.label}</span>
                        {field.locked && <span className="rounded-full border border-amber-400/20 bg-amber-400/10 px-2 py-1 text-[11px] text-amber-500">{t('metadataReviews.locked')}</span>}
                      </div>
                      <div className="grid gap-2 sm:grid-cols-2">
                        <div className="min-w-0 rounded-lg border border-red-500/10 bg-red-500/1 px-3 py-2 text-xs text-gray-400/80 whitespace-pre-wrap wrap-break-word">{field.current || t('common.none')}</div>
                        <div className={`min-w-0 rounded-lg border px-3 py-2 text-xs whitespace-pre-wrap wrap-break-word ${field.current !== field.proposed ? 'border-emerald-500/30 bg-emerald-500/4 text-emerald-500 font-medium ring-1 ring-emerald-500/10' : 'border-white/5 bg-white/1 text-gray-300'}`}>{field.proposed || t('common.none')}</div>
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

      {/* 底部浮动控制 Dock (选定项数 > 0 时呼出) */}
      {markedCount > 0 && (
        <div className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50 bg-gray-900/90 backdrop-blur-xl border border-white/10 rounded-2xl px-6 py-4 shadow-[0_20px_50px_rgba(0,0,0,0.6)] flex flex-col sm:flex-row items-center gap-4 animate-in slide-in-from-bottom duration-300 select-none">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold text-white">
              {t('reviewInbox.markedSummary', { apply: applyIds.length, reject: rejectIds.length })}
            </span>
          </div>
          <div className="flex items-center gap-3">
            <select value={mode} onChange={(event) => setMode(event.target.value as BulkMode)} className="rounded-xl border border-white/10 bg-black px-3 py-1.5 text-xs text-white focus:outline-hidden focus:ring-1 focus:ring-komgaPrimary">
              <option value="fill_empty">{t('metadataReviews.mode.fillEmpty')}</option>
              <option value="all">{t('metadataReviews.mode.all')}</option>
            </select>
            <button onClick={clearMarks} disabled={acting} className="inline-flex items-center justify-center gap-1.5 rounded-xl border border-white/10 bg-white/5 px-4 py-2 text-xs font-semibold text-gray-200 hover:bg-white/10 transition-all active:scale-95">
              {t('reviewInbox.clearMarks')}
            </button>
            <button onClick={runMarkedActions} disabled={acting} className="inline-flex items-center justify-center gap-1.5 rounded-xl bg-komgaPrimary px-5 py-2 text-xs font-semibold text-white hover:bg-komgaPrimaryHover shadow-lg shadow-komgaPrimary/20 transition-all active:scale-95">
              {acting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CheckCircle2 className="h-3.5 w-3.5" />}
              {t('reviewInbox.applyMarked')}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
