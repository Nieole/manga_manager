/**
 * 业务说明：本文件是前端合集页面的「智能合集快照」弹窗组件，把动态智能合集在当前时刻的成员固化为
 * 一个手工合集。展示快照名称/描述表单与实时预览（总数、将创建数、上限、命名冲突/截断提示、样例封面）。
 * 表单值与预览数据由父组件持有并透传，本组件仅负责渲染与回调。
 */

import { Camera, BookOpen, AlertTriangle } from 'lucide-react';
import { ModalShell } from '../../components/ui/ModalShell';
import { modalGhostButtonClass, modalInputClass, modalPrimaryButtonClass, modalTextareaClass } from '../../components/ui/modalStyles';
import type { SmartCollectionSnapshotPreview, TFunc } from './types';

interface SnapshotModalProps {
  open: boolean;
  onClose: () => void;
  onSubmit: () => void;
  name: string;
  onNameChange: (value: string) => void;
  desc: string;
  onDescChange: (value: string) => void;
  preview: SmartCollectionSnapshotPreview | null;
  previewLoading: boolean;
  t: TFunc;
}

export function SnapshotModal({ open, onClose, onSubmit, name, onNameChange, desc, onDescChange, preview, previewLoading, t }: SnapshotModalProps) {
  return (
    <ModalShell
      open={open}
      onClose={onClose}
      title={t('collections.snapshotTitle')}
      description={t('collections.snapshotDescription')}
      icon={<Camera className="h-5 w-5" />}
      size="standard"
      footer={
        <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
          <button onClick={onClose} className={modalGhostButtonClass}>{t('modal.cancel')}</button>
          <button
            onClick={onSubmit}
            disabled={!name.trim() || previewLoading || (preview?.snapshot_count ?? 1) <= 0}
            className={`${modalPrimaryButtonClass} disabled:cursor-not-allowed disabled:opacity-50`}
          >
            {t('collections.snapshotSubmit')}
          </button>
        </div>
      }
    >
      <div className="space-y-4">
        <input value={name} onChange={(e) => onNameChange(e.target.value)} placeholder={t('collections.namePlaceholder')} className={modalInputClass} />
        <textarea value={desc} onChange={(e) => onDescChange(e.target.value)} placeholder={t('collections.descriptionPlaceholder')} rows={3} className={modalTextareaClass} />
        <div className="rounded-2xl border border-gray-800 bg-gray-950/45 p-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <div>
              <p className="text-[11px] uppercase text-gray-500">{t('collections.snapshotPreview.total')}</p>
              <p className="mt-1 text-lg font-semibold text-white">{previewLoading ? '...' : preview?.total ?? '-'}</p>
            </div>
            <div>
              <p className="text-[11px] uppercase text-gray-500">{t('collections.snapshotPreview.create')}</p>
              <p className="mt-1 text-lg font-semibold text-white">{previewLoading ? '...' : preview?.snapshot_count ?? '-'}</p>
            </div>
            <div>
              <p className="text-[11px] uppercase text-gray-500">{t('collections.snapshotPreview.limit')}</p>
              <p className="mt-1 text-lg font-semibold text-white">{preview?.snapshot_limit ?? 1000}</p>
            </div>
          </div>
          {preview?.name_conflict && (
            <div className="mt-4 flex gap-2 rounded-xl border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-xs font-medium text-amber-500">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
              <span>{t('collections.snapshotPreview.nameConflict')}</span>
            </div>
          )}
          {preview?.truncated && (
            <div className="mt-3 flex gap-2 rounded-xl border border-cyan-500/25 bg-cyan-500/10 px-3 py-2 text-xs text-cyan-100">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
              <span>{t('collections.snapshotPreview.truncated', { count: preview.snapshot_limit })}</span>
            </div>
          )}
        </div>
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <p className="text-xs font-medium text-gray-300">{t('collections.snapshotPreview.sample')}</p>
            <p className="text-[11px] text-gray-500">{previewLoading ? t('common.loading') : t('common.seriesCount', { count: preview?.items.length ?? 0 })}</p>
          </div>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {(preview?.items || []).map((item) => {
              const coverUrl = item.cover_path?.Valid ? `/api/thumbnails/${item.cover_path.String}` : '';
              return (
                <div key={item.id} className="min-w-0 rounded-xl border border-gray-800 bg-gray-950/40 p-2">
                  <div className="aspect-2/3 overflow-hidden rounded-lg bg-gray-900">
                    {coverUrl ? (
                      <img src={coverUrl} alt={item.name} className="h-full w-full object-cover" />
                    ) : (
                      <div className="flex h-full w-full items-center justify-center text-gray-700"><BookOpen className="h-6 w-6" /></div>
                    )}
                  </div>
                  <p className="mt-2 truncate text-xs text-gray-300">{item.title?.Valid ? item.title.String : item.name}</p>
                </div>
              );
            })}
          </div>
          {!previewLoading && preview?.total === 0 && (
            <p className="rounded-xl border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-200">{t('collections.snapshotPreview.empty')}</p>
          )}
        </div>
      </div>
    </ModalShell>
  );
}
