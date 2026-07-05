/**
 * 业务说明：本文件是批量元数据编辑弹窗，对多选的系列做增量编辑（增/删标签、改状态、改出版社）。
 * 它复用资料库的多选框架，把选中的 seriesIds 提交到 POST /api/series/bulk-edit（增量语义，与单系列全量替换不同）。
 * 维护时应关注“未填即不改”的语义、标签 chip 输入与后端字段（add_tags/remove_tags/status/publisher）的对应。
 */

import { useEffect, useState, type KeyboardEvent } from 'react';
import { apiClient, getApiErrorMessage } from '../api/client';
import { Tags, Loader2, X } from 'lucide-react';
import { ModalShell } from './ui/ModalShell';
import { modalPrimaryButtonClass, modalSectionClass } from './ui/modalStyles';
import { useI18n } from '../i18n/LocaleProvider';

interface Props {
  seriesIds: number[];
  onClose: () => void;
  onSuccess: (updated: number) => void;
  onError: (message: string) => void;
}

const STATUS_OPTIONS = ['completed', 'ongoing', 'cancelled', 'hiatus'] as const;

export default function BulkEditSeriesModal({ seriesIds, onClose, onSuccess, onError }: Props) {
  const { t } = useI18n();
  const [allTags, setAllTags] = useState<string[]>([]);
  const [addTags, setAddTags] = useState<string[]>([]);
  const [removeTags, setRemoveTags] = useState<string[]>([]);
  const [addInput, setAddInput] = useState('');
  const [removeInput, setRemoveInput] = useState('');
  const [status, setStatus] = useState('');
  const [publisher, setPublisher] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    apiClient
      .get<{ name: string }[]>('/api/tags/all')
      .then((res) => setAllTags((res.data || []).map((tag) => tag.name)))
      .catch(() => setAllTags([]));
  }, []);

  const addChip = (list: string[], setList: (v: string[]) => void, raw: string, clear: () => void) => {
    const value = raw.trim();
    if (value && !list.includes(value)) setList([...list, value]);
    clear();
  };

  const chipKeyDown = (
    e: KeyboardEvent<HTMLInputElement>,
    list: string[],
    setList: (v: string[]) => void,
    input: string,
    clear: () => void,
  ) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      addChip(list, setList, input, clear);
    }
  };

  const hasChanges = addTags.length > 0 || removeTags.length > 0 || status !== '' || publisher.trim() !== '';

  const handleApply = async () => {
    if (!hasChanges) return;
    setSubmitting(true);
    try {
      const res = await apiClient.post<{ updated: number }>('/api/series/bulk-edit', {
        series_ids: seriesIds,
        add_tags: addTags,
        remove_tags: removeTags,
        status: status !== '' ? status : undefined,
        publisher: publisher.trim() !== '' ? publisher.trim() : undefined,
      });
      onSuccess(res.data?.updated ?? seriesIds.length);
      onClose();
    } catch (err) {
      onError(getApiErrorMessage(err, t('bulkEdit.failed')));
    } finally {
      setSubmitting(false);
    }
  };

  const renderChips = (list: string[], setList: (v: string[]) => void) => (
    <div className="mt-1.5 flex flex-wrap gap-1.5">
      {list.map((tag) => (
        <span key={tag} className="inline-flex items-center gap-1 rounded-full border border-komgaPrimary/30 bg-komgaPrimary/10 px-2.5 py-1 text-xs text-komgaPrimary">
          {tag}
          <button onClick={() => setList(list.filter((x) => x !== tag))} className="rounded-full p-0.5 hover:bg-komgaPrimary/20" aria-label={t('common.remove')}>
            <X className="h-3 w-3" />
          </button>
        </span>
      ))}
    </div>
  );

  return (
    <ModalShell
      open
      onClose={onClose}
      title={t('bulkEdit.title')}
      description={t('bulkEdit.description', { count: seriesIds.length })}
      icon={<Tags className="h-5 w-5" />}
      size="compact"
      zIndexClassName="z-100"
    >
      <datalist id="bulk-edit-tag-options">
        {allTags.map((tag) => (
          <option key={tag} value={tag} />
        ))}
      </datalist>
      <div className="space-y-4">
        <div className={modalSectionClass}>
          <label className="text-xs font-semibold text-gray-300">{t('bulkEdit.addTags')}</label>
          <input
            list="bulk-edit-tag-options"
            value={addInput}
            onChange={(e) => setAddInput(e.target.value)}
            onKeyDown={(e) => chipKeyDown(e, addTags, setAddTags, addInput, () => setAddInput(''))}
            onBlur={() => addChip(addTags, setAddTags, addInput, () => setAddInput(''))}
            placeholder={t('bulkEdit.addTagsPlaceholder')}
            className="mt-1.5 w-full rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white outline-hidden placeholder:text-gray-500 focus:border-komgaPrimary"
          />
          {renderChips(addTags, setAddTags)}
        </div>

        <div className={modalSectionClass}>
          <label className="text-xs font-semibold text-gray-300">{t('bulkEdit.removeTags')}</label>
          <input
            list="bulk-edit-tag-options"
            value={removeInput}
            onChange={(e) => setRemoveInput(e.target.value)}
            onKeyDown={(e) => chipKeyDown(e, removeTags, setRemoveTags, removeInput, () => setRemoveInput(''))}
            onBlur={() => addChip(removeTags, setRemoveTags, removeInput, () => setRemoveInput(''))}
            placeholder={t('bulkEdit.removeTagsPlaceholder')}
            className="mt-1.5 w-full rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white outline-hidden placeholder:text-gray-500 focus:border-komgaPrimary"
          />
          {renderChips(removeTags, setRemoveTags)}
        </div>

        <div className={`${modalSectionClass} grid grid-cols-2 gap-3`}>
          <div>
            <label className="text-xs font-semibold text-gray-300">{t('bulkEdit.status')}</label>
            <select
              value={status}
              onChange={(e) => setStatus(e.target.value)}
              className="mt-1.5 w-full rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white outline-hidden focus:border-komgaPrimary"
            >
              <option value="">{t('bulkEdit.unchanged')}</option>
              {STATUS_OPTIONS.map((s) => (
                <option key={s} value={s}>
                  {t(`status.${s}`)}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="text-xs font-semibold text-gray-300">{t('bulkEdit.publisher')}</label>
            <input
              value={publisher}
              onChange={(e) => setPublisher(e.target.value)}
              placeholder={t('bulkEdit.publisherPlaceholder')}
              className="mt-1.5 w-full rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white outline-hidden placeholder:text-gray-500 focus:border-komgaPrimary"
            />
          </div>
        </div>

        <div className="flex justify-end gap-2 pt-1">
          <button onClick={onClose} className="rounded-lg border border-white/10 px-4 py-2 text-sm text-gray-300 hover:bg-white/5">
            {t('common.cancel')}
          </button>
          <button onClick={handleApply} disabled={!hasChanges || submitting} className={`${modalPrimaryButtonClass} inline-flex items-center gap-2 px-4 py-2 text-sm disabled:opacity-50`}>
            {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
            {submitting ? t('bulkEdit.applying') : t('bulkEdit.apply')}
          </button>
        </div>
      </div>
    </ModalShell>
  );
}
