import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import axios from 'axios';
import { ArrowDown, ArrowUp, BookOpen, ListOrdered, Pencil, Play, Plus, Search, Trash2, X } from 'lucide-react';
import { ModalShell } from '../components/ui/ModalShell';
import { ConfirmDialog } from '../components/ui/ConfirmDialog';
import { modalGhostButtonClass, modalInputClass, modalPrimaryButtonClass, modalTextareaClass } from '../components/ui/modalStyles';
import { useI18n } from '../i18n/LocaleProvider';
import type { SearchHit } from '../components/layout/types';

interface ReadingList {
  id: number;
  name: string;
  description: string;
  item_count: number;
}

interface ReadingListItem {
  id: number;
  reading_list_id: number;
  series_id: number;
  series_name: string;
  series_title: string;
  book_count: number;
  cover_path: string;
  next_book_id: number;
  note: string;
}

export default function ReadingLists() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [lists, setLists] = useState<ReadingList[]>([]);
  const [selected, setSelected] = useState<ReadingList | null>(null);
  const [items, setItems] = useState<ReadingListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [showEdit, setShowEdit] = useState(false);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [pendingDelete, setPendingDelete] = useState<ReadingList | null>(null);
  const [seriesQuery, setSeriesQuery] = useState('');
  const [seriesResults, setSeriesResults] = useState<SearchHit[]>([]);

  const loadLists = () => {
    setLoading(true);
    axios.get<ReadingList[]>('/api/reading-lists/')
      .then((res) => {
        const next = res.data || [];
        setLists(next);
        setSelected((current) => current ? next.find((item) => item.id === current.id) || next[0] || null : next[0] || null);
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    queueMicrotask(loadLists);
  }, []);

  useEffect(() => {
    if (!selected) {
      queueMicrotask(() => setItems([]));
      return;
    }
    axios.get<ReadingListItem[]>(`/api/reading-lists/${selected.id}/items`)
      .then((res) => setItems(res.data || []));
  }, [selected]);

  useEffect(() => {
    const query = seriesQuery.trim();
    if (!query) {
      queueMicrotask(() => setSeriesResults([]));
      return;
    }
    const controller = new AbortController();
    axios.get<{ hits: SearchHit[] }>('/api/search', {
      params: { q: query, target: 'series' },
      signal: controller.signal,
    }).then((res) => setSeriesResults(res.data.hits || []))
      .catch((error) => {
        if (!axios.isCancel(error)) setSeriesResults([]);
      });
    return () => controller.abort();
  }, [seriesQuery]);

  const openCreate = () => {
    setName('');
    setDescription('');
    setShowCreate(true);
  };

  const openEdit = () => {
    if (!selected) return;
    setName(selected.name);
    setDescription(selected.description || '');
    setShowEdit(true);
  };

  const saveCreate = () => {
    if (!name.trim()) return;
    axios.post<ReadingList>('/api/reading-lists/', { name, description }).then((res) => {
      setShowCreate(false);
      setSelected(res.data);
      loadLists();
    });
  };

  const saveEdit = () => {
    if (!selected || !name.trim()) return;
    axios.put<ReadingList>(`/api/reading-lists/${selected.id}`, { name, description }).then((res) => {
      setShowEdit(false);
      setSelected(res.data);
      loadLists();
    });
  };

  const deleteList = () => {
    if (!pendingDelete) return;
    axios.delete(`/api/reading-lists/${pendingDelete.id}`).then(() => {
      if (selected?.id === pendingDelete.id) setSelected(null);
      setPendingDelete(null);
      loadLists();
    });
  };

  const addSeries = (hit: SearchHit) => {
    if (!selected) return;
    const id = Number((hit.id || hit.fields?.id || '').replace('s_', ''));
    if (!id) return;
    axios.post(`/api/reading-lists/${selected.id}/items`, { series_id: id }).then(() => {
      setSeriesQuery('');
      setSeriesResults([]);
      return axios.get<ReadingListItem[]>(`/api/reading-lists/${selected.id}/items`);
    }).then((res) => {
      setItems(res.data || []);
      loadLists();
    });
  };

  const removeItem = (item: ReadingListItem) => {
    if (!selected) return;
    axios.delete(`/api/reading-lists/${selected.id}/items/${item.id}`).then(() => {
      setItems((prev) => prev.filter((entry) => entry.id !== item.id));
      loadLists();
    });
  };

  const reorder = (index: number, direction: -1 | 1) => {
    if (!selected) return;
    const nextIndex = index + direction;
    if (nextIndex < 0 || nextIndex >= items.length) return;
    const next = [...items];
    [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
    setItems(next);
    axios.post(`/api/reading-lists/${selected.id}/items/reorder`, { item_ids: next.map((item) => item.id) });
  };

  if (loading) {
    return <div className="flex min-h-[60vh] items-center justify-center text-gray-500">{t('common.loading')}</div>;
  }

  return (
    <div className="mx-auto max-w-6xl p-4 sm:p-8">
      <div className="mb-6 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <ListOrdered className="h-7 w-7 text-komgaPrimary" />
          <div>
            <h1 className="text-2xl font-bold tracking-tight text-white">{t('readingLists.title')}</h1>
            <p className="text-sm text-gray-500">{t('readingLists.subtitle')}</p>
          </div>
        </div>
        <button onClick={openCreate} className="flex items-center gap-2 rounded-lg bg-komgaPrimary px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-komgaPrimaryHover">
          <Plus className="h-4 w-4" />
          {t('readingLists.create')}
        </button>
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <aside className="space-y-2">
          {lists.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-gray-800 bg-gray-950/40 p-8 text-center text-gray-500">
              <ListOrdered className="mx-auto mb-3 h-10 w-10 opacity-40" />
              <p className="text-sm">{t('readingLists.empty')}</p>
            </div>
          ) : lists.map((list) => (
            <button
              key={list.id}
              onClick={() => setSelected(list)}
              className={`w-full rounded-xl border p-4 text-left transition ${selected?.id === list.id ? 'border-komgaPrimary/50 bg-komgaPrimary/10' : 'border-gray-800 bg-komgaSurface hover:border-gray-700'}`}
            >
              <div className="font-medium text-white">{list.name}</div>
              <div className="mt-1 text-xs text-gray-500">{t('readingLists.itemCount', { count: list.item_count })}</div>
            </button>
          ))}
        </aside>

        <section className="lg:col-span-2">
          {selected ? (
            <div className="space-y-5">
              <div className="rounded-2xl border border-gray-800 bg-komgaSurface p-5">
                <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                  <div>
                    <h2 className="text-xl font-semibold text-white">{selected.name}</h2>
                    {selected.description && <p className="mt-1 text-sm text-gray-500">{selected.description}</p>}
                  </div>
                  <div className="flex gap-2">
                    <button onClick={openEdit} className="rounded-lg border border-gray-700 p-2 text-gray-300 hover:text-white" title={t('common.edit')}>
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button onClick={() => setPendingDelete(selected)} className="rounded-lg border border-red-900/50 p-2 text-red-300 hover:bg-red-950/40" title={t('common.delete')}>
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </div>

                <div className="relative mt-5">
                  <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-gray-500" />
                  <input
                    value={seriesQuery}
                    onChange={(event) => setSeriesQuery(event.target.value)}
                    placeholder={t('readingLists.searchPlaceholder')}
                    className="w-full rounded-lg border border-gray-800 bg-gray-950 py-2 pl-9 pr-3 text-sm text-white outline-none focus:border-komgaPrimary/60"
                  />
                  {seriesResults.length > 0 && (
                    <div className="absolute z-20 mt-2 w-full overflow-hidden rounded-xl border border-gray-800 bg-gray-950 shadow-xl">
                      {seriesResults.map((hit) => (
                        <button key={hit.id} onClick={() => addSeries(hit)} className="flex w-full items-center justify-between px-4 py-3 text-left text-sm text-gray-200 hover:bg-gray-900">
                          <span>{hit.fields?.title || hit.fields?.series_name || hit.id}</span>
                          <Plus className="h-4 w-4 text-komgaPrimary" />
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              </div>

              {items.length === 0 ? (
                <div className="rounded-2xl border border-dashed border-gray-800 bg-gray-950/30 p-12 text-center text-gray-500">
                  <BookOpen className="mx-auto mb-3 h-10 w-10 opacity-40" />
                  <p>{t('readingLists.noItems')}</p>
                </div>
              ) : (
                <div className="space-y-3">
                  {items.map((item, index) => {
                    const title = item.series_title || item.series_name;
                    return (
                      <div key={item.id} className="flex gap-4 rounded-2xl border border-gray-800 bg-komgaSurface p-3">
                        <div className="h-24 w-16 shrink-0 overflow-hidden rounded-lg bg-gray-900">
                          {item.cover_path ? <img src={`/api/thumbnails/${item.cover_path}`} alt={title} className="h-full w-full object-cover" /> : <BookOpen className="m-5 h-6 w-6 text-gray-700" />}
                        </div>
                        <div className="min-w-0 flex-1">
                          <button onClick={() => navigate(`/series/${item.series_id}`)} className="truncate text-left font-medium text-white hover:text-komgaPrimary">{title}</button>
                          <p className="mt-1 text-xs text-gray-500">{t('common.books', { count: item.book_count })}</p>
                          {item.note && <p className="mt-2 text-xs text-gray-400">{item.note}</p>}
                          <div className="mt-3 flex flex-wrap gap-2">
                            <button disabled={!item.next_book_id} onClick={() => navigate(`/reader/${item.next_book_id}`)} className="inline-flex items-center gap-1 rounded-lg bg-komgaPrimary px-3 py-1.5 text-xs font-medium text-white disabled:cursor-not-allowed disabled:bg-gray-800 disabled:text-gray-500">
                              <Play className="h-3 w-3" />
                              {t('readingLists.readNext')}
                            </button>
                            <button onClick={() => reorder(index, -1)} disabled={index === 0} className="rounded-lg border border-gray-800 p-1.5 text-gray-400 disabled:opacity-30"><ArrowUp className="h-3.5 w-3.5" /></button>
                            <button onClick={() => reorder(index, 1)} disabled={index === items.length - 1} className="rounded-lg border border-gray-800 p-1.5 text-gray-400 disabled:opacity-30"><ArrowDown className="h-3.5 w-3.5" /></button>
                            <button onClick={() => removeItem(item)} className="rounded-lg border border-red-900/50 p-1.5 text-red-300"><X className="h-3.5 w-3.5" /></button>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          ) : (
            <div className="flex min-h-[40vh] items-center justify-center rounded-2xl border border-gray-800 text-gray-500">{t('readingLists.pickLeft')}</div>
          )}
        </section>
      </div>

      <ModalShell
        open={showCreate || showEdit}
        onClose={() => { setShowCreate(false); setShowEdit(false); }}
        title={showEdit ? t('readingLists.editTitle') : t('readingLists.createTitle')}
        description={t('readingLists.formDescription')}
        icon={<ListOrdered className="h-5 w-5" />}
        size="compact"
        footer={
          <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
            <button onClick={() => { setShowCreate(false); setShowEdit(false); }} className={modalGhostButtonClass}>{t('modal.cancel')}</button>
            <button onClick={showEdit ? saveEdit : saveCreate} className={modalPrimaryButtonClass}>{showEdit ? t('common.save') : t('common.create')}</button>
          </div>
        }
      >
        <div className="space-y-4">
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('readingLists.namePlaceholder')} className={modalInputClass} autoFocus />
          <textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder={t('readingLists.descriptionPlaceholder')} rows={4} className={modalTextareaClass} />
        </div>
      </ModalShell>

      <ConfirmDialog
        open={pendingDelete !== null}
        onClose={() => setPendingDelete(null)}
        onConfirm={deleteList}
        title={t('readingLists.deleteTitle')}
        description={t('readingLists.deleteDescription', { name: pendingDelete?.name || '' })}
        confirmLabel={t('common.delete')}
        tone="danger"
      />
    </div>
  );
}
