/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

import { CheckCircle2, FolderOpen } from 'lucide-react';
import type { VolumeItem } from './hooks/useSeriesVolumes';
import { useI18n } from '../../i18n/LocaleProvider';

interface SeriesVolumeGridProps {
  volumes: VolumeItem[];
  isSelectionMode: boolean;
  selectedVolumes: string[];
  onToggleVolumeSelection: (name: string) => void;
  onCardClick: (volumeName: string) => void;
  onQuickToggleVolumeRead: (volume: VolumeItem, makeRead: boolean) => void;
  seriesUpdatedAt?: string;
}

export function SeriesVolumeGrid({
  volumes,
  isSelectionMode,
  selectedVolumes,
  onToggleVolumeSelection,
  onCardClick,
  onQuickToggleVolumeRead,
  seriesUpdatedAt,
}: SeriesVolumeGridProps) {
  const { t } = useI18n();
  if (volumes.length === 0) return null;

  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4 sm:gap-6">
      {volumes.map((volume) => {
        const selected = selectedVolumes.includes(volume.name);
        const isFullyRead = volume.total_pages > 0 && volume.read_pages >= volume.total_pages;
        const progressPct = volume.total_pages > 0 ? Math.min(100, (volume.read_pages / volume.total_pages) * 100) : 0;
        const coverSrc =
          volume.cover_path?.Valid && volume.cover_path?.String && volume.cover_book_id
            ? `/api/covers/${volume.cover_book_id}${seriesUpdatedAt ? `?v=${new Date(seriesUpdatedAt).getTime()}` : ''}`
            : null;

        return (
          <div
            key={volume.name}
            onClick={() => {
              if (isSelectionMode) {
                onToggleVolumeSelection(volume.name);
              } else {
                onCardClick(volume.name);
              }
            }}
            className={`group flex flex-col rounded-2xl overflow-hidden bg-gray-950/40 backdrop-blur-md border ${
              selected
                ? 'border-komgaPrimary ring-2 ring-komgaPrimary shadow-[0_0_20px_rgba(var(--rgb-komga-primary),0.3)]'
                : 'border-white/5 hover:border-white/20 hover:-translate-y-1.5 hover:shadow-[0_20px_40px_-15px_rgba(0,0,0,0.7)]'
            } transition-all duration-500 cursor-pointer`}
          >
            <div className="aspect-3/4 w-full bg-gray-900/50 border-b border-white/5 flex items-center justify-center relative overflow-hidden">
              {isSelectionMode && (
                <div className="absolute top-2 left-2 z-30">
                  <div
                    className={`w-5 h-5 rounded-full border-2 flex items-center justify-center transition-colors ${
                      selected ? 'bg-komgaPrimary border-komgaPrimary' : 'bg-black/50 border-gray-400'
                    }`}
                  >
                    {selected && <span className="text-white text-xs font-bold leading-none select-none">&#10003;</span>}
                  </div>
                </div>
              )}
              {coverSrc ? (
                <>
                  <img
                    src={coverSrc}
                    className="absolute inset-0 w-full h-full object-cover transition-transform duration-700 group-hover:scale-110"
                    alt={volume.name}
                    loading="lazy"
                  />
                  <div className="absolute inset-0 ring-1 ring-inset ring-white/10 pointer-events-none transition-opacity group-hover:opacity-50" />
                  <div className="absolute inset-0 bg-linear-to-t from-gray-950/80 via-transparent to-gray-950/20 opacity-0 group-hover:opacity-100 transition-opacity duration-500 pointer-events-none" />
                </>
              ) : (
                <FolderOpen className="w-12 h-12 text-gray-700 opacity-50 group-hover:text-komgaPrimary transition-colors relative z-10" />
              )}

              {!isSelectionMode && (
                <div className="absolute top-2 right-2 z-30 flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      onQuickToggleVolumeRead(volume, !isFullyRead);
                    }}
                    className="p-1.5 rounded-full bg-black/60 border border-white/10 text-white/40 hover:text-green-400 hover:bg-green-400/20 hover:border-green-400/40 transition-colors backdrop-blur-sm"
                    title={isFullyRead ? t('series.content.markVolumeUnread') : t('series.content.markVolumeRead')}
                  >
                    <CheckCircle2 className={`w-4 h-4 ${isFullyRead ? 'text-green-400 fill-green-400/20' : ''}`} />
                  </button>
                </div>
              )}
            </div>

            <div className="p-3 sm:p-4 flex flex-col flex-1">
              <h4 className="font-bold text-sm sm:text-base text-white line-clamp-2 leading-tight group-hover:text-komgaPrimary transition-colors" title={volume.name}>
                {volume.name}
              </h4>
              
              <div className="mt-auto pt-3 flex items-center justify-between">
                <div className="text-[10px] sm:text-xs font-medium text-gray-400">
                  {t('series.content.bookCount', { count: volume.books.length })}
                </div>
                
                {volume.total_pages > 0 && (
                  <div className="flex items-center gap-2">
                    <span className={`text-[10px] sm:text-xs font-bold ${isFullyRead ? 'text-green-400' : 'text-komgaPrimary'}`}>
                      {Math.round(progressPct)}%
                    </span>
                  </div>
                )}
              </div>
              
              {volume.total_pages > 0 && (
                <div className="mt-2 h-1 w-full bg-white/5 rounded-full overflow-hidden">
                  <div
                    className={`h-full ${isFullyRead ? 'bg-green-500' : 'bg-komgaPrimary'} transition-all duration-700 ease-out`}
                    style={{ width: `${progressPct}%` }}
                  />
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
