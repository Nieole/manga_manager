/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useCallback, useState } from 'react';
import axios from 'axios';
import type { MetaTag, SearchResult, Series as DetailSeries } from '../../series-detail/types';
import type { Series } from '../types';

interface UseSeriesScrapingParams {
  onSuccess: (seriesId: number) => void;
  onError: (msg: string) => void;
}

interface UseSeriesScrapingResult {
  scrapeProvider: string;
  scrapeModalSearchQuery: string;
  showScrapeModal: boolean;
  scrapeSearchResults: SearchResult[];
  selectedScrapeResult: SearchResult | null;
  scrapeTotal: number;
  scrapeOffset: number;
  isScraping: boolean;
  scrapingSeries: Series | null;
  scrapeSeriesDetail: DetailSeries | null;
  scrapeCurrentTags: MetaTag[];
  scrapeLockedFields: Set<string>;
  scrapeMenuOpenId: number | null;
  setScrapeMenuOpenId: (id: number | null) => void;
  setScrapeModalSearchQuery: (value: string) => void;
  setSelectedScrapeResult: (value: SearchResult | null) => void;
  closeScrapeModal: () => void;
  startScrape: (series: Series, providerKey: string) => Promise<void>;
  reSearch: (offset?: number) => Promise<void>;
  applyScrape: (metadata: Record<string, unknown>) => Promise<void>;
}

export function useSeriesScraping({ onSuccess, onError }: UseSeriesScrapingParams): UseSeriesScrapingResult {
  const [scrapeProvider, setScrapeProvider] = useState('');
  const [scrapeModalSearchQuery, setScrapeModalSearchQuery] = useState('');
  const [showScrapeModal, setShowScrapeModal] = useState(false);
  const [scrapeSearchResults, setScrapeSearchResults] = useState<SearchResult[]>([]);
  const [selectedScrapeResult, setSelectedScrapeResult] = useState<SearchResult | null>(null);
  const [scrapeTotal, setScrapeTotal] = useState(0);
  const [scrapeOffset, setScrapeOffset] = useState(0);
  const [isScraping, setIsScraping] = useState(false);
  const [scrapingSeries, setScrapingSeries] = useState<Series | null>(null);
  const [scrapeSeriesDetail, setScrapeSeriesDetail] = useState<DetailSeries | null>(null);
  const [scrapeCurrentTags, setScrapeCurrentTags] = useState<MetaTag[]>([]);
  const [scrapeLockedFields, setScrapeLockedFields] = useState<Set<string>>(new Set());
  const [scrapeMenuOpenId, setScrapeMenuOpenId] = useState<number | null>(null);

  const closeScrapeModal = useCallback(() => {
    setShowScrapeModal(false);
    setScrapeSearchResults([]);
    setSelectedScrapeResult(null);
    setScrapingSeries(null);
    setScrapeSeriesDetail(null);
  }, []);

  const startScrape = useCallback(
    async (series: Series, providerKey: string) => {
      setScrapeMenuOpenId(null);
      setScrapingSeries(series);
      setScrapeProvider(providerKey);
      setScrapeModalSearchQuery(series.title?.Valid ? series.title.String : series.name);
      setIsScraping(true);
      try {
        const [seriesRes, tagsRes, searchRes] = await Promise.all([
          axios.get<DetailSeries>(`/api/series/${series.id}`),
          axios.get<MetaTag[]>(`/api/series/${series.id}/tags`).catch(() => ({ data: [] as MetaTag[] })),
          axios.get<{ results?: SearchResult[]; total?: number }>(
            `/api/series/${series.id}/scrape-search?provider=${providerKey}&q=${encodeURIComponent(series.title?.Valid ? series.title.String : series.name)}&offset=0`,
          ),
        ]);
        setScrapeSeriesDetail(seriesRes.data);
        setScrapeCurrentTags(tagsRes.data || []);
        // locked_fields 已包含在 series 详情里（逗号分隔），与详情页解析方式一致，
        // 无需再请求独立的 /locked-fields 端点（该端点不存在，会产生 404）。
        const lockedRaw = seriesRes.data?.locked_fields;
        const lockedList = lockedRaw?.Valid && lockedRaw.String ? lockedRaw.String.split(',') : [];
        setScrapeLockedFields(new Set(lockedList));
        setScrapeSearchResults(searchRes.data?.results || []);
        setScrapeTotal(searchRes.data?.total || 0);
        setScrapeOffset(0);
        setShowScrapeModal(true);
      } catch (err) {
        console.error('Failed to start scrape', err);
        onError('series.toast.scrapeFailed');
      } finally {
        setIsScraping(false);
      }
    },
    [onError],
  );

  const reSearch = useCallback(
    async (offset = 0) => {
      if (!scrapingSeries) return;
      setIsScraping(true);
      try {
        const res = await axios.get<{ results?: SearchResult[]; total?: number }>(
          `/api/series/${scrapingSeries.id}/scrape-search?provider=${scrapeProvider}&q=${encodeURIComponent(scrapeModalSearchQuery)}&offset=${offset}`,
        );
        setScrapeSearchResults(res.data?.results || []);
        setScrapeTotal(res.data?.total || 0);
        setScrapeOffset(offset);
      } catch (err) {
        console.error('Re-search failed', err);
        onError('series.toast.scrapeFailed');
      } finally {
        setIsScraping(false);
      }
    },
    [scrapingSeries, scrapeProvider, scrapeModalSearchQuery, onError],
  );

  const applyScrape = useCallback(
    async (metadata: Record<string, unknown>) => {
      if (!scrapingSeries) return;
      setIsScraping(true);
      try {
        const res = await axios.post<{ queued?: boolean; message?: string }>(
          `/api/series/${scrapingSeries.id}/scrape-apply?provider=${scrapeProvider}`,
          metadata,
        );
        // 后端可能因待审核队列中已存在完全相同的记录而忽略本次提交（queued=false），
        // 此时应提示用户而非静默当作成功。
        if (res.data?.queued === false) {
          onError('series.toast.scrapeDuplicate');
        } else {
          onSuccess(scrapingSeries.id);
        }
        closeScrapeModal();
      } catch (err) {
        console.error('Apply scrape failed', err);
        onError('series.toast.applyMetadataFailed');
      } finally {
        setIsScraping(false);
      }
    },
    [scrapingSeries, scrapeProvider, onSuccess, closeScrapeModal, onError],
  );

  return {
    scrapeProvider,
    scrapeModalSearchQuery,
    showScrapeModal,
    scrapeSearchResults,
    selectedScrapeResult,
    scrapeTotal,
    scrapeOffset,
    isScraping,
    scrapingSeries,
    scrapeSeriesDetail,
    scrapeCurrentTags,
    scrapeLockedFields,
    scrapeMenuOpenId,
    setScrapeMenuOpenId,
    setScrapeModalSearchQuery,
    setSelectedScrapeResult,
    closeScrapeModal,
    startScrape,
    reSearch,
    applyScrape,
  };
}
