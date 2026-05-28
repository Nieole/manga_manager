import { useEffect, useRef } from 'react';
import { useI18n } from '../../i18n/LocaleProvider';
import { LibraryCard } from './LibraryCard';
import type { ExternalSeriesStatus, Series } from './types';

interface LibraryGridProps {
  series: Series[];
  loading: boolean;
  isSelectionMode: boolean;
  selectedSeriesIds: number[];
  rescanningId: number | null;
  scrapingSeriesId: number | null;
  scrapeMenuOpenId: number | null;
  externalSeriesMap: Record<number, ExternalSeriesStatus>;
  externalSessionActive: boolean;
  hasMore: boolean;
  paginationMode: 'paged' | 'infinite';
  onCardClick: (series: Series) => void;
  onToggleFavorite: (event: React.MouseEvent, series: Series) => void;
  onRescan: (event: React.MouseEvent, series: Series) => void;
  onOpenScrapeMenu: (series: Series) => void;
  onCloseScrapeMenu: () => void;
  onChooseScrapeProvider: (series: Series, provider: 'bangumi' | 'ollama') => void;
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
  onCardClick,
  onToggleFavorite,
  onRescan,
  onOpenScrapeMenu,
  onCloseScrapeMenu,
  onChooseScrapeProvider,
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

  return (
    <div className={`relative transition-opacity duration-300 ${loading ? 'opacity-40 pointer-events-none' : 'opacity-100'}`}>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-4 sm:gap-6 min-h-[600px] items-start">
        {series.map((s) => (
          <LibraryCard
            key={s.id}
            series={s}
            isSelectionMode={isSelectionMode}
            isSelected={selectedSeriesIds.includes(s.id)}
            rescanning={rescanningId === s.id}
            scrapingActive={scrapingSeriesId === s.id}
            scrapeMenuOpen={scrapeMenuOpenId === s.id}
            externalStatus={externalSeriesMap[s.id]}
            externalSessionActive={externalSessionActive}
            onCardClick={onCardClick}
            onToggleFavorite={onToggleFavorite}
            onRescan={onRescan}
            onOpenScrapeMenu={onOpenScrapeMenu}
            onCloseScrapeMenu={onCloseScrapeMenu}
            onChooseScrapeProvider={onChooseScrapeProvider}
          />
        ))}
      </div>
      {paginationMode === 'infinite' && (
        <div ref={sentinelRef} className="h-12 w-full" aria-hidden="true" />
      )}
    </div>
  );
}
