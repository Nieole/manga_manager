/**
 * 业务说明：本文件是前端合集页面右栏详情组件，展示当前选中合集的标题、来源、编辑/快照入口，
 * 以及其系列成员网格（手工合集可移除成员，点击封面进入系列详情）。
 * 维护时应关注选中为空时的占位、手工与智能合集操作入口的差异。
 */

import { Pencil, Camera, BookOpen, X, Search } from 'lucide-react';
import type { Collection, CollectionSeriesItem, TFunc } from './types';

interface CollectionDetailPanelProps {
  selected: Collection | null;
  seriesItems: CollectionSeriesItem[];
  onEditCollection: () => void;
  onOpenSmartEdit: () => void;
  onOpenSnapshot: () => void;
  onRemoveSeries: (seriesId: number) => void;
  onOpenSeries: (seriesId: number) => void;
  t: TFunc;
}

export function CollectionDetailPanel({
  selected,
  seriesItems,
  onEditCollection,
  onOpenSmartEdit,
  onOpenSnapshot,
  onRemoveSeries,
  onOpenSeries,
  t,
}: CollectionDetailPanelProps) {
  return (
    <div className="lg:col-span-2">
      {selected ? (
        <div>
          <div className="flex items-center justify-between mb-4">
            <div className="flex items-start justify-between w-full">
              <div>
                <div className="flex items-center gap-2">
                  <h2 className="text-lg font-semibold text-white">{selected.name}</h2>
                  <span className="rounded-full border border-white/10 bg-white/5 px-2 py-0.5 text-[10px] text-gray-400">{t(`collections.source.${selected.source_type || 'manual'}`)}</span>
                  {selected.kind === 'collection' && (
                    <button
                      onClick={onEditCollection}
                      className="p-1 rounded-md text-gray-500 hover:text-white hover:bg-gray-800 transition-colors"
                      title={t('common.edit')}
                    >
                      <Pencil className="w-3.5 h-3.5" />
                    </button>
                  )}
                  {selected.kind === 'smart' && (
                    <>
                      <button onClick={onOpenSmartEdit} className="p-1 rounded-md text-gray-500 hover:text-white hover:bg-gray-800 transition-colors" title={t('common.edit')}>
                        <Pencil className="w-3.5 h-3.5" />
                      </button>
                      <button onClick={onOpenSnapshot} className="p-1 rounded-md text-gray-500 hover:text-white hover:bg-gray-800 transition-colors" title={t('collections.snapshot')}>
                        <Camera className="w-3.5 h-3.5" />
                      </button>
                    </>
                  )}
                </div>
                {selected.description && <p className="text-xs text-gray-500 mt-1">{selected.description}</p>}
              </div>
              <span className="text-xs text-gray-500 bg-gray-900 px-3 py-1 rounded-full">{t('common.seriesCount', { count: seriesItems.length })}</span>
            </div>
          </div>

          {seriesItems.length === 0 ? (
            <div className="text-center py-20 text-gray-600">
              <BookOpen className="w-10 h-10 mx-auto mb-3 opacity-40" />
              <p className="text-sm">{t('collections.noSeries')}</p>
              <p className="text-xs mt-1">{t('collections.noSeriesHint')}</p>
            </div>
          ) : (
            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
              {seriesItems.map((item) => {
                const coverUrl = item.cover_path?.Valid ? `/api/thumbnails/${item.cover_path.String}` : '';
                return (
                  <div key={item.series_id} className="group relative cursor-pointer" onClick={() => onOpenSeries(item.series_id)}>
                    <div className="aspect-2/3 rounded-xl overflow-hidden bg-gray-900 border border-gray-800 group-hover:border-komgaPrimary/40 transition-all shadow-lg">
                      {coverUrl ? (
                        <img src={coverUrl} alt={item.series_name} className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500" />
                      ) : (
                        <div className="w-full h-full flex items-center justify-center text-gray-700"><BookOpen className="w-8 h-8" /></div>
                      )}
                      {selected.kind === 'collection' && (
                        <button
                          onClick={(e) => { e.stopPropagation(); onRemoveSeries(item.series_id); }}
                          className="absolute top-2 right-2 p-1.5 rounded-full bg-black/70 text-white/60 hover:text-red-400 hover:bg-red-900/80 opacity-0 group-hover:opacity-100 transition-all"
                        >
                          <X className="w-3 h-3" />
                        </button>
                      )}
                    </div>
                    <p className="text-xs text-gray-300 mt-2 truncate group-hover:text-komgaPrimary transition-colors">{item.series_name}</p>
                    <p className="text-[10px] text-gray-600">{t('common.books', { count: item.book_count })}</p>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      ) : (
        <div className="flex items-center justify-center h-full min-h-[40vh] text-gray-600">
          <div className="text-center">
            <Search className="w-10 h-10 mx-auto mb-3 opacity-30" />
            <p className="text-sm">{t('collections.pickLeft')}</p>
          </div>
        </div>
      )}
    </div>
  );
}
