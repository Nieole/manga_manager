/**
 * 业务说明：本文件是前端合集页面左栏列表组件，负责按「全部/手工/智能」分页签过滤展示合集列表、
 * 高亮选中项、并在悬停时提供删除入口。智能合集额外展示其筛选条件芯片。
 * 维护时应关注选中态与父组件的同步、删除确认交由父组件处理。
 */

import { FolderHeart, SlidersHorizontal, Trash2, ChevronRight } from 'lucide-react';
import { SmartFilterChips } from './SmartFilterChips';
import type { Collection, TFunc } from './types';

interface CollectionListPanelProps {
  collections: Collection[];
  kindTab: 'all' | 'manual' | 'smart';
  onKindTabChange: (tab: 'all' | 'manual' | 'smart') => void;
  selected: Collection | null;
  onSelect: (c: Collection) => void;
  onRequestDeleteCollection: (c: Collection) => void;
  onRequestDeleteSmart: (c: Collection) => void;
  t: TFunc;
}

export function CollectionListPanel({
  collections,
  kindTab,
  onKindTabChange,
  selected,
  onSelect,
  onRequestDeleteCollection,
  onRequestDeleteSmart,
  t,
}: CollectionListPanelProps) {
  const filtered = collections.filter((c) =>
    kindTab === 'all' ? true : kindTab === 'manual' ? c.kind === 'collection' : c.kind === 'smart',
  );

  return (
    <div className="lg:col-span-1 space-y-3">
      <div className="flex gap-1 rounded-xl border border-gray-800 bg-gray-950/60 p-1">
        {(['all', 'manual', 'smart'] as const).map((key) => {
          const count = key === 'all'
            ? collections.length
            : key === 'manual'
              ? collections.filter((c) => c.kind === 'collection').length
              : collections.filter((c) => c.kind === 'smart').length;
          const isActive = kindTab === key;
          return (
            <button
              key={key}
              type="button"
              onClick={() => onKindTabChange(key)}
              className={`flex-1 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                isActive ? 'bg-komgaPrimary text-white' : 'text-gray-400 hover:bg-gray-800/60 hover:text-white'
              }`}
            >
              {t(`collections.kindTab.${key}`)}
              <span className={`ml-1.5 text-[10px] ${isActive ? 'text-white/80' : 'text-gray-500'}`}>{count}</span>
            </button>
          );
        })}
      </div>
      {filtered.length === 0 ? (
        <div className="text-center py-16 text-gray-600">
          <FolderHeart className="w-12 h-12 mx-auto mb-3 opacity-50" />
          <p className="text-sm">{t('collections.empty')}</p>
          <p className="text-xs mt-1">{t('collections.emptyHint')}</p>
        </div>
      ) : (
        filtered.map((c) => (
          <div
            key={c.view_id}
            onClick={() => onSelect(c)}
            className={`group flex items-center justify-between p-4 rounded-xl border cursor-pointer transition-all duration-200 ${selected?.view_id === c.view_id
                ? 'bg-komgaPrimary/10 border-komgaPrimary/40 shadow-lg shadow-komgaPrimary/5'
                : 'bg-komgaSurface border-gray-800 hover:border-gray-700 hover:bg-gray-900'
              }`}
          >
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2">
                {c.kind === 'smart' ? (
                  <SlidersHorizontal className={`w-4 h-4 shrink-0 ${selected?.view_id === c.view_id ? 'text-cyan-300' : 'text-gray-600'}`} />
                ) : (
                  <FolderHeart className={`w-4 h-4 shrink-0 ${selected?.view_id === c.view_id ? 'text-komgaPrimary' : 'text-gray-600'}`} />
                )}
                <p className="font-medium text-white truncate">{c.name}</p>
              </div>
              <div className="mt-1 ml-6 flex flex-wrap items-center gap-2">
                <p className="text-xs text-gray-500">{t('common.seriesCount', { count: c.series_count })}</p>
                <span className="rounded-full border border-white/10 bg-white/5 px-2 py-0.5 text-[10px] text-gray-400">{t(`collections.source.${c.source_type || 'manual'}`)}</span>
              </div>
              {c.kind === 'smart' && <SmartFilterChips collection={c} t={t} />}
            </div>
            <div className="flex items-center gap-1.5 shrink-0">
              {c.kind === 'collection' && (
                <button
                  onClick={(e) => { e.stopPropagation(); onRequestDeleteCollection(c); }}
                  className="p-1.5 rounded-lg text-gray-600 hover:text-red-400 hover:bg-red-900/20 transition opacity-0 group-hover:opacity-100"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              )}
              {c.kind === 'smart' && (
                <button
                  onClick={(e) => { e.stopPropagation(); onRequestDeleteSmart(c); }}
                  className="p-1.5 rounded-lg text-gray-600 hover:text-red-400 hover:bg-red-900/20 transition opacity-0 group-hover:opacity-100"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              )}
              <ChevronRight className={`w-4 h-4 transition-colors ${selected?.view_id === c.view_id ? 'text-komgaPrimary' : 'text-gray-700'}`} />
            </div>
          </div>
        ))
      )}
    </div>
  );
}
