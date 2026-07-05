/**
 * 业务说明：本文件是「重复文件」去重工作流面板，按 file_hash 分组列出内容相同的书籍，供用户挑选保留/移除。
 * 移除对应后端 POST /api/books/remove —— 仅从库中删除记录，可选把原文件移入回收站，绝不硬删源文件（安全）。
 * 维护时应关注：默认不选中任何书（避免误删）、移除前二次确认、移除后刷新分组。
 */

import { useEffect, useMemo, useState } from 'react';
import { apiClient, getApiErrorMessage } from '../api/client';
import { Copy, Loader2, RefreshCw, Trash2 } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';
import { useToast } from './ToastProvider';
import { ConfirmDialog } from './ui/ConfirmDialog';

interface DupBook {
  id: number;
  name: string;
  path: string;
  size: number;
  page_count: number;
  series_name: string;
}

interface DupGroup {
  file_hash: string;
  books: DupBook[];
}

function formatBytes(bytes: number): string {
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function DuplicatesPanel() {
  const { t } = useI18n();
  const { showToast } = useToast();
  const [groups, setGroups] = useState<DupGroup[]>([]);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [moveToTrash, setMoveToTrash] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [removing, setRemoving] = useState(false);

  const load = () => {
    setLoading(true);
    apiClient
      .get<{ groups: DupGroup[] }>('/api/books/duplicates')
      .then((res) => setGroups(res.data?.groups || []))
      .catch((err) => showToast(getApiErrorMessage(err, t('dedup.loadFailed')), 'error'))
      .finally(() => setLoading(false));
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const selectedCount = selected.size;
  const totalDuplicateBooks = useMemo(() => groups.reduce((sum, g) => sum + g.books.length, 0), [groups]);

  const doRemove = async () => {
    setConfirming(false);
    setRemoving(true);
    try {
      const res = await apiClient.post<{ removed: number; failed: number }>('/api/books/remove', {
        book_ids: [...selected],
        move_to_trash: moveToTrash,
      });
      showToast(t('dedup.removed', { count: res.data?.removed ?? 0 }), res.data?.failed ? 'error' : 'success');
      setSelected(new Set());
      load();
    } catch (err) {
      showToast(getApiErrorMessage(err, t('dedup.removeFailed')), 'error');
    } finally {
      setRemoving(false);
    }
  };

  return (
    <div className="rounded-2xl border border-gray-800 bg-komgaSurface p-6 shadow-xs">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Copy className="h-5 w-5 text-komgaPrimary" />
          <h3 className="text-lg font-bold text-white">{t('dedup.title')}</h3>
        </div>
        <button onClick={load} disabled={loading} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-1.5 text-xs text-gray-300 hover:text-white disabled:opacity-50">
          <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
          {t('dedup.refresh')}
        </button>
      </div>
      <p className="mt-1.5 text-sm text-gray-400">{t('dedup.description')}</p>

      {loading ? (
        <div className="flex justify-center py-12">
          <Loader2 className="h-7 w-7 animate-spin text-komgaPrimary" />
        </div>
      ) : groups.length === 0 ? (
        <p className="py-10 text-center text-sm text-gray-500">{t('dedup.empty')}</p>
      ) : (
        <>
          <p className="mt-4 text-xs text-gray-500">{t('dedup.summary', { groups: groups.length, books: totalDuplicateBooks })}</p>
          <div className="mt-3 max-h-[55vh] space-y-4 overflow-y-auto pr-1">
            {groups.map((group) => (
              <div key={group.file_hash} className="rounded-xl border border-gray-800 bg-gray-900/40 p-3">
                <div className="mb-2 flex items-center gap-2 text-xs text-gray-500">
                  <span className="rounded bg-gray-800 px-1.5 py-0.5 font-mono">{group.file_hash.slice(0, 12)}…</span>
                  <span>{t('dedup.groupCount', { count: group.books.length })}</span>
                </div>
                <div className="space-y-1.5">
                  {group.books.map((book) => (
                    <label key={book.id} className="flex cursor-pointer items-center gap-3 rounded-lg px-2 py-1.5 hover:bg-white/5">
                      <input type="checkbox" checked={selected.has(book.id)} onChange={() => toggle(book.id)} className="h-4 w-4 accent-red-500" />
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm text-gray-200">
                          <span className="text-gray-400">{book.series_name} · </span>
                          {book.name}
                        </div>
                        <div className="truncate text-[11px] text-gray-600">{book.path}</div>
                      </div>
                      <span className="shrink-0 text-[11px] text-gray-500">
                        {book.page_count}p · {formatBytes(book.size)}
                      </span>
                    </label>
                  ))}
                </div>
              </div>
            ))}
          </div>

          <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-gray-800 pt-4">
            <label className="flex items-center gap-2 text-xs text-gray-300">
              <input type="checkbox" checked={moveToTrash} onChange={(e) => setMoveToTrash(e.target.checked)} className="h-4 w-4 accent-komgaPrimary" />
              {t('dedup.moveToTrash')}
              <span className="text-gray-500">{t('dedup.moveToTrashHint')}</span>
            </label>
            <button
              onClick={() => setConfirming(true)}
              disabled={selectedCount === 0 || removing}
              className="inline-flex items-center gap-2 rounded-xl bg-red-500/90 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-red-500 disabled:opacity-40"
            >
              {removing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
              {t('dedup.removeSelected', { count: selectedCount })}
            </button>
          </div>
        </>
      )}

      <ConfirmDialog
        open={confirming}
        onClose={() => setConfirming(false)}
        onConfirm={doRemove}
        title={t('dedup.confirmTitle')}
        description={moveToTrash ? t('dedup.confirmDescriptionTrash', { count: selectedCount }) : t('dedup.confirmDescription', { count: selectedCount })}
        confirmLabel={t('dedup.remove')}
        tone="danger"
      />
    </div>
  );
}
