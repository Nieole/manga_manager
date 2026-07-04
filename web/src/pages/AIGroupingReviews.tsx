/**
 * 业务说明：本文件是业务实现，属于项目源码的一部分，负责支撑漫画管理器在资料库、阅读器、扫描、元数据或系统设置中的具体业务能力。
 * 它与相邻模块共同组成前后端业务链路，修改时需要结合调用方理解数据流和用户可见行为。
 * 维护时应关注输入输出契约、错误处理、状态同步和与既有业务语义的一致性。
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { apiClient } from '../api/client';
import { getApiErrorMessage } from '../api/client';
import { Link, useOutletContext } from 'react-router-dom';
import { CheckCircle2, Filter, Layers3, Loader2, Pencil, Save, Sparkles, X, XCircle } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';
import { useToast } from '../components/ToastProvider';

interface LibraryOption {
  id: number | string;
  name: string;
}

interface AIGroupingReviewSeries {
  id: number;
  name: string;
  title: string;
}

interface AIGroupingReviewCollection {
  id: number;
  review_id: number;
  name: string;
  description: string;
  series_ids: number[];
  series: AIGroupingReviewSeries[];
  series_count: number;
  status: string;
  created_collection_id?: number;
}

interface AIGroupingReview {
  id: number;
  library_id: number;
  library_name: string;
  provider: string;
  status: string;
  summary: string;
  candidate_count: number;
  collection_count: number;
  created_at: string;
  updated_at: string;
  applied_at?: string;
  rejected_at?: string;
  collections: AIGroupingReviewCollection[];
}

interface AIGroupingReviewsResponse {
  items: AIGroupingReview[];
  total: number;
  limit: number;
  offset: number;
}

interface CollectionDraft {
  name: string;
  description: string;
  seriesIds: number[];
}

type ReviewMark = 'apply' | 'reject';


function statusClass(status: string) {
  switch (status) {
    case 'applied':
      return 'border-green-400/20 bg-green-500/10 text-green-200';
    case 'rejected':
      return 'border-red-400/20 bg-red-500/10 text-red-200';
    default:
      return 'border-amber-400/20 bg-amber-400/10 text-amber-100';
  }
}

function displaySeriesName(series: AIGroupingReviewSeries) {
  return series.title || series.name;
}

interface AIGroupingReviewsProps {
  embedded?: boolean;
  onReviewChange?: () => void;
}

export default function AIGroupingReviews({ embedded, onReviewChange }: AIGroupingReviewsProps = {}) {
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
  const [items, setItems] = useState<AIGroupingReview[]>([]);
  const [total, setTotal] = useState(0);
  const [libraryId, setLibraryId] = useState('0');
  const [status, setStatus] = useState('pending');
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [actingKey, setActingKey] = useState<string | null>(null);
  const [editingDrafts, setEditingDrafts] = useState<Record<number, CollectionDraft>>({});
  const [activeReviewId, setActiveReviewId] = useState<number | null>(null);
  const [markedActions, setMarkedActions] = useState<Record<number, ReviewMark>>({});
  const listScrollRef = useRef<HTMLUListElement | null>(null);
  const loadMoreRef = useRef<HTMLLIElement | null>(null);
  const itemsLengthRef = useRef(0);
  const hasMoreRef = useRef(true);
  const requestBusyRef = useRef(false);
  const requestSeqRef = useRef(0);

  const limit = 20;
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
      const res = await apiClient.get<AIGroupingReviewsResponse>('/api/ai-grouping/reviews', {
        params: {
          library_id: libraryId,
          status,
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
      showToast(getApiErrorMessage(err, t('aiGroupingReviews.toast.loadFailed')), 'error');
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
  }, [libraryId, showToast, status, t]);

  useEffect(() => {
    setMarkedActions({});
    void loadReviews(true);
  }, [libraryId, status, refreshTrigger, loadReviews]);

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

  const focusNextReview = (id: number, nextMarks: Record<number, ReviewMark>) => {
    const index = items.findIndex((item) => item.id === id);
    if (index < 0) return;
    const nextItem = items.slice(index + 1).find((item) => item.status === 'pending' && !nextMarks[item.id]);
    if (nextItem) {
      setActiveReviewId(nextItem.id);
      return;
    }
    if (hasMoreRef.current) {
      void loadReviews(false).then((loadedItems) => {
        const loadedNext = loadedItems.find((item) => item.status === 'pending' && !nextMarks[item.id]);
        if (loadedNext) {
          setActiveReviewId(loadedNext.id);
        }
      });
    }
  };

  const markReview = (review: AIGroupingReview, action: ReviewMark) => {
    if (review.status !== 'pending') return;
    const next = { ...markedActions };
    const isClearing = next[review.id] === action;
    if (isClearing) {
      delete next[review.id];
    } else {
      next[review.id] = action;
    }
    setMarkedActions(next);
    if (!isClearing) {
      focusNextReview(review.id, next);
    }
  };

  const clearMarks = () => setMarkedActions({});

  const runMarkedActions = async () => {
    if (markedCount === 0) return;
    setActingKey('marked');
    let applied = 0;
    let rejected = 0;
    let failed = 0;
    try {
      for (const id of applyIds) {
        try {
          await apiClient.post(`/api/ai-grouping/reviews/${id}/apply`);
          applied += 1;
        } catch {
          failed += 1;
        }
      }
      for (const id of rejectIds) {
        try {
          await apiClient.post(`/api/ai-grouping/reviews/${id}/reject`);
          rejected += 1;
        } catch {
          failed += 1;
        }
      }
      showToast(t('reviewInbox.toast.markedApplied', {
        applied,
        rejected,
        skipped: 0,
        failed,
      }), failed > 0 ? 'error' : 'success');
      setMarkedActions({});
      await loadReviews(true);
      onReviewChange?.();
    } finally {
      setActingKey(null);
    }
  };

  const runCollectionAction = async (review: AIGroupingReview, collection: AIGroupingReviewCollection, action: 'apply' | 'reject') => {
    if (review.status !== 'pending' || collection.status !== 'pending') return;
    setActingKey(`collection:${collection.id}:${action}`);
    try {
      const res = await apiClient.post(`/api/ai-grouping/reviews/${review.id}/collections/${collection.id}/${action}`);
      if (action === 'apply') {
        showToast(t('aiGroupingReviews.toast.collectionApplied', { id: res.data.created_collection_id || '' }));
      } else {
        showToast(t('aiGroupingReviews.toast.collectionRejected'));
      }
      setEditingDrafts((current) => {
        const next = { ...current };
        delete next[collection.id];
        return next;
      });
      await loadReviews(true);
      onReviewChange?.();
    } catch (err: unknown) {
      showToast(getApiErrorMessage(err, t('aiGroupingReviews.toast.actionFailed')), 'error');
    } finally {
      setActingKey(null);
    }
  };

  const startEdit = (collection: AIGroupingReviewCollection) => {
    setEditingDrafts((current) => ({
      ...current,
      [collection.id]: {
        name: collection.name,
        description: collection.description,
        seriesIds: collection.series_ids,
      },
    }));
  };

  const cancelEdit = (collectionID: number) => {
    setEditingDrafts((current) => {
      const next = { ...current };
      delete next[collectionID];
      return next;
    });
  };

  const updateDraft = (collectionID: number, patch: Partial<CollectionDraft>) => {
    setEditingDrafts((current) => ({
      ...current,
      [collectionID]: {
        ...current[collectionID],
        ...patch,
      },
    }));
  };

  const saveDraft = async (review: AIGroupingReview, collection: AIGroupingReviewCollection) => {
    const draft = editingDrafts[collection.id];
    if (!draft || !draft.name.trim() || draft.seriesIds.length === 0) return;
    setActingKey(`collection:${collection.id}:save`);
    try {
      await apiClient.put(`/api/ai-grouping/reviews/${review.id}/collections/${collection.id}`, {
        name: draft.name,
        description: draft.description,
        series_ids: draft.seriesIds,
      });
      showToast(t('aiGroupingReviews.toast.collectionUpdated'));
      cancelEdit(collection.id);
      await loadReviews(true);
    } catch (err: unknown) {
      showToast(getApiErrorMessage(err, t('aiGroupingReviews.toast.actionFailed')), 'error');
    } finally {
      setActingKey(null);
    }
  };

  const toggleDraftSeries = (collectionID: number, seriesID: number) => {
    const draft = editingDrafts[collectionID];
    if (!draft) return;
    const exists = draft.seriesIds.includes(seriesID);
    updateDraft(collectionID, {
      seriesIds: exists ? draft.seriesIds.filter((id) => id !== seriesID) : [...draft.seriesIds, seriesID],
    });
  };

  return (
    <div className={embedded ? '' : 'min-h-screen bg-komgaDark p-6 lg:p-10'}>
      {!embedded && (
      <div className="mb-8 flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <div className="inline-flex items-center gap-2 rounded-full border border-amber-400/20 bg-amber-400/10 px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-amber-100">
            <Sparkles className="h-3.5 w-3.5" />
            {t('aiGroupingReviews.badge')}
          </div>
          <h1 className="mt-4 text-3xl font-bold tracking-tight text-white">{t('aiGroupingReviews.title')}</h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-gray-400">{t('aiGroupingReviews.description')}</p>
        </div>
        <div className="grid grid-cols-2 gap-3 sm:flex sm:items-center">
          <div className="rounded-2xl border border-white/10 bg-gray-950/60 px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-gray-500">{t('aiGroupingReviews.metric.total')}</p>
            <p className="mt-1 text-2xl font-semibold text-white">{total}</p>
          </div>
          <div className="rounded-2xl border border-white/10 bg-gray-950/60 px-4 py-3">
            <p className="text-xs uppercase tracking-wide text-gray-500">{t('reviewInbox.metric.marked')}</p>
            <p className="mt-1 text-2xl font-semibold text-white">{markedCount}</p>
          </div>
        </div>
      </div>
      )}

      <div className="mb-6 grid gap-3 rounded-2xl border border-white/10 bg-komgaSurface/70 p-4 backdrop-blur-sm md:grid-cols-[1fr_180px_auto]">
        <select value={libraryId} onChange={(event) => setLibraryId(event.target.value)} className="rounded-xl border border-white/10 bg-gray-950 px-3 py-2.5 text-sm text-white outline-hidden focus:border-komgaPrimary">
          <option value="0">{t('aiGroupingReviews.allLibraries')}</option>
          {(libraries || []).map((library) => (
            <option key={library.id} value={library.id}>{library.name}</option>
          ))}
        </select>
        <select value={status} onChange={(event) => setStatus(event.target.value)} className="rounded-xl border border-white/10 bg-gray-950 px-3 py-2.5 text-sm text-white outline-hidden focus:border-komgaPrimary">
          <option value="pending">{t('aiGroupingReviews.status.pending')}</option>
          <option value="applied">{t('aiGroupingReviews.status.applied')}</option>
          <option value="rejected">{t('aiGroupingReviews.status.rejected')}</option>
          <option value="">{t('aiGroupingReviews.status.all')}</option>
        </select>
        <button onClick={() => loadReviews(true)} className="inline-flex items-center justify-center gap-2 rounded-xl bg-komgaPrimary px-4 py-2.5 text-sm font-semibold text-white shadow-lg shadow-komgaPrimary/20 hover:bg-komgaPrimaryHover">
          <Filter className="h-4 w-4" />
          {t('common.search')}
        </button>
      </div>

      <div className="mb-5 flex flex-col gap-3 rounded-2xl border border-white/10 bg-gray-950/45 p-4 md:flex-row md:items-center md:justify-between select-none">
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-xs text-gray-500">{t('reviewInbox.loadedSummary', { loaded: items.length, total })}</span>
          {markedCount > 0 && (
            <span className="text-xs text-gray-500">{t('reviewInbox.markedSummary', { apply: applyIds.length, reject: rejectIds.length })}</span>
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
          {t('aiGroupingReviews.empty')}
        </div>
      ) : (
        <div className="grid gap-4 lg:grid-cols-[minmax(280px,30%)_1fr]">
          {/* 左：高密度 review 列表 */}
          <div className="rounded-2xl border border-white/10 bg-gray-950/40 overflow-hidden">
            <ul ref={listScrollRef} className="max-h-[70vh] overflow-y-auto divide-y divide-white/5">
              {items.map((review) => {
                const isActive = activeReviewId === review.id;
                const mark = markedActions[review.id];
                const markable = review.status === 'pending';
                return (
                  <li key={review.id}>
                    <button
                      type="button"
                      onClick={() => setActiveReviewId(review.id)}
                      className={`flex w-full items-start gap-3 border-l-2 px-3 py-2.5 text-left transition-colors ${mark === 'apply' ? 'border-emerald-400' : mark === 'reject' ? 'border-red-400' : 'border-transparent'} ${isActive ? 'bg-komgaPrimary/15 ring-1 ring-inset ring-komgaPrimary/40' : 'hover:bg-white/5'}`}
                    >
                      <span className="flex shrink-0 flex-col gap-1" onClick={(event) => event.stopPropagation()}>
                        <span
                          role="button"
                          tabIndex={0}
                          onClick={() => markReview(review, 'apply')}
                          className={`inline-flex h-7 w-7 items-center justify-center rounded-lg border transition-colors ${mark === 'apply' ? 'border-emerald-400/50 bg-emerald-500/20 text-emerald-200' : markable ? 'border-white/10 bg-white/5 text-gray-500 hover:text-emerald-200' : 'border-white/5 bg-white/3 text-gray-700'}`}
                          title={t('reviewInbox.markApply')}
                        >
                          <CheckCircle2 className="h-3.5 w-3.5" />
                        </span>
                        <span
                          role="button"
                          tabIndex={0}
                          onClick={() => markReview(review, 'reject')}
                          className={`inline-flex h-7 w-7 items-center justify-center rounded-lg border transition-colors ${mark === 'reject' ? 'border-red-400/50 bg-red-500/20 text-red-200' : markable ? 'border-white/10 bg-white/5 text-gray-500 hover:text-red-200' : 'border-white/5 bg-white/3 text-gray-700'}`}
                          title={t('reviewInbox.markReject')}
                        >
                          <XCircle className="h-3.5 w-3.5" />
                        </span>
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center justify-between gap-2">
                          <p className={`truncate text-sm font-medium ${isActive ? 'text-white' : 'text-gray-200'}`}>
                            {t('aiGroupingReviews.reviewTitle', { id: review.id })}
                          </p>
                          <span className={`shrink-0 rounded-full border px-1.5 py-0.5 text-[10px] font-semibold ${statusClass(review.status)}`}>
                            {t(`aiGroupingReviews.status.${review.status}`)}
                          </span>
                        </div>
                        <div className="mt-0.5 flex items-center gap-2 text-[11px] text-gray-500">
                          <span className="truncate">{review.library_name}</span>
                          <span className="shrink-0 text-cyan-300/80">{review.provider || '—'}</span>
                        </div>
                        <div className="mt-1 flex items-center gap-1.5 text-[11px]">
                          <span className="rounded-sm bg-amber-500/15 px-1.5 py-0.5 text-amber-300">{t('aiGroupingReviews.collectionsBadge', { count: review.collection_count })}</span>
                          <span className="rounded-sm bg-cyan-500/15 px-1.5 py-0.5 text-cyan-300">{t('aiGroupingReviews.candidatesBadge', { count: review.candidate_count })}</span>
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

          {/* 右：当前 review 的详情 */}
          <div className="rounded-2xl border border-white/10 bg-komgaSurface/70 p-4">
            {activeReview ? (
              <article>
                <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between border-b border-white/10 pb-4">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className={`rounded-full border px-2.5 py-1 text-xs font-semibold ${statusClass(activeReview.status)}`}>{t(`aiGroupingReviews.status.${activeReview.status}`)}</span>
                      <span className="rounded-lg border border-white/10 bg-white/5 px-2 py-1 text-xs text-gray-300">{activeReview.library_name}</span>
                      <span className="rounded-lg border border-cyan-400/20 bg-cyan-400/10 px-2 py-1 text-xs text-cyan-200">{activeReview.provider || t('common.none')}</span>
                      <span className="text-xs text-gray-500">{formatDateTime(activeReview.created_at)}</span>
                    </div>
                    <h2 className="mt-3 text-xl font-semibold text-white">{t('aiGroupingReviews.reviewTitle', { id: activeReview.id })}</h2>
                    <p className="mt-2 text-sm text-gray-400">{t('aiGroupingReviews.reviewSummary', { candidates: activeReview.candidate_count, collections: activeReview.collection_count })}</p>
                  </div>
                  {activeReview.status === 'pending' && (
                    <div className="flex flex-wrap gap-2">
                      <button onClick={() => markReview(activeReview, 'reject')} className={`inline-flex items-center justify-center gap-2 rounded-xl border px-4 py-2 text-sm font-medium disabled:opacity-40 ${markedActions[activeReview.id] === 'reject' ? 'border-red-400/50 bg-red-500/20 text-red-100' : 'border-red-400/20 bg-red-500/10 text-red-200 hover:bg-red-500/15'}`}>
                        <XCircle className="h-4 w-4" />
                        {t('reviewInbox.markReject')}
                      </button>
                      <button onClick={() => markReview(activeReview, 'apply')} className={`inline-flex items-center justify-center gap-2 rounded-xl px-4 py-2 text-sm font-semibold text-white disabled:opacity-40 ${markedActions[activeReview.id] === 'apply' ? 'bg-emerald-500' : 'bg-komgaPrimary hover:bg-komgaPrimaryHover'}`}>
                        <CheckCircle2 className="h-4 w-4" />
                        {t('reviewInbox.markApply')}
                      </button>
                    </div>
                  )}
                </div>

                <div className="mt-4 grid gap-3 xl:grid-cols-2">
                  {activeReview.collections.map((collection) => {
                    const draft = editingDrafts[collection.id];
                    const editable = activeReview.status === 'pending' && collection.status === 'pending';
                    return (
                      <section key={collection.id} className="rounded-xl border border-white/10 bg-gray-950/60 p-4">
                        <div className="flex items-start justify-between gap-3">
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                              <Layers3 className="h-4 w-4 shrink-0 text-amber-500" />
                              {draft ? (
                                <input value={draft.name} onChange={(event) => updateDraft(collection.id, { name: event.target.value })} className="min-w-0 flex-1 rounded-lg border border-white/10 bg-black/30 px-3 py-1.5 text-sm font-semibold text-white outline-hidden focus:border-komgaPrimary" />
                              ) : (
                                <h3 className="truncate text-base font-semibold text-white">{collection.name}</h3>
                              )}
                            </div>
                            {draft ? (
                              <textarea value={draft.description} onChange={(event) => updateDraft(collection.id, { description: event.target.value })} rows={2} className="mt-2 w-full rounded-lg border border-white/10 bg-black/30 px-3 py-2 text-sm text-gray-200 outline-hidden focus:border-komgaPrimary" />
                            ) : (
                              collection.description && <p className="mt-2 text-sm leading-6 text-gray-400">{collection.description}</p>
                            )}
                          </div>
                          <span className={`shrink-0 rounded-full border px-2 py-1 text-[11px] ${statusClass(collection.status)}`}>{t(`aiGroupingReviews.status.${collection.status}`)}</span>
                        </div>
                        <div className="mt-3 flex flex-wrap gap-2">
                          {collection.series.map((series) => {
                            if (draft) {
                              const selected = draft.seriesIds.includes(series.id);
                              return (
                                <button key={series.id} type="button" onClick={() => toggleDraftSeries(collection.id, series.id)} className={`max-w-full rounded-lg border px-2.5 py-1 text-xs transition-colors ${selected ? 'border-komgaPrimary/50 bg-komgaPrimary/15 text-white' : 'border-white/10 bg-white/5 text-gray-500'}`}>
                                  <span className="inline-block max-w-[220px] truncate align-bottom">{displaySeriesName(series)}</span>
                                </button>
                              );
                            }
                            return (
                              <Link key={series.id} to={`/series/${series.id}`} className="max-w-full rounded-lg border border-white/10 bg-white/5 px-2.5 py-1 text-xs text-gray-200 hover:border-komgaPrimary/50 hover:text-white">
                                <span className="inline-block max-w-[220px] truncate align-bottom">{displaySeriesName(series)}</span>
                              </Link>
                            );
                          })}
                          {collection.series.length === 0 && <span className="text-xs text-gray-500">{t('aiGroupingReviews.noSeries')}</span>}
                        </div>
                        {collection.created_collection_id && (
                          <p className="mt-3 text-xs text-green-200">{t('aiGroupingReviews.createdCollection', { id: collection.created_collection_id })}</p>
                        )}
                        {editable && (
                          <div className="mt-4 flex flex-wrap justify-end gap-2 border-t border-white/10 pt-3">
                            {draft ? (
                              <>
                                <button onClick={() => cancelEdit(collection.id)} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-xs text-gray-300 hover:bg-white/10">
                                  <X className="h-3.5 w-3.5" />
                                  {t('modal.cancel')}
                                </button>
                                <button onClick={() => saveDraft(activeReview, collection)} disabled={actingKey === `collection:${collection.id}:save` || !draft.name.trim() || draft.seriesIds.length === 0} className="inline-flex items-center gap-1.5 rounded-lg bg-komgaPrimary px-3 py-1.5 text-xs font-semibold text-white hover:bg-komgaPrimaryHover disabled:opacity-40">
                                  {actingKey === `collection:${collection.id}:save` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                                  {t('aiGroupingReviews.save')}
                                </button>
                              </>
                            ) : (
                              <>
                                <button onClick={() => startEdit(collection)} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/5 px-3 py-1.5 text-xs text-gray-300 hover:bg-white/10">
                                  <Pencil className="h-3.5 w-3.5" />
                                  {t('aiGroupingReviews.edit')}
                                </button>
                                <button onClick={() => runCollectionAction(activeReview, collection, 'reject')} disabled={actingKey === `collection:${collection.id}:reject`} className="inline-flex items-center gap-1.5 rounded-lg border border-red-400/20 bg-red-500/10 px-3 py-1.5 text-xs text-red-200 hover:bg-red-500/15 disabled:opacity-40">
                                  {actingKey === `collection:${collection.id}:reject` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <XCircle className="h-3.5 w-3.5" />}
                                  {t('aiGroupingReviews.rejectCollection')}
                                </button>
                                <button onClick={() => runCollectionAction(activeReview, collection, 'apply')} disabled={actingKey === `collection:${collection.id}:apply`} className="inline-flex items-center gap-1.5 rounded-lg bg-komgaPrimary px-3 py-1.5 text-xs font-semibold text-white hover:bg-komgaPrimaryHover disabled:opacity-40">
                                  {actingKey === `collection:${collection.id}:apply` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CheckCircle2 className="h-3.5 w-3.5" />}
                                  {t('aiGroupingReviews.applyCollection')}
                                </button>
                              </>
                            )}
                          </div>
                        )}
                      </section>
                    );
                  })}
                </div>
              </article>
            ) : (
              <div className="flex min-h-[320px] items-center justify-center text-sm text-gray-500">
                {t('aiGroupingReviews.detail.empty')}
              </div>
            )}
          </div>
        </div>
      )}

      {markedCount > 0 && (
        <div className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50 bg-gray-900/90 backdrop-blur-xl border border-white/10 rounded-2xl px-6 py-4 shadow-[0_20px_50px_rgba(0,0,0,0.6)] flex flex-col sm:flex-row items-center gap-4 animate-in slide-in-from-bottom duration-300 select-none">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold text-white">
              {t('reviewInbox.markedSummary', { apply: applyIds.length, reject: rejectIds.length })}
            </span>
          </div>
          <div className="flex items-center gap-3">
            <button onClick={clearMarks} disabled={actingKey === 'marked'} className="inline-flex items-center justify-center gap-1.5 rounded-xl border border-white/10 bg-white/5 px-4 py-2 text-xs font-semibold text-gray-200 hover:bg-white/10 transition-all active:scale-95">
              {t('reviewInbox.clearMarks')}
            </button>
            <button onClick={runMarkedActions} disabled={actingKey === 'marked'} className="inline-flex items-center justify-center gap-1.5 rounded-xl bg-komgaPrimary px-5 py-2 text-xs font-semibold text-white hover:bg-komgaPrimaryHover shadow-lg shadow-komgaPrimary/20 transition-all active:scale-95">
              {actingKey === 'marked' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CheckCircle2 className="h-3.5 w-3.5" />}
              {t('reviewInbox.applyMarked')}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
