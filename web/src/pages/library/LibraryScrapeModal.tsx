import { SeriesSearchModal } from '../series-detail/SeriesSearchModal';
import type { Series as DetailSeries } from '../series-detail/types';

import type { Series } from './types';
import type { useSeriesScraping } from './hooks/useSeriesScraping';

type ScrapingState = ReturnType<typeof useSeriesScraping>;

interface LibraryScrapeModalProps {
  scraping: ScrapingState;
}

export function LibraryScrapeModal({ scraping }: LibraryScrapeModalProps) {
  if (!scraping.scrapingSeries) return null;

  return (
    <SeriesSearchModal
      open={scraping.showScrapeModal}
      onClose={scraping.closeScrapeModal}
      providerLabel={scraping.scrapeProvider === 'bangumi' ? 'Bangumi' : 'AI/LLM'}
      modalSearchQuery={scraping.scrapeModalSearchQuery}
      isScraping={scraping.isScraping}
      searchResults={scraping.scrapeSearchResults}
      currentOffset={scraping.scrapeOffset}
      searchTotal={scraping.scrapeTotal}
      currentSeries={scraping.scrapeSeriesDetail || (scraping.scrapingSeries as unknown as DetailSeries)}
      currentTags={scraping.scrapeCurrentTags}
      lockedFields={scraping.scrapeLockedFields}
      selectedResult={scraping.selectedScrapeResult}
      onSearchQueryChange={scraping.setScrapeModalSearchQuery}
      onReSearch={scraping.reSearch}
      onSelectMetadata={scraping.setSelectedScrapeResult}
      onApplyMetadata={(metadata) => scraping.applyScrape(metadata as unknown as Record<string, unknown>)}
    />
  );
}

export type { Series };
