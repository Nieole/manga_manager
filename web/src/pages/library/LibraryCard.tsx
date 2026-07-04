/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { Heart, ImageIcon, RefreshCw, Sparkles } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import type { ExternalSeriesStatus, Series } from './types';

interface LibraryCardProps {
  series: Series;
  isSelectionMode: boolean;
  isSelected: boolean;
  rescanning: boolean;
  scrapingActive: boolean;
  scrapeMenuOpen: boolean;
  onCardClick: (series: Series) => void; // 多选 / 默认导航
  onToggleFavorite: (event: React.MouseEvent, series: Series) => void;
  onRescan: (event: React.MouseEvent, series: Series) => void;
  onOpenScrapeMenu: (series: Series) => void;
  onCloseScrapeMenu: () => void;
  onChooseScrapeProvider: (series: Series, provider: 'bangumi' | 'llm') => void;
  externalStatus?: ExternalSeriesStatus;
  externalSessionActive: boolean;
  /** 长按 / 右键打开操作面板（暂未实现，预留） */
  onLongPress?: (series: Series) => void;
}

const LONG_PRESS_MS = 450;

export function LibraryCard({
  series: s,
  isSelectionMode,
  isSelected,
  rescanning,
  scrapingActive,
  scrapeMenuOpen,
  onCardClick,
  onToggleFavorite,
  onRescan,
  onOpenScrapeMenu,
  onCloseScrapeMenu,
  onChooseScrapeProvider,
  externalStatus,
  externalSessionActive,
  onLongPress,
}: LibraryCardProps) {
  const { t, formatNumber } = useI18n();
  const [pressing, setPressing] = useState(false);
  const longPressTimer = useRef<number | null>(null);

  const externalTotal = externalStatus?.external_total_count ?? s.actual_book_count ?? 0;
  const externalMatched = externalStatus?.external_match_count ?? 0;
  const externalSyncStatus =
    externalStatus?.external_sync_status ??
    (externalMatched > 0
      ? externalMatched >= externalTotal && externalTotal > 0
        ? 'complete'
        : 'partial'
      : 'missing');
  const externalPercent = externalTotal > 0 ? Math.min(100, Math.round((externalMatched / externalTotal) * 100)) : 0;
  const externalStatusLabel =
    externalSyncStatus === 'complete'
      ? t('home.external.cardComplete')
      : externalSyncStatus === 'partial'
        ? t('home.external.cardPartial')
        : t('home.external.cardMissing');
  const externalStatusClass =
    externalSyncStatus === 'complete'
      ? 'bg-emerald-500/10 text-emerald-300 border border-emerald-500/20'
      : externalSyncStatus === 'partial'
        ? 'bg-amber-500/10 text-amber-300 border border-amber-500/20'
        : 'bg-gray-800/60 text-gray-400 border border-white/10';

  const totalPagesRaw = s.total_pages?.Valid ? s.total_pages.Float64 : 0;
  const readCount = s.read_count || 0;
  const showProgress = readCount > 0 && totalPagesRaw > 0;
  const progressPercent = showProgress ? Math.min(100, (readCount / totalPagesRaw) * 100) : 0;
  const fullyRead = showProgress && readCount >= totalPagesRaw;

  const lastReadPage = s.last_read_page?.Valid ? s.last_read_page.Int64 : 0;
  const lastReadAtValid = s.last_read_at?.Valid ?? false;
  const showLastReadBadge = lastReadAtValid && lastReadPage > 0 && !fullyRead;
  const lastReadBadgeLabel = showLastReadBadge
    ? totalPagesRaw > 0
      ? t('home.card.lastReadAtPageOfTotal', { page: lastReadPage, total: formatNumber(totalPagesRaw) })
      : t('home.card.lastReadAtPage', { page: lastReadPage })
    : '';

  const cardClass = `group relative rounded-xl bg-komgaSurface border ${
    isSelected
      ? 'border-komgaPrimary ring-2 ring-komgaPrimary shadow-lg shadow-komgaPrimary/20'
      : 'border-gray-800 hover:border-komgaPrimary/50 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10'
  } transition-all duration-300 cursor-pointer block h-fit ${scrapeMenuOpen ? 'z-100' : 'hover:z-40'} ${pressing ? 'scale-[0.98]' : ''}`;

  const handleClick = (event: React.MouseEvent) => {
    if (isSelectionMode) {
      event.preventDefault();
      onCardClick(s);
    } else {
      onCardClick(s);
    }
  };

  const startLongPress = () => {
    if (!onLongPress) return;
    setPressing(true);
    longPressTimer.current = window.setTimeout(() => {
      onLongPress(s);
      longPressTimer.current = null;
    }, LONG_PRESS_MS);
  };
  const cancelLongPress = () => {
    setPressing(false);
    if (longPressTimer.current !== null) {
      window.clearTimeout(longPressTimer.current);
      longPressTimer.current = null;
    }
  };

  return (
    <Link
      to={`/series/${s.id}`}
      onClick={handleClick}
      onPointerDown={startLongPress}
      onPointerUp={cancelLongPress}
      onPointerLeave={cancelLongPress}
      onPointerCancel={cancelLongPress}
      onContextMenu={(event) => {
        if (!onLongPress) return;
        event.preventDefault();
        onLongPress(s);
      }}
      className={cardClass}
    >
      <div className="absolute inset-x-0 top-0 p-3 z-20 flex justify-between items-start pointer-events-none">
        {s.rating?.Valid && s.rating.Float64 > 0 && (
          <span className="flex items-center text-xs font-bold text-yellow-400 bg-black/70 px-1.5 py-0.5 rounded-sm backdrop-blur-sm border border-yellow-400/20 shadow-md pointer-events-none">
            ★ {s.rating.Float64.toFixed(1)}
          </span>
        )}
        {!isSelectionMode && (
          <div className="flex gap-1.5 ml-auto pointer-events-auto">
            <div className="relative">
              <button
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  if (scrapeMenuOpen) onCloseScrapeMenu();
                  else onOpenScrapeMenu(s);
                }}
                disabled={scrapingActive}
                className="p-1.5 rounded-full backdrop-blur-sm border shadow-md transition-all bg-black/60 border-white/10 text-white/40 hover:text-purple-400 hover:bg-purple-400/20 hover:border-purple-400/40 opacity-0 group-hover:opacity-100 disabled:opacity-100 disabled:cursor-not-allowed"
                title={t('series.scrape.action')}
              >
                <Sparkles className={`w-3.5 h-3.5 ${scrapingActive ? 'animate-pulse text-purple-400' : ''}`} />
              </button>
              {scrapeMenuOpen && !scrapingActive && (
                <div
                  className="fixed inset-0 z-40 cursor-default"
                  onPointerDown={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    onCloseScrapeMenu();
                  }}
                />
              )}
            </div>
            <button
              onClick={(e) => onRescan(e, s)}
              disabled={rescanning}
              className="p-1.5 rounded-full backdrop-blur-sm border shadow-md transition-all bg-black/60 border-white/10 text-white/40 hover:text-blue-400 hover:bg-blue-400/20 hover:border-blue-400/40 opacity-0 group-hover:opacity-100 disabled:opacity-100 disabled:cursor-not-allowed"
              title={t('home.seriesRescan')}
            >
              <RefreshCw className={`w-3.5 h-3.5 ${rescanning ? 'animate-spin text-blue-400' : ''}`} />
            </button>
            <button
              onClick={(e) => onToggleFavorite(e, s)}
              className={`p-1.5 rounded-full backdrop-blur-sm border shadow-md transition-all ${
                s.is_favorite
                  ? 'bg-red-500/20 border-red-500/40 text-red-500'
                  : 'bg-black/60 border-white/10 text-white/40 hover:text-red-400 hover:bg-red-400/20 hover:border-red-400/40 opacity-0 group-hover:opacity-100'
              }`}
            >
              <Heart className={`w-3.5 h-3.5 ${s.is_favorite ? 'fill-current' : ''}`} />
            </button>
          </div>
        )}
      </div>
      <div className="aspect-[1/1.4] w-full bg-gray-900 flex items-center justify-center relative overflow-hidden rounded-t-xl">
        {isSelectionMode && (
          <div className="absolute top-2 left-2 z-30">
            <div
              className={`w-5 h-5 rounded-full border-2 flex items-center justify-center transition-colors ${
                isSelected ? 'bg-komgaPrimary border-komgaPrimary' : 'bg-black/50 border-gray-400'
              }`}
            >
              {isSelected && <span className="text-white text-xs font-bold leading-none select-none">✓</span>}
            </div>
          </div>
        )}
        {s.cover_path?.Valid && s.cover_path?.String ? (
          <img
            /* cover_path 内容寻址（基于 bookHash），封面内容变化时路径即变化，天然就是稳定的缓存键。
               此前追加 ?v=updated_at 会让任何元数据变更/扫描（updated_at 变但封面未变）都失效整库封面缓存并重新下载。 */
            src={`/api/thumbnails/${s.cover_path.String}`}
            alt={t('common.cover')}
            loading="lazy"
            className="absolute inset-0 w-full h-full object-cover transition-transform duration-500 group-hover:scale-105"
          />
        ) : (
          <ImageIcon className="h-12 w-12 text-gray-700 opacity-50 transition-opacity group-hover:opacity-100 relative z-10" />
        )}
        {showLastReadBadge && (
          <div className="absolute right-2 bottom-12 z-20 pointer-events-none">
            <span
              className="rounded-md border border-komgaPrimary/40 bg-black/70 px-2 py-0.5 text-[10px] font-semibold text-komgaPrimary shadow-xs backdrop-blur-xs"
              title={lastReadBadgeLabel}
            >
              {lastReadBadgeLabel}
            </span>
          </div>
        )}
        <div className="absolute inset-x-0 bottom-0 bg-linear-to-t from-black/95 via-black/60 to-transparent p-3 pt-8 z-10 pointer-events-none">
          <div className="flex justify-between text-[11px] font-medium text-gray-300">
            <span>
              {s.volume_count > 0
                ? t('home.seriesCountsWithVolumes', { volumes: s.volume_count, books: s.actual_book_count })
                : t('home.seriesCountsBooksOnly', { books: s.actual_book_count })}
            </span>
            <span>{formatNumber(totalPagesRaw)} P</span>
          </div>
          {showProgress && (
            <div className="w-full h-1 bg-gray-700/60 rounded-full mt-1.5 overflow-hidden">
              <div
                className={`h-full ${fullyRead ? 'bg-green-500' : 'bg-komgaPrimary'}`}
                style={{ width: `${progressPercent}%` }}
              />
            </div>
          )}
        </div>
      </div>
      <div className="p-3 rounded-b-xl">
        <div>
          <h4 className="text-sm font-bold text-gray-200 line-clamp-1 leading-tight group-hover:text-komgaPrimary transition-colors mb-1.5">
            {s.title?.Valid ? s.title.String : s.name}
          </h4>
          {s.summary?.Valid && (
            <p className="text-[11px] text-gray-500 line-clamp-2 leading-tight opacity-70">{s.summary.String}</p>
          )}
        </div>
        {externalSessionActive && (
          <div className="mt-3 rounded-lg border border-gray-800 bg-gray-950/70 px-3 py-2">
            <div className="flex items-center justify-between gap-2">
              <span className={`rounded-full px-2 py-0.5 text-[10px] font-bold ${externalStatusClass}`}>{externalStatusLabel}</span>
              <span className="text-[11px] font-medium text-gray-400">
                {externalMatched}/{externalTotal}
              </span>
            </div>
            <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-gray-800">
              <div
                className={`h-full transition-all ${
                  externalSyncStatus === 'complete'
                    ? 'bg-emerald-400'
                    : externalSyncStatus === 'partial'
                      ? 'bg-amber-400'
                      : 'bg-gray-500'
                }`}
                style={{ width: `${externalPercent}%` }}
              />
            </div>
          </div>
        )}
      </div>

      {scrapeMenuOpen && !scrapingActive && (
        <div
          className="absolute right-3 top-12 z-50 w-44 rounded-lg border border-gray-800 bg-gray-950/95 shadow-2xl backdrop-blur-sm"
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
          }}
        >
          <button
            type="button"
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              onChooseScrapeProvider(s, 'bangumi');
            }}
            className="block w-full text-center px-2 py-3 text-[13px] font-semibold text-gray-200 hover:bg-komgaPrimary hover:text-white transition-colors cursor-pointer truncate"
          >
            {t('series.header.bangumiRecommended')}
          </button>
          <button
            type="button"
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              onChooseScrapeProvider(s, 'llm');
            }}
            className="block w-full text-center px-2 py-3 text-[13px] font-semibold text-gray-200 hover:bg-komgaPrimary hover:text-white transition-colors border-t border-gray-800 cursor-pointer truncate"
          >
            {t('series.header.ollama')}
          </button>
        </div>
      )}
    </Link>
  );
}
