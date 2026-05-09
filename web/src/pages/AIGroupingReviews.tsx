import { useEffect, useMemo, useState } from 'react';
import axios from 'axios';
import { Link, useOutletContext } from 'react-router-dom';
import { CheckCircle2, Filter, Layers3, Loader2, Pencil, Save, Sparkles, X, XCircle } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';

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

function getApiErrorMessage(error: unknown, fallback: string) {
  if (axios.isAxiosError(error)) {
    return error.response?.data?.error || error.message || fallback;
  }
  if (error instanceof Error) return error.message;
  return fallback;
}

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

export default function AIGroupingReviews() {
  const { t, formatDateTime } = useI18n();
  const { refreshTrigger, libraries } = useOutletContext<{ refreshTrigger: number; libraries?: LibraryOption[] }>() || { refreshTrigger: 0, libraries: [] };
  const [items, setItems] = useState<AIGroupingReview[]>([]);
  const [total, setTotal] = useState(0);
  const [libraryId, setLibraryId] = useState('0');
  const [status, setStatus] = useState('pending');
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [actingKey, setActingKey] = useState<string | null>(null);
  const [editingDrafts, setEditingDrafts] = useState<Record<number, CollectionDraft>>({});
  const [toastMsg, setToastMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);

  const limit = 20;
  const pendingCount = useMemo(() => items.filter((item) => item.status === 'pending').length, [items]);

  const showToast = (text: string, type: 'success' | 'error' = 'success') => {
    setToastMsg({ text, type });
    window.setTimeout(() => setToastMsg(null), 3200);
  };

  const loadReviews = async () => {
    setLoading(true);
    try {
      const offset = (page - 1) * limit;
      const res = await axios.get<AIGroupingReviewsResponse>('/api/ai-grouping/reviews', {
        params: {
          library_id: libraryId,
          status,
          limit,
          offset,
        },
      });
      setItems(res.data.items || []);
      setTotal(res.data.total || 0);
    } catch (err: unknown) {
      showToast(getApiErrorMessage(err, t('aiGroupingReviews.toast.loadFailed')), 'error');
      setItems([]);
      setTotal(0);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadReviews();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [libraryId, status, page, refreshTrigger]);

  const runAction = async (review: AIGroupingReview, action: 'apply' | 'reject') => {
    if (review.status !== 'pending') return;
    setActingKey(`review:${review.id}:${action}`);
    try {
      const res = await axios.post(`/api/ai-grouping/reviews/${review.id}/${action}`);
      if (action === 'apply') {
        showToast(t('aiGroupingReviews.toast.applied', { count: res.data.collections || 0 }));
      } else {
        showToast(t('aiGroupingReviews.toast.rejected'));
      }
      await loadReviews();
    } catch (err: unknown) {
      showToast(getApiErrorMessage(err, t('aiGroupingReviews.toast.actionFailed')), 'error');
    } finally {
      setActingKey(null);
    }
  };

  const runCollectionAction = async (review: AIGroupingReview, collection: AIGroupingReviewCollection, action: 'apply' | 'reject') => {
    if (review.status !== 'pending' || collection.status !== 'pending') return;
    setActingKey(`collection:${collection.id}:${action}`);
    try {
      const res = await axios.post(`/api/ai-grouping/reviews/${review.id}/collections/${collection.id}/${action}`);
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
      await loadReviews();
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
      await axios.put(`/api/ai-grouping/reviews/${review.id}/collections/${collection.id}`, {
        name: draft.name,
        description: draft.description,
        series_ids: draft.seriesIds,
      });
      showToast(t('aiGroupingReviews.toast.collectionUpdated'));
      cancelEdit(collection.id);
      await loadReviews();
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

  const totalPages = Math.max(1, Math.ceil(total / limit));

  return (
    <div className="min-h-screen bg-komgaDark p-6 lg:p-10">
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
            <p className="text-xs uppercase tracking-wide text-gray-500">{t('aiGroupingReviews.metric.pendingPage')}</p>
            <p className="mt-1 text-2xl font-semibold text-white">{pendingCount}</p>
          </div>
        </div>
      </div>

      <div className="mb-6 grid gap-3 rounded-2xl border border-white/10 bg-komgaSurface/70 p-4 backdrop-blur md:grid-cols-[1fr_180px_auto]">
        <select value={libraryId} onChange={(event) => { setLibraryId(event.target.value); setPage(1); }} className="rounded-xl border border-white/10 bg-gray-950 px-3 py-2.5 text-sm text-white outline-none focus:border-komgaPrimary">
          <option value="0">{t('aiGroupingReviews.allLibraries')}</option>
          {(libraries || []).map((library) => (
            <option key={library.id} value={library.id}>{library.name}</option>
          ))}
        </select>
        <select value={status} onChange={(event) => { setStatus(event.target.value); setPage(1); }} className="rounded-xl border border-white/10 bg-gray-950 px-3 py-2.5 text-sm text-white outline-none focus:border-komgaPrimary">
          <option value="pending">{t('aiGroupingReviews.status.pending')}</option>
          <option value="applied">{t('aiGroupingReviews.status.applied')}</option>
          <option value="rejected">{t('aiGroupingReviews.status.rejected')}</option>
          <option value="">{t('aiGroupingReviews.status.all')}</option>
        </select>
        <button onClick={() => loadReviews()} className="inline-flex items-center justify-center gap-2 rounded-xl bg-komgaPrimary px-4 py-2.5 text-sm font-semibold text-white shadow-lg shadow-komgaPrimary/20 hover:bg-komgaPrimaryHover">
          <Filter className="h-4 w-4" />
          {t('common.search')}
        </button>
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
        <div className="space-y-4">
          {items.map((review) => (
            <article key={review.id} className="rounded-2xl border border-white/10 bg-komgaSurface/70 p-4">
              <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={`rounded-full border px-2.5 py-1 text-xs font-semibold ${statusClass(review.status)}`}>{t(`aiGroupingReviews.status.${review.status}`)}</span>
                    <span className="rounded-lg border border-white/10 bg-white/5 px-2 py-1 text-xs text-gray-300">{review.library_name}</span>
                    <span className="rounded-lg border border-cyan-400/20 bg-cyan-400/10 px-2 py-1 text-xs text-cyan-200">{review.provider || t('common.none')}</span>
                    <span className="text-xs text-gray-500">{formatDateTime(review.created_at)}</span>
                  </div>
                  <h2 className="mt-3 text-xl font-semibold text-white">{t('aiGroupingReviews.reviewTitle', { id: review.id })}</h2>
                  <p className="mt-2 text-sm text-gray-400">{t('aiGroupingReviews.reviewSummary', { candidates: review.candidate_count, collections: review.collection_count })}</p>
                </div>
                {review.status === 'pending' && (
                  <div className="flex flex-wrap gap-2">
                    <button onClick={() => runAction(review, 'reject')} disabled={actingKey === `review:${review.id}:reject`} className="inline-flex items-center justify-center gap-2 rounded-xl border border-red-400/20 bg-red-500/10 px-4 py-2 text-sm font-medium text-red-200 hover:bg-red-500/15 disabled:opacity-40">
                      {actingKey === `review:${review.id}:reject` ? <Loader2 className="h-4 w-4 animate-spin" /> : <XCircle className="h-4 w-4" />}
                      {t('aiGroupingReviews.reject')}
                    </button>
                    <button onClick={() => runAction(review, 'apply')} disabled={actingKey === `review:${review.id}:apply`} className="inline-flex items-center justify-center gap-2 rounded-xl bg-komgaPrimary px-4 py-2 text-sm font-semibold text-white hover:bg-komgaPrimaryHover disabled:opacity-40">
                      {actingKey === `review:${review.id}:apply` ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
                      {t('aiGroupingReviews.apply')}
                    </button>
                  </div>
                )}
              </div>

              <div className="mt-4 grid gap-3 xl:grid-cols-2">
                {review.collections.map((collection) => {
                  const draft = editingDrafts[collection.id];
                  const editable = review.status === 'pending' && collection.status === 'pending';
                  return (
                    <section key={collection.id} className="rounded-xl border border-white/10 bg-gray-950/60 p-4">
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2">
                            <Layers3 className="h-4 w-4 shrink-0 text-amber-200" />
                            {draft ? (
                              <input value={draft.name} onChange={(event) => updateDraft(collection.id, { name: event.target.value })} className="min-w-0 flex-1 rounded-lg border border-white/10 bg-black/30 px-3 py-1.5 text-sm font-semibold text-white outline-none focus:border-komgaPrimary" />
                            ) : (
                              <h3 className="truncate text-base font-semibold text-white">{collection.name}</h3>
                            )}
                          </div>
                          {draft ? (
                            <textarea value={draft.description} onChange={(event) => updateDraft(collection.id, { description: event.target.value })} rows={2} className="mt-2 w-full rounded-lg border border-white/10 bg-black/30 px-3 py-2 text-sm text-gray-200 outline-none focus:border-komgaPrimary" />
                          ) : (
                            collection.description && <p className="mt-2 text-sm leading-6 text-gray-400">{collection.description}</p>
                          )}
                        </div>
                        <span className={`shrink-0 rounded-full border px-2 py-1 text-[11px] ${statusClass(collection.status)}`}>{t(`aiGroupingReviews.status.${collection.status}`)}</span>
                      </div>
                      <div className="mt-3 flex flex-wrap gap-2">
                        {collection.series.map((series) => {
                          const selected = draft ? draft.seriesIds.includes(series.id) : true;
                          if (draft) {
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
                              <button onClick={() => saveDraft(review, collection)} disabled={actingKey === `collection:${collection.id}:save` || !draft.name.trim() || draft.seriesIds.length === 0} className="inline-flex items-center gap-1.5 rounded-lg bg-komgaPrimary px-3 py-1.5 text-xs font-semibold text-white hover:bg-komgaPrimaryHover disabled:opacity-40">
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
                              <button onClick={() => runCollectionAction(review, collection, 'reject')} disabled={actingKey === `collection:${collection.id}:reject`} className="inline-flex items-center gap-1.5 rounded-lg border border-red-400/20 bg-red-500/10 px-3 py-1.5 text-xs text-red-200 hover:bg-red-500/15 disabled:opacity-40">
                                {actingKey === `collection:${collection.id}:reject` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <XCircle className="h-3.5 w-3.5" />}
                                {t('aiGroupingReviews.rejectCollection')}
                              </button>
                              <button onClick={() => runCollectionAction(review, collection, 'apply')} disabled={actingKey === `collection:${collection.id}:apply`} className="inline-flex items-center gap-1.5 rounded-lg bg-komgaPrimary px-3 py-1.5 text-xs font-semibold text-white hover:bg-komgaPrimaryHover disabled:opacity-40">
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
          ))}
        </div>
      )}

      <div className="mt-8 flex flex-col items-center justify-between gap-4 border-t border-white/10 pt-6 sm:flex-row">
        <p className="text-sm text-gray-500">{t('aiGroupingReviews.pageSummary', { page, total: totalPages })}</p>
        <div className="flex gap-2">
          <button onClick={() => setPage((value) => Math.max(1, value - 1))} disabled={page <= 1} className="rounded-xl border border-white/10 bg-gray-950 px-4 py-2 text-sm text-gray-300 hover:bg-white/5 disabled:opacity-40">{t('home.pagination.prev')}</button>
          <button onClick={() => setPage((value) => Math.min(totalPages, value + 1))} disabled={page >= totalPages} className="rounded-xl border border-white/10 bg-gray-950 px-4 py-2 text-sm text-gray-300 hover:bg-white/5 disabled:opacity-40">{t('home.pagination.next')}</button>
        </div>
      </div>

      {toastMsg && (
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
