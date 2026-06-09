/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

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
