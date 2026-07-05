/**
 * 业务说明：本文件是标签管理设置页，提供跨全库的标签重命名 / 合并 / 删除。
 * 它消费后端 /api/tags 系列端点（PATCH 改名、POST merge 合并、DELETE 删除），
 * 这些是影响多个系列的破坏性操作，UI 需给出计数与二次确认。
 * 维护时应关注：改名重名冲突（后端 409）提示改用合并、合并/删除后列表与计数刷新。
 */

import { useEffect, useMemo, useState } from 'react';
import { apiClient, getApiErrorMessage } from '../../api/client';
import { Check, GitMerge, Loader2, Pencil, Search, Trash2, X } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';
import { ConfirmDialog } from '../../components/ui/ConfirmDialog';
import { SettingsPageIntro, sectionClassName, inputClassName } from './shared';

interface TagRow {
  id: number;
  name: string;
  series_count: number;
}

export function SettingsTagsPage() {
  const { t } = useI18n();
  const { showToast } = useToast();
  const [tags, setTags] = useState<TagRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState('');
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editName, setEditName] = useState('');
  const [mergingId, setMergingId] = useState<number | null>(null);
  const [mergeTarget, setMergeTarget] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<TagRow | null>(null);
  const [busyId, setBusyId] = useState<number | null>(null);

  const load = () => {
    setLoading(true);
    apiClient
      .get<TagRow[]>('/api/tags/all')
      .then((res) => setTags(res.data || []))
      .catch((err) => showToast(getApiErrorMessage(err, t('settingsTags.loadFailed')), 'error'))
      .finally(() => setLoading(false));
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = q ? tags.filter((tag) => tag.name.toLowerCase().includes(q)) : tags;
    return [...list].sort((a, b) => b.series_count - a.series_count || a.name.localeCompare(b.name));
  }, [tags, query]);

  const startRename = (tag: TagRow) => {
    setMergingId(null);
    setEditingId(tag.id);
    setEditName(tag.name);
  };

  const submitRename = async (tag: TagRow) => {
    const name = editName.trim();
    if (!name || name === tag.name) {
      setEditingId(null);
      return;
    }
    setBusyId(tag.id);
    try {
      await apiClient.patch(`/api/tags/${tag.id}`, { name });
      showToast(t('settingsTags.renamed'), 'success');
      setEditingId(null);
      load();
    } catch (err) {
      showToast(getApiErrorMessage(err, t('settingsTags.renameFailed')), 'error');
    } finally {
      setBusyId(null);
    }
  };

  const submitMerge = async (tag: TagRow) => {
    const target = tags.find((x) => x.name === mergeTarget && x.id !== tag.id);
    if (!target) {
      showToast(t('settingsTags.mergePickTarget'), 'error');
      return;
    }
    setBusyId(tag.id);
    try {
      await apiClient.post(`/api/tags/${tag.id}/merge`, { target_id: target.id });
      showToast(t('settingsTags.merged', { source: tag.name, target: target.name }), 'success');
      setMergingId(null);
      setMergeTarget('');
      load();
    } catch (err) {
      showToast(getApiErrorMessage(err, t('settingsTags.mergeFailed')), 'error');
    } finally {
      setBusyId(null);
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    setBusyId(deleteTarget.id);
    try {
      await apiClient.delete(`/api/tags/${deleteTarget.id}`);
      showToast(t('settingsTags.deleted'), 'success');
      setDeleteTarget(null);
      load();
    } catch (err) {
      showToast(getApiErrorMessage(err, t('settingsTags.deleteFailed')), 'error');
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div className="space-y-6">
      <SettingsPageIntro title={t('settingsTags.title')} description={t('settingsTags.description')} />

      <div className={sectionClassName}>
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('settingsTags.searchPlaceholder')}
            className={`${inputClassName} pl-9`}
          />
        </div>

        {loading ? (
          <div className="flex justify-center py-16">
            <Loader2 className="h-7 w-7 animate-spin text-komgaPrimary" />
          </div>
        ) : filtered.length === 0 ? (
          <p className="py-12 text-center text-sm text-gray-500">{t('settingsTags.empty')}</p>
        ) : (
          <div className="max-h-[62vh] space-y-1.5 overflow-y-auto pr-1">
            {filtered.map((tag) => (
              <div key={tag.id} className="rounded-xl border border-gray-800 bg-gray-900/50 px-4 py-2.5">
                <div className="flex items-center gap-3">
                  {editingId === tag.id ? (
                    <input
                      autoFocus
                      value={editName}
                      onChange={(e) => setEditName(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') submitRename(tag);
                        if (e.key === 'Escape') setEditingId(null);
                      }}
                      className="flex-1 rounded-md border border-gray-700 bg-gray-950 px-2 py-1 text-sm text-white outline-hidden focus:border-komgaPrimary"
                    />
                  ) : (
                    <span className="flex-1 truncate text-sm font-medium text-gray-100">{tag.name}</span>
                  )}
                  <span className="shrink-0 text-xs text-gray-500">{t('common.seriesCount', { count: tag.series_count })}</span>
                  <div className="flex shrink-0 items-center gap-1">
                    {busyId === tag.id ? (
                      <Loader2 className="h-4 w-4 animate-spin text-komgaPrimary" />
                    ) : editingId === tag.id ? (
                      <>
                        <button onClick={() => submitRename(tag)} className="rounded-md p-1.5 text-emerald-400 hover:bg-emerald-400/10" title={t('common.save')}>
                          <Check className="h-4 w-4" />
                        </button>
                        <button onClick={() => setEditingId(null)} className="rounded-md p-1.5 text-gray-400 hover:bg-white/5" title={t('common.cancel')}>
                          <X className="h-4 w-4" />
                        </button>
                      </>
                    ) : (
                      <>
                        <button onClick={() => startRename(tag)} className="rounded-md p-1.5 text-gray-400 hover:bg-white/5 hover:text-white" title={t('settingsTags.rename')}>
                          <Pencil className="h-4 w-4" />
                        </button>
                        <button
                          onClick={() => {
                            setEditingId(null);
                            setMergingId(mergingId === tag.id ? null : tag.id);
                            setMergeTarget('');
                          }}
                          className={`rounded-md p-1.5 hover:bg-white/5 ${mergingId === tag.id ? 'text-komgaPrimary' : 'text-gray-400 hover:text-white'}`}
                          title={t('settingsTags.merge')}
                        >
                          <GitMerge className="h-4 w-4" />
                        </button>
                        <button onClick={() => setDeleteTarget(tag)} className="rounded-md p-1.5 text-gray-400 hover:bg-red-500/10 hover:text-red-400" title={t('settingsTags.delete')}>
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </>
                    )}
                  </div>
                </div>

                {mergingId === tag.id && (
                  <div className="mt-2.5 flex items-center gap-2 border-t border-white/5 pt-2.5">
                    <span className="text-xs text-gray-400">{t('settingsTags.mergeInto')}</span>
                    <input
                      list="settings-tags-merge-options"
                      value={mergeTarget}
                      onChange={(e) => setMergeTarget(e.target.value)}
                      placeholder={t('settingsTags.mergeTargetPlaceholder')}
                      className="flex-1 rounded-md border border-gray-700 bg-gray-950 px-2 py-1 text-sm text-white outline-hidden focus:border-komgaPrimary"
                    />
                    <button onClick={() => submitMerge(tag)} className="rounded-md bg-komgaPrimary px-3 py-1 text-xs font-medium text-white hover:bg-komgaPrimaryHover">
                      {t('settingsTags.mergeConfirm')}
                    </button>
                  </div>
                )}
              </div>
            ))}
            <datalist id="settings-tags-merge-options">
              {tags.map((tag) => (
                <option key={tag.id} value={tag.name} />
              ))}
            </datalist>
          </div>
        )}
      </div>

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        onConfirm={confirmDelete}
        title={t('settingsTags.deleteTitle')}
        description={deleteTarget ? t('settingsTags.deleteDescription', { name: deleteTarget.name, count: deleteTarget.series_count }) : ''}
        confirmLabel={t('settingsTags.delete')}
        tone="danger"
      />
    </div>
  );
}
