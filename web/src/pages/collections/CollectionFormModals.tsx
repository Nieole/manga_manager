/**
 * 业务说明：本文件是前端合集页面的「新建合集」与「编辑合集」弹窗组件（手工合集的名称/描述表单）。
 * 表单状态由父组件持有，这里仅负责展示与回调，保持受控。
 * 维护时应保持两个弹窗的字段与提交语义一致。
 */

import { FolderHeart, Pencil } from 'lucide-react';
import { ModalShell } from '../../components/ui/ModalShell';
import { modalGhostButtonClass, modalInputClass, modalPrimaryButtonClass, modalTextareaClass } from '../../components/ui/modalStyles';
import type { TFunc } from './types';

interface CreateCollectionModalProps {
  open: boolean;
  onClose: () => void;
  name: string;
  onNameChange: (value: string) => void;
  desc: string;
  onDescChange: (value: string) => void;
  onSubmit: () => void;
  t: TFunc;
}

export function CreateCollectionModal({ open, onClose, name, onNameChange, desc, onDescChange, onSubmit, t }: CreateCollectionModalProps) {
  return (
    <ModalShell
      open={open}
      onClose={onClose}
      title={t('collections.createTitle')}
      description={t('collections.createDescription')}
      icon={<FolderHeart className="h-5 w-5" />}
      size="compact"
      footer={
        <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
          <button onClick={onClose} className={modalGhostButtonClass}>{t('modal.cancel')}</button>
          <button onClick={onSubmit} className={modalPrimaryButtonClass}>{t('common.create')}</button>
        </div>
      }
    >
      <div className="space-y-4">
        <input
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          placeholder={t('collections.namePlaceholder')}
          className={modalInputClass}
          autoFocus
        />
        <textarea
          value={desc}
          onChange={(e) => onDescChange(e.target.value)}
          placeholder={t('collections.descriptionPlaceholder')}
          rows={4}
          className={modalTextareaClass}
        />
      </div>
    </ModalShell>
  );
}

interface EditCollectionModalProps {
  open: boolean;
  onClose: () => void;
  name: string;
  onNameChange: (value: string) => void;
  desc: string;
  onDescChange: (value: string) => void;
  onSubmit: () => void;
  t: TFunc;
}

export function EditCollectionModal({ open, onClose, name, onNameChange, desc, onDescChange, onSubmit, t }: EditCollectionModalProps) {
  return (
    <ModalShell
      open={open}
      onClose={onClose}
      title={t('collections.editTitle')}
      description={t('collections.editDescription')}
      icon={<Pencil className="h-5 w-5" />}
      size="compact"
      footer={
        <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
          <button onClick={onClose} className={modalGhostButtonClass}>{t('modal.cancel')}</button>
          <button onClick={onSubmit} className={modalPrimaryButtonClass}>{t('collections.editSubmit')}</button>
        </div>
      }
    >
      <div className="space-y-4">
        <input
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          placeholder={t('collections.namePlaceholder')}
          className={modalInputClass}
          autoFocus
        />
        <textarea
          value={desc}
          onChange={(e) => onDescChange(e.target.value)}
          placeholder={t('collections.descriptionPlaceholder')}
          rows={4}
          className={modalTextareaClass}
        />
      </div>
    </ModalShell>
  );
}
