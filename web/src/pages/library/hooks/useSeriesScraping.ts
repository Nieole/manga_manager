import { useCallback, useRef, useState } from 'react';
import { apiClient } from '../../../api/client';
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
  // 取候选的世代号，startScrape 与 reSearch 共用：只有最新一次取到的结果可以写进弹窗状态。
  // 资料库页只禁用发起刮削的那张卡片，A 的请求在飞时用户仍可对 B 再点一次；A 迟到的响应
  // 若写了进来，弹窗就指向 B 而候选与对比列是 A 的，用户应用后会给 B 生成一条属于 A 的提案。
  const latestRequestIDRef = useRef(0);

  const closeScrapeModal = useCallback(() => {
    setShowScrapeModal(false);
    setScrapeSearchResults([]);
    setSelectedScrapeResult(null);
    setScrapingSeries(null);
    setScrapeSeriesDetail(null);
  }, []);

  const startScrape = useCallback(
    async (series: Series, providerKey: string) => {
      const requestID = latestRequestIDRef.current + 1;
      latestRequestIDRef.current = requestID;
      setScrapeMenuOpenId(null);
      setScrapingSeries(series);
      setScrapeProvider(providerKey);
      setScrapeModalSearchQuery(series.title?.Valid ? series.title.String : series.name);
      setIsScraping(true);
      try {
        const [seriesRes, tagsRes, searchRes] = await Promise.all([
          apiClient.get<DetailSeries>(`/api/series/${series.id}`),
          apiClient.get<MetaTag[]>(`/api/series/${series.id}/tags`).catch(() => ({ data: [] as MetaTag[] })),
          apiClient.get<{ results?: SearchResult[]; total?: number }>(
            `/api/series/${series.id}/scrape-search?provider=${providerKey}&q=${encodeURIComponent(series.title?.Valid ? series.title.String : series.name)}&offset=0`,
          ),
        ]);
        if (requestID !== latestRequestIDRef.current) return;
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
        if (requestID !== latestRequestIDRef.current) return;
        console.error('Failed to start scrape', err);
        onError('series.toast.scrapeFailed');
      } finally {
        if (requestID === latestRequestIDRef.current) setIsScraping(false);
      }
    },
    [onError],
  );

  const reSearch = useCallback(
    async (offset = 0) => {
      if (!scrapingSeries) return;
      const requestID = latestRequestIDRef.current + 1;
      latestRequestIDRef.current = requestID;
      setIsScraping(true);
      try {
        const res = await apiClient.get<{ results?: SearchResult[]; total?: number }>(
          `/api/series/${scrapingSeries.id}/scrape-search?provider=${scrapeProvider}&q=${encodeURIComponent(scrapeModalSearchQuery)}&offset=${offset}`,
        );
        if (requestID !== latestRequestIDRef.current) return;
        setScrapeSearchResults(res.data?.results || []);
        setScrapeTotal(res.data?.total || 0);
        setScrapeOffset(offset);
      } catch (err) {
        if (requestID !== latestRequestIDRef.current) return;
        console.error('Re-search failed', err);
        onError('series.toast.scrapeFailed');
      } finally {
        if (requestID === latestRequestIDRef.current) setIsScraping(false);
      }
    },
    [scrapingSeries, scrapeProvider, scrapeModalSearchQuery, onError],
  );

  const applyScrape = useCallback(
    async (metadata: Record<string, unknown>) => {
      if (!scrapingSeries) return;
      setIsScraping(true);
      try {
        const res = await apiClient.post<{ queued?: boolean; outcome?: string; message?: string }>(
          `/api/series/${scrapingSeries.id}/scrape-apply?provider=${scrapeProvider}`,
          metadata,
        );
        // 不入队（queued=false）的原因必须按 outcome 分别提示：已被拒绝过、字段全被锁、无变更，
        // 都不能笼统说成「队列里已有相同记录」。
        if (res.data?.queued === false) {
          const outcome = res.data?.outcome;
          if (outcome === 'rejected_before') {
            onError('series.toast.scrapeRejectedBefore');
          } else if (outcome === 'all_locked') {
            onError('series.toast.scrapeAllLocked');
          } else if (outcome === 'no_changes') {
            onError('series.toast.noMetadataReviewChanges');
          } else {
            onError('series.toast.scrapeDuplicate');
          }
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
