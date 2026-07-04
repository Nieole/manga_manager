/**
 * 业务说明：本文件是前端合集页面的「编辑智能合集」弹窗组件，提供标签/作者/状态/首字母/阅读状态/
 * 评分区间/进度区间/加入天数/排序/每页数量等筛选条件的受控表单。
 * 表单值与 setter 均由父组件持有并透传，本组件仅负责渲染与回调，保持行为不变。
 */

import { SlidersHorizontal } from 'lucide-react';
import { ModalShell } from '../../components/ui/ModalShell';
import { modalGhostButtonClass, modalInputClass, modalPrimaryButtonClass } from '../../components/ui/modalStyles';
import type { TFunc } from './types';

export interface SmartEditValues {
  name: string;
  tag: string;
  author: string;
  status: string;
  letter: string;
  readState: string;
  minRating: string;
  maxRating: string;
  minProgress: string;
  maxProgress: string;
  addedWithinDays: string;
  sortBy: string;
  sortDir: string;
  pageSize: number;
}

export interface SmartEditSetters {
  setName: (v: string) => void;
  setTag: (v: string) => void;
  setAuthor: (v: string) => void;
  setStatus: (v: string) => void;
  setLetter: (v: string) => void;
  setReadState: (v: string) => void;
  setMinRating: (v: string) => void;
  setMaxRating: (v: string) => void;
  setMinProgress: (v: string) => void;
  setMaxProgress: (v: string) => void;
  setAddedWithinDays: (v: string) => void;
  setSortBy: (v: string) => void;
  setSortDir: (v: string) => void;
  setPageSize: (v: number) => void;
}

interface SmartEditModalProps {
  open: boolean;
  onClose: () => void;
  onSubmit: () => void;
  values: SmartEditValues;
  set: SmartEditSetters;
  t: TFunc;
}

export function SmartEditModal({ open, onClose, onSubmit, values, set, t }: SmartEditModalProps) {
  return (
    <ModalShell
      open={open}
      onClose={onClose}
      title={t('collections.editSmartTitle')}
      description={t('collections.editSmartDescription')}
      icon={<SlidersHorizontal className="h-5 w-5" />}
      size="standard"
      footer={
        <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
          <button onClick={onClose} className={modalGhostButtonClass}>{t('modal.cancel')}</button>
          <button onClick={onSubmit} className={modalPrimaryButtonClass}>{t('collections.editSubmit')}</button>
        </div>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <input value={values.name} onChange={(e) => set.setName(e.target.value)} placeholder={t('collections.namePlaceholder')} className={modalInputClass} />
        <input value={values.tag} onChange={(e) => set.setTag(e.target.value)} placeholder={t('collections.smartTagPlaceholder')} className={modalInputClass} />
        <input value={values.author} onChange={(e) => set.setAuthor(e.target.value)} placeholder={t('collections.smartAuthorPlaceholder')} className={modalInputClass} />
        <input value={values.status} onChange={(e) => set.setStatus(e.target.value)} placeholder={t('collections.smartStatusPlaceholder')} className={modalInputClass} />
        <input value={values.letter} onChange={(e) => set.setLetter(e.target.value.toUpperCase())} placeholder={t('collections.smartLetterPlaceholder')} className={modalInputClass} />
        <select value={values.readState} onChange={(e) => set.setReadState(e.target.value)} className={modalInputClass}>
          <option value="">{t('collections.readState.any')}</option>
          <option value="unread">{t('collections.readState.unread')}</option>
          <option value="reading">{t('collections.readState.reading')}</option>
          <option value="completed">{t('collections.readState.completed')}</option>
        </select>
        <input type="number" min="0" max="10" step="0.1" value={values.minRating} onChange={(e) => set.setMinRating(e.target.value)} placeholder={t('collections.minRatingPlaceholder')} className={modalInputClass} />
        <input type="number" min="0" max="10" step="0.1" value={values.maxRating} onChange={(e) => set.setMaxRating(e.target.value)} placeholder={t('collections.maxRatingPlaceholder')} className={modalInputClass} />
        <input type="number" min="0" max="100" step="1" value={values.minProgress} onChange={(e) => set.setMinProgress(e.target.value)} placeholder={t('collections.minProgressPlaceholder')} className={modalInputClass} />
        <input type="number" min="0" max="100" step="1" value={values.maxProgress} onChange={(e) => set.setMaxProgress(e.target.value)} placeholder={t('collections.maxProgressPlaceholder')} className={modalInputClass} />
        <input type="number" min="1" max="3650" step="1" value={values.addedWithinDays} onChange={(e) => set.setAddedWithinDays(e.target.value)} placeholder={t('collections.addedWithinDaysPlaceholder')} className={modalInputClass} />
        <select value={values.sortBy} onChange={(e) => set.setSortBy(e.target.value)} className={modalInputClass}>
          {['name', 'created', 'updated', 'rating', 'volumes', 'books', 'pages', 'read', 'favorite'].map((field) => <option key={field} value={field}>{t(`home.toolbar.sort.${field}`)}</option>)}
        </select>
        <select value={values.sortDir} onChange={(e) => set.setSortDir(e.target.value)} className={modalInputClass}>
          <option value="asc">{t('home.smartFilters.dir.asc')}</option>
          <option value="desc">{t('home.smartFilters.dir.desc')}</option>
        </select>
        <select value={values.pageSize} onChange={(e) => set.setPageSize(Number(e.target.value))} className={modalInputClass}>
          {[30, 50, 100].map((size) => <option key={size} value={size}>{size}</option>)}
        </select>
      </div>
    </ModalShell>
  );
}
