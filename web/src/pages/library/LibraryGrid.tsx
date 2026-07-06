/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useEffect, useRef } from 'react';
import { useI18n } from '../../i18n/LocaleProvider';
import { LibraryCard } from './LibraryCard';
import type { ExternalSeriesStatus, Series, ViewMode } from './types';
import type { ScrapeProvider } from '../../hooks/useScrapeProviders';

interface LibraryGridProps {
  series: Series[];
  loading: boolean;
  isSelectionMode: boolean;
  selectedSeriesIds: Set<number>;
  rescanningId: number | null;
  scrapingSeriesId: number | null;
  scrapeMenuOpenId: number | null;
  externalSeriesMap: Record<number, ExternalSeriesStatus>;
  externalSessionActive: boolean;
  hasMore: boolean;
  paginationMode: 'paged' | 'infinite';
  viewMode: ViewMode;
  onCardClick: (series: Series) => void;
  onToggleFavorite: (event: React.MouseEvent, series: Series) => void;
  onRescan: (event: React.MouseEvent, series: Series) => void;
  onOpenScrapeMenu: (series: Series) => void;
  onCloseScrapeMenu: () => void;
  onChooseScrapeProvider: (series: Series, provider: string) => void;
  scrapeProviders: ScrapeProvider[];
  onLoadMore: () => void;
}

export function LibraryGrid({
  series,
  loading,
  isSelectionMode,
  selectedSeriesIds,
  rescanningId,
  scrapingSeriesId,
  scrapeMenuOpenId,
  externalSeriesMap,
  externalSessionActive,
  hasMore,
  paginationMode,
  viewMode,
  onCardClick,
  onToggleFavorite,
  onRescan,
  onOpenScrapeMenu,
  onCloseScrapeMenu,
  onChooseScrapeProvider,
  scrapeProviders,
  onLoadMore,
}: LibraryGridProps) {
  const { t } = useI18n();
  const sentinelRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (paginationMode !== 'infinite') return;
    const node = sentinelRef.current;
    if (!node) return;
    if (typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting && hasMore && !loading) {
            onLoadMore();
            return;
          }
        }
      },
      { rootMargin: '300px 0px' },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [paginationMode, hasMore, loading, onLoadMore]);

  if (loading && series.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-40">
        <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-komgaPrimary mb-4" />
        <div className="text-gray-400 font-medium">{t('common.loading')}</div>
      </div>
    );
  }
  if (series.length === 0) {
    return <div className="text-center py-20 text-gray-500">{t('home.noMatches')}</div>;
  }

  // 网格：大图 minmax(180px)；紧凑 minmax(120px) 更多列；列表：单列纵向信息行。
  const containerClass =
    viewMode === 'list'
      ? 'flex flex-col gap-2 min-h-[600px]'
      : viewMode === 'compact'
        ? 'grid grid-cols-[repeat(auto-fill,minmax(110px,1fr))] sm:grid-cols-[repeat(auto-fill,minmax(130px,1fr))] gap-3 sm:gap-4 min-h-[600px] items-start'
        : 'grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-4 sm:gap-6 min-h-[600px] items-start';

  return (
    <div className={`relative transition-opacity duration-300 ${loading ? 'opacity-40 pointer-events-none' : 'opacity-100'}`}>
      <div className={containerClass}>
        {series.map((s) => (
          <LibraryCard
            key={s.id}
            series={s}
            isSelectionMode={isSelectionMode}
            isSelected={selectedSeriesIds.has(s.id)}
            rescanning={rescanningId === s.id}
            scrapingActive={scrapingSeriesId === s.id}
            scrapeMenuOpen={scrapeMenuOpenId === s.id}
            viewMode={viewMode}
            externalStatus={externalSeriesMap[s.id]}
            externalSessionActive={externalSessionActive}
            onCardClick={onCardClick}
            onToggleFavorite={onToggleFavorite}
            onRescan={onRescan}
            onOpenScrapeMenu={onOpenScrapeMenu}
            onCloseScrapeMenu={onCloseScrapeMenu}
            onChooseScrapeProvider={onChooseScrapeProvider}
            scrapeProviders={scrapeProviders}
          />
        ))}
      </div>
      {paginationMode === 'infinite' && (
        <div ref={sentinelRef} className="h-12 w-full" aria-hidden="true" />
      )}
    </div>
  );
}
