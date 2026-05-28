import { useCallback, useState } from 'react';
import axios from 'axios';
import type { SearchResult, Series } from '../types';

function getApiErrorMessage(error: unknown, fallback: string) {
  if (axios.isAxiosError(error)) return error.response?.data?.error || error.message || fallback;
  if (error instanceof Error) return error.message;
  return fallback;
}

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

      if (providerKey === 'bangumi') {
        setIsScraping(true);
        try {
          const res = await axios.get(`/api/series/${seriesId}/scrape-search?provider=${providerKey}`);
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
        const res = await axios.post(`/api/series/${seriesId}/scrape`, { provider: providerKey });
        if (res.data.scraped) {
          showToast(`[${res.data.provider}] ${res.data.message}`, 'success');
          await reload();
        } else {
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
        const res = await axios.post(`/api/series/${seriesId}/scrape-apply?provider=${searchProvider}`, metadata);
        if (res.data.success) {
          showToast(
            res.data.queued
              ? t('series.toast.metadataReviewQueued', { count: res.data.field_count || 0 })
              : t('series.toast.noMetadataReviewChanges'),
            'success',
          );
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
        const res = await axios.get(
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
