/**
 * 业务说明：本文件是系列「自定义字段」编辑区，内嵌于元数据编辑弹窗，提供任意 key-value 元数据（如 ISBN、收藏位置）。
 * 它自包含地消费 GET/PUT /api/series/{id}/custom-fields（整体替换语义），与主元数据保存解耦、单独保存。
 * 维护时应关注：空 key 跳过、加载态、保存反馈。
 */

import { useEffect, useState } from 'react';
import { apiClient, getApiErrorMessage } from '../../api/client';
import { Loader2, Plus, Save, X } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';

interface Props {
  seriesId: number | undefined;
}

interface FieldRow {
  key: string;
  value: string;
}

export function SeriesCustomFieldsEditor({ seriesId }: Props) {
  const { t } = useI18n();
  const { showToast } = useToast();
  const [rows, setRows] = useState<FieldRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!seriesId) return;
    setLoading(true);
    apiClient
      .get<FieldRow[]>(`/api/series/${seriesId}/custom-fields`)
      .then((res) => setRows(res.data || []))
      .catch(() => setRows([]))
      .finally(() => setLoading(false));
  }, [seriesId]);

  const updateRow = (idx: number, patch: Partial<FieldRow>) => {
    setRows((prev) => prev.map((row, i) => (i === idx ? { ...row, ...patch } : row)));
  };

  const save = async () => {
    if (!seriesId) return;
    setSaving(true);
    try {
      await apiClient.put(`/api/series/${seriesId}/custom-fields`, {
        fields: rows.filter((row) => row.key.trim() !== ''),
      });
      showToast(t('customFields.saved'), 'success');
    } catch (err) {
      showToast(getApiErrorMessage(err, t('customFields.saveFailed')), 'error');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <label className="text-sm font-medium text-gray-300">{t('customFields.label')}</label>
        {loading && <Loader2 className="h-4 w-4 animate-spin text-komgaPrimary" />}
      </div>
      <div className="space-y-2">
        {rows.map((row, idx) => (
          <div key={idx} className="flex items-center gap-2">
            <input
              value={row.key}
              onChange={(e) => updateRow(idx, { key: e.target.value })}
              placeholder={t('customFields.keyPlaceholder')}
              className="w-1/3 rounded-xl border border-gray-800 bg-gray-900 px-3 py-2 text-sm text-white outline-hidden focus:ring-2 focus:ring-komgaPrimary/40"
            />
            <input
              value={row.value}
              onChange={(e) => updateRow(idx, { value: e.target.value })}
              placeholder={t('customFields.valuePlaceholder')}
              className="flex-1 rounded-xl border border-gray-800 bg-gray-900 px-3 py-2 text-sm text-white outline-hidden focus:ring-2 focus:ring-komgaPrimary/40"
            />
            <button
              onClick={() => setRows((prev) => prev.filter((_, i) => i !== idx))}
              className="rounded-xl border border-red-500/20 bg-red-500/10 p-2.5 text-red-300 transition-all hover:bg-red-500/20"
              aria-label={t('common.remove')}
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        ))}
        <div className="flex items-center gap-2">
          <button
            onClick={() => setRows((prev) => [...prev, { key: '', value: '' }])}
            className="inline-flex flex-1 items-center justify-center gap-1.5 rounded-xl border border-komgaPrimary/30 bg-komgaPrimary/10 px-3 py-2 text-xs font-medium text-komgaPrimary transition-colors hover:bg-komgaPrimary/20"
          >
            <Plus className="h-3.5 w-3.5" />
            {t('customFields.add')}
          </button>
          <button
            onClick={save}
            disabled={saving || !seriesId}
            className="inline-flex items-center gap-1.5 rounded-xl bg-komgaPrimary px-4 py-2 text-xs font-medium text-white transition-colors hover:bg-komgaPrimaryHover disabled:opacity-50"
          >
            {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
            {t('customFields.save')}
          </button>
        </div>
      </div>
    </div>
  );
}
