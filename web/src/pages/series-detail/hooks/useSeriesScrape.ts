/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

import { useCallback, useState } from 'react';
import { apiClient } from '../../../api/client';
import { getApiErrorMessage } from '../../../api/client';
import type { SearchResult, Series } from '../types';


interface UseSeriesScrapeParams {
  seriesId: string | undefined;
  series: Series | null;
  reload: () => Promise<void>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesScrape({ seriesId, series, reload, showToast, t }: UseSeriesScrapeParams) {
  const [scrapeMenuOpen, setScrapeMenuOpen] = useState(false);
  const [isScraping, setIsScraping] = useState(false);

  const [showSearchModal, setShowSearchModal] = useState(false);
  const [searchProvider, setSearchProvider] = useState('');
  const [modalSearchQuery, setModalSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<SearchResult[]>([]);
  const [selectedSearchResult, setSelectedSearchResult] = useState<SearchResult | null>(null);
  const [currentOffset, setCurrentOffset] = useState(0);
  const [searchTotal, setSearchTotal] = useState(0);

  const closeSearchModal = useCallback(() => {
    setShowSearchModal(false);
    setSearchResults([]);
    setSelectedSearchResult(null);
  }, []);

  const handleScrape = useCallback(
    async (providerKey: string) => {
      if (!seriesId) return;
      setScrapeMenuOpen(false);

      // 除 LLM 直接推理落库外，所有外部数据源（Bangumi/AniList/MangaDex/MyAnimeList/Comic Vine）
      // 都走“搜索候选 → 人工挑选”弹窗流程，后端 scrape-search/apply 与 provider 无关。
      if (providerKey !== 'llm') {
        setIsScraping(true);
        try {
          const res = await apiClient.get(`/api/series/${seriesId}/scrape-search?provider=${providerKey}`);
          setSearchProvider(providerKey);
          setModalSearchQuery(series?.title?.Valid && series.title.String ? series.title.String : series?.name || '');
          setShowSearchModal(true);

          if (res.data.results && res.data.results.length > 0) {
            setSearchResults(res.data.results);
            setSelectedSearchResult(res.data.results[0]);
          } else {
            setSearchResults([]);
            setSelectedSearchResult(null);
            showToast(t('series.toast.autoMatchNotFound'), 'error');
          }
        } catch (err) {
          showToast(`${t('series.toast.searchFailed')}: ${getApiErrorMessage(err, t('series.toast.searchFailed'))}`, 'error');
        } finally {
          setIsScraping(false);
        }
        return;
      }

      setIsScraping(true);
      try {
        const res = await apiClient.post(`/api/series/${seriesId}/scrape`, { provider: providerKey });
        // 依据后端稳定的 outcome 结果码决定提示级别并本地化文案，不再解析中文 message 内容。
        const outcome = res.data.outcome as string | undefined;
        if (res.data.scraped || outcome === 'queued') {
          showToast(t('series.toast.metadataReviewQueued', { count: res.data.field_count ?? 0 }), 'success');
          await reload();
        } else if (outcome === 'no_changes') {
          showToast(t('series.toast.noMetadataReviewChanges'), 'success');
        } else if (outcome === 'duplicate_ignored') {
          showToast(t('series.toast.scrapeDuplicate'), 'success');
        } else {
          // outcome === 'not_found'，或老后端未返回 outcome 时兜底显示后端 message。
          showToast(res.data.message || t('series.toast.metadataNotFound'), 'error');
        }
      } catch (err) {
        showToast(`${t('series.toast.scrapeFailed')}: ${getApiErrorMessage(err, t('series.toast.scrapeFailed'))}`, 'error');
      } finally {
        setIsScraping(false);
      }
    },
    [seriesId, series, reload, showToast, t],
  );

  const handleApplyMetadata = useCallback(
    async (metadata: SearchResult) => {
      if (!seriesId) return;
      setShowSearchModal(false);
      setIsScraping(true);
      try {
        const res = await apiClient.post(`/api/series/${seriesId}/scrape-apply?provider=${searchProvider}`, metadata);
        if (res.data.success) {
          const outcome = res.data.outcome as string | undefined;
          let msg: string;
          if (res.data.queued || outcome === 'queued') {
            msg = t('series.toast.metadataReviewQueued', { count: res.data.field_count || 0 });
          } else if (outcome === 'duplicate_ignored') {
            msg = t('series.toast.scrapeDuplicate');
          } else if (outcome === 'no_changes') {
            msg = t('series.toast.noMetadataReviewChanges');
          } else {
            msg = res.data.message || t('series.toast.noMetadataReviewChanges');
          }
          showToast(msg, 'success');
          await reload();
        }
      } catch (err) {
        showToast(`${t('series.toast.applyMetadataFailed')}: ${getApiErrorMessage(err, t('series.toast.applyMetadataFailed'))}`, 'error');
      } finally {
        setIsScraping(false);
        setSearchResults([]);
        setSelectedSearchResult(null);
      }
    },
    [seriesId, searchProvider, reload, showToast, t],
  );

  const handleModalReSearch = useCallback(
    async (offset = 0) => {
      if (!seriesId || !modalSearchQuery.trim()) return;
      setIsScraping(true);
      setCurrentOffset(offset);
      try {
        const res = await apiClient.get(
          `/api/series/${seriesId}/scrape-search?provider=${searchProvider}&q=${encodeURIComponent(modalSearchQuery)}&offset=${offset}`,
        );
        if (res.data.results && res.data.results.length > 0) {
          setSearchResults(res.data.results);
          setSelectedSearchResult(res.data.results[0]);
          setSearchTotal(res.data.total || 0);
          showToast(t('series.toast.searchFound', { count: res.data.results.length }), 'success');
        } else {
          setSearchResults([]);
          setSelectedSearchResult(null);
          setSearchTotal(0);
          showToast(t('series.toast.searchNoResult'), 'error');
        }
      } catch (err) {
        showToast(`${t('series.toast.searchFailed')}: ${getApiErrorMessage(err, t('series.toast.searchFailed'))}`, 'error');
      } finally {
        setIsScraping(false);
      }
    },
    [seriesId, searchProvider, modalSearchQuery, showToast, t],
  );

  return {
    scrapeMenuOpen,
    setScrapeMenuOpen,
    isScraping,
    showSearchModal,
    closeSearchModal,
    searchProvider,
    modalSearchQuery,
    setModalSearchQuery,
    searchResults,
    selectedSearchResult,
    setSelectedSearchResult,
    currentOffset,
    searchTotal,
    handleScrape,
    handleApplyMetadata,
    handleModalReSearch,
  };
}
