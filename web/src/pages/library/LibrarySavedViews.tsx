/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useState } from 'react';
import { ChevronDown, ChevronUp, Save, Trash2 } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import type { SavedSmartFilter } from './types';

interface LibrarySavedViewsProps {
  views: SavedSmartFilter[];
  hasAnyFilter: boolean;
  onSave: (name: string) => void;
  onApply: (view: SavedSmartFilter) => void;
  onDelete: (id: string) => void;
  onExpand?: () => void;
}

export function LibrarySavedViews({ views, hasAnyFilter, onSave, onApply, onDelete, onExpand }: LibrarySavedViewsProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [draftName, setDraftName] = useState('');

  const toggleOpen = () => {
    setOpen((prev) => {
      const next = !prev;
      if (next) onExpand?.();
      return next;
    });
  };

  return (
    <div className="mb-4 rounded-xl border border-white/10 bg-komgaSurface/50 px-4 py-2">
      <div className="flex items-center gap-2">
        <button
          onClick={toggleOpen}
          className="inline-flex items-center gap-1 text-sm font-medium text-gray-300 hover:text-white transition-colors"
        >
          {open ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
          {t('home.smartFilters.title')} ({views.length})
        </button>
        {!open && views.length > 0 && (
          <div className="flex flex-wrap gap-1.5 overflow-x-auto">
            {views.slice(0, 6).map((view) => (
              <button
                key={view.id}
                onClick={() => onApply(view)}
                className="rounded-full border border-white/10 bg-gray-950/60 px-3 py-1 text-xs text-gray-300 hover:border-komgaPrimary hover:text-komgaPrimary transition-colors"
              >
                {view.name}
              </button>
            ))}
          </div>
        )}
      </div>

      {open && (
        <div className="mt-3 space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <input
              value={draftName}
              onChange={(e) => setDraftName(e.target.value)}
              placeholder={t('home.smartFilters.namePlaceholder')}
              disabled={!hasAnyFilter}
              className="flex-1 min-w-[180px] rounded-lg border border-white/10 bg-gray-950/60 px-3 py-1.5 text-sm text-white outline-hidden placeholder:text-gray-500 focus:border-komgaPrimary disabled:opacity-50"
            />
            <button
              onClick={() => {
                onSave(draftName);
                setDraftName('');
              }}
              disabled={!hasAnyFilter}
              className="inline-flex items-center gap-1.5 rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 px-3 py-1.5 text-sm font-medium text-komgaPrimary hover:bg-komgaPrimary/20 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Save className="h-4 w-4" />
              {t('home.smartFilters.save')}
            </button>
          </div>
          {views.length === 0 ? (
            <p className="text-xs text-gray-500">{t('home.smartFilters.empty')}</p>
          ) : (
            <ul className="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-3">
              {views.map((view) => (
                <li
                  key={view.id}
                  className="flex items-center justify-between gap-2 rounded-lg border border-white/10 bg-gray-950/40 px-3 py-1.5"
                >
                  <button
                    onClick={() => onApply(view)}
                    className="flex-1 truncate text-left text-sm text-gray-200 hover:text-komgaPrimary transition-colors"
                  >
                    {view.name}
                  </button>
                  <button
                    onClick={() => onDelete(view.id)}
                    className="rounded-sm p-1 text-gray-500 hover:bg-red-500/10 hover:text-red-400"
                    aria-label={t('home.smartFilters.delete')}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
