import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useOutletContext, useParams } from 'react-router-dom';

import AddToCollectionModal from '../../components/AddToCollectionModal';
import { useToast } from '../../components/ToastProvider';
import { useI18n } from '../../i18n/LocaleProvider';

import { LibraryFilterBar } from './LibraryFilterBar';
import { LibraryGrid } from './LibraryGrid';
import { LibraryHeader } from './LibraryHeader';
import { LibraryPagination } from './LibraryPagination';
import { LibrarySavedViews } from './LibrarySavedViews';
import { LibrarySelectionBar } from './LibrarySelectionBar';
import { ExternalLibraryDrawer } from './ExternalLibraryDrawer';
import { LibraryScrapeModal } from './LibraryScrapeModal';
import { TransferConfirmModal } from './TransferConfirmModal';
import { type PaginationMode } from './types';
import { useLibraryFilters, supportsCursorPagination } from './hooks/useLibraryFilters';
import { useLibrarySeries } from './hooks/useLibrarySeries';
import { useLibrarySelection } from './hooks/useLibrarySelection';
import { useLibraryKeyboard } from './hooks/useLibraryKeyboard';
import { useExternalLibrary } from './hooks/useExternalLibrary';
import { useSeriesScraping } from './hooks/useSeriesScraping';
import { useSmartFilters } from './hooks/useSmartFilters';
import { useLibraryFilterOptions } from './hooks/useLibraryFilterOptions';
import { useLibraryCardActions } from './hooks/useLibraryCardActions';
import { useLibraryTransfer } from './hooks/useLibraryTransfer';

const ALL_STATUSES = ['completed', 'ongoing', 'cancelled', 'hiatus'];
const PAGINATION_MODE_KEY = 'manga_manager_pagination_mode';

export default function LibraryPage() {
  const { libId } = useParams<{ libId: string }>();
  const { showToast } = useToast();
  const { t } = useI18n();
  const { refreshTrigger } = useOutletContext<{ refreshTrigger: number }>() || { refreshTrigger: 0 };

  const filters = useLibraryFilters({ libId });
  const {
    activeTag,
    activeAuthor,
    activeStatus,
    activeLetter,
    sortByField,
    sortDir,
    page,
    pageSize,
    settingsReady,
    serializedFilters,
    setActiveTag,
    setActiveAuthor,
    setActiveStatus,
    setActiveLetter,
    setSortByField,
    setSortDir,
    setPage,
    setPageSize,
    applySnapshot,
    resetAll,
  } = filters;

  const [paginationMode, setPaginationMode] = useState<PaginationMode>(() => {
    const stored = localStorage.getItem(PAGINATION_MODE_KEY);
    return stored === 'infinite' ? 'infinite' : 'paged';
  });
  useEffect(() => {
    localStorage.setItem(PAGINATION_MODE_KEY, paginationMode);
  }, [paginationMode]);

  const [externalDrawerOpen, setExternalDrawerOpen] = useState(false);
  const [showCollectionModal, setShowCollectionModal] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const searchInputRef = useRef<HTMLInputElement>(null!);

  const showError = useCallback((messageKey: string) => showToast(t(messageKey), 'error'), [showToast, t]);

  // ===== series 数据 =====
  const seriesData = useLibrarySeries({
    libId,
    page,
    pageSize,
    activeTag,
    activeAuthor,
    activeStatus,
    activeLetter,
    sortByField,
    sortDir,
    serializedFilters,
    refreshTrigger,
    enabled: settingsReady,
  });
  const { allSeries, totalSeries, loading, pageCursorMap, resetPagination, refetchCurrentPage, patchSeries } = seriesData;

  // 翻页：filter 变化重置
  useEffect(() => {
    if (!settingsReady) return;
    setPage(1);
    resetPagination();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTag, activeAuthor, activeStatus, activeLetter, sortByField, sortDir, pageSize]);

  // 文本搜索：本地过滤显示
  const filteredSeries = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    if (!q) return allSeries;
    return allSeries.filter((s) => {
      const title = s.title?.Valid ? s.title.String.toLowerCase() : '';
      return s.name.toLowerCase().includes(q) || title.includes(q);
    });
  }, [allSeries, searchQuery]);

  // ===== 选择 =====
  const selection = useLibrarySelection({
    allSeries: filteredSeries,
    onChanged: refetchCurrentPage,
    onError: showError,
  });

  // ===== 外部库 =====
  const allSeriesIds = useMemo(() => allSeries.map((s) => s.id), [allSeries]);
  const externalLib = useExternalLibrary({ libId, refreshTrigger, allSeriesIds, onError: showError });

  // ===== 智能筛选 =====
  const smartFilters = useSmartFilters({
    libId,
    onSaved: () => showToast(t('home.smartFilters.saved'), 'success'),
    onError: showError,
    onApplied: (filter) => {
      applySnapshot({
        activeTag: filter.activeTag,
        activeAuthor: filter.activeAuthor,
        activeStatus: filter.activeStatus,
        activeLetter: filter.activeLetter,
        sortByField: filter.sortByField,
        sortDir: filter.sortDir,
        pageSize: filter.pageSize,
      });
      if (filter.id !== 'reset') {
        showToast(t('home.smartFilters.applied', { name: filter.name }), 'success');
      }
    },
  });

  const hasAnyFilter = Boolean(activeTag || activeAuthor || activeStatus || activeLetter);

  const smartFilterChips = useMemo(() => {
    const chips: string[] = [];
    chips.push(t('home.smartFilters.chipSort', {
      field: t(`home.toolbar.sort.${sortByField}`),
      dir: t(sortDir === 'asc' ? 'home.smartFilters.dir.asc' : 'home.smartFilters.dir.desc'),
    }));
    chips.push(t('home.smartFilters.chipPageSize', { count: pageSize }));
    return chips;
  }, [pageSize, sortByField, sortDir, t]);

  // ===== filter options =====
  const { allTags, allAuthors, filterOptionsLoading, loadFilterOptions, searchTagOptions, searchAuthorOptions } =
    useLibraryFilterOptions({ activeTag, activeAuthor });

  // ===== 卡片操作 =====
  const { rescanningId, handleCardClick, handleToggleFavorite, handleRescanSeries } = useLibraryCardActions({
    isSelectionMode: selection.isSelectionMode,
    toggleSelectSeries: selection.toggleSelectSeries,
    patchSeries,
    refetchCurrentPage,
    showError,
    showToast,
    t,
  });

  // ===== 刮削 =====
  const scraping = useSeriesScraping({
    onSuccess: () => {
      showToast(t('series.toast.metadataReviewQueued', { count: 0 }), 'success');
      refetchCurrentPage();
    },
    onError: showError,
  });

  // ===== 转移 =====
  const transfer = useLibraryTransfer({
    externalSession: externalLib.externalSession,
    externalSeriesMap: externalLib.externalSeriesMap,
    allSeries,
    selectedSeries: selection.selectedSeries,
    startExternalTransfer: externalLib.startExternalTransfer,
    clearSelection: selection.clearSelection,
    showError,
    showToast,
    t,
  });

  // ===== 外部访问 series visibility 摘要 =====
  const externalVisibilitySummary = useMemo(() => {
    const summary = { complete: 0, partial: 0, missing: 0 };
    allSeriesIds.forEach((id) => {
      const status = externalLib.externalSeriesMap[id];
      if (!status) {
        summary.missing += 1;
        return;
      }
      summary[status.external_sync_status] += 1;
    });
    return summary;
  }, [allSeriesIds, externalLib.externalSeriesMap]);

  // ===== 键盘 =====
  useLibraryKeyboard({
    enabled: settingsReady,
    onFocusSearch: () => searchInputRef.current?.focus(),
    onJumpFirst: () => setPage(1),
    onJumpLast: () => {
      const totalPages = Math.max(1, Math.ceil(totalSeries / pageSize));
      setPage(totalPages);
    },
    onToggleSelection: selection.toggleSelectionMode,
    onEscape: () => {
      if (externalDrawerOpen) setExternalDrawerOpen(false);
      else if (selection.isSelectionMode) selection.clearSelection();
    },
  });

  if (!libId) {
    return (
      <div className="text-center py-20 text-gray-400">
        <p>{t('home.empty.pickLibrary')}</p>
        <Link to="/" className="text-komgaPrimary hover:underline mt-2 inline-block">
          {t('home.empty.goDashboard')}
        </Link>
      </div>
    );
  }

  const supportsCursor = supportsCursorPagination(sortByField);
  const totalPages = Math.max(1, Math.ceil(totalSeries / pageSize));
  const hasMore = paginationMode === 'infinite' && filteredSeries.length < totalSeries;

  return (
    <div className="px-4 sm:px-6 py-6">
      <LibraryHeader
        totalSeries={totalSeries}
        hasSeries={allSeries.length > 0}
        isSelectionMode={selection.isSelectionMode}
        allCurrentPageSelected={selection.allCurrentPageSelected}
        selectedCount={selection.selectedSeries.length}
        sortByField={sortByField}
        sortDir={sortDir}
        searchValue={searchQuery}
        searchInputRef={searchInputRef}
        externalSessionActive={Boolean(externalLib.externalSession)}
        onSearchChange={setSearchQuery}
        onToggleSelectionMode={selection.toggleSelectionMode}
        onToggleSelectCurrentPage={selection.toggleSelectCurrentPage}
        onSortFieldChange={setSortByField}
        onToggleSortDir={() => setSortDir(sortDir === 'asc' ? 'desc' : 'asc')}
        onOpenExternal={() => setExternalDrawerOpen(true)}
      />

      <LibrarySavedViews
        views={smartFilters.savedSmartFilters}
        hasAnyFilter={hasAnyFilter}
        onSave={(name) =>
          smartFilters.saveSmartFilter(name, {
            activeTag,
            activeAuthor,
            activeStatus,
            activeLetter,
            sortByField,
            sortDir,
            pageSize,
          })
        }
        onApply={smartFilters.applySmartFilter}
        onDelete={smartFilters.deleteSmartFilter}
      />

      <LibraryFilterBar
        allStatuses={ALL_STATUSES}
        allTags={allTags}
        allAuthors={allAuthors}
        activeStatus={activeStatus}
        activeTag={activeTag}
        activeAuthor={activeAuthor}
        activeLetter={activeLetter}
        filterOptionsLoading={filterOptionsLoading}
        smartFilterChips={smartFilterChips}
        hasAnyFilter={hasAnyFilter}
        onStatusChange={setActiveStatus}
        onTagChange={setActiveTag}
        onAuthorChange={setActiveAuthor}
        onLetterChange={setActiveLetter}
        onResetFilters={resetAll}
        onFiltersOpen={loadFilterOptions}
        onTagSearch={searchTagOptions}
        onAuthorSearch={searchAuthorOptions}
      />

      <LibraryGrid
        series={filteredSeries}
        loading={loading}
        isSelectionMode={selection.isSelectionMode}
        selectedSeriesIds={selection.selectedSeries}
        rescanningId={rescanningId}
        scrapingSeriesId={scraping.scrapingSeries?.id ?? null}
        scrapeMenuOpenId={scraping.scrapeMenuOpenId}
        externalSeriesMap={externalLib.externalSeriesMap}
        externalSessionActive={Boolean(externalLib.externalSession)}
        hasMore={hasMore}
        paginationMode={paginationMode}
        onCardClick={handleCardClick}
        onToggleFavorite={handleToggleFavorite}
        onRescan={handleRescanSeries}
        onOpenScrapeMenu={(s) => scraping.setScrapeMenuOpenId(s.id)}
        onCloseScrapeMenu={() => scraping.setScrapeMenuOpenId(null)}
        onChooseScrapeProvider={(s, provider) => scraping.startScrape(s, provider)}
        onLoadMore={() => {
          if (paginationMode === 'infinite' && page < totalPages) setPage(page + 1);
        }}
      />

      {paginationMode === 'paged' && totalSeries > 0 && (
        <LibraryPagination
          paginationMode={paginationMode}
          totalSeries={totalSeries}
          page={page}
          pageSize={pageSize}
          pageCursorMap={pageCursorMap}
          supportsCursor={supportsCursor}
          lastLoadedPage={page}
          onChangePageSize={setPageSize}
          onChangePage={setPage}
          onTogglePaginationMode={() =>
            setPaginationMode((mode) => (mode === 'paged' ? 'infinite' : 'paged'))
          }
          onResetCursor={resetPagination}
        />
      )}
      {paginationMode === 'infinite' && totalSeries > 0 && (
        <div className="mt-8 flex items-center justify-center gap-3 border-t border-gray-800 pt-6 text-xs text-gray-500">
          <span>{t('home.pagination.totalSeries', { count: totalSeries })}</span>
          <button
            onClick={() => setPaginationMode('paged')}
            className="rounded-md border border-gray-700 px-2 py-1 text-xs text-gray-300 hover:border-komgaPrimary hover:text-komgaPrimary transition-colors"
          >
            {t('library.pagination.switchToPaged')}
          </button>
        </div>
      )}

      <LibrarySelectionBar
        visible={selection.isSelectionMode}
        count={selection.selectedSeries.length}
        currentPageSelectedCount={selection.currentPageSelectedCount}
        bulkProgressUpdating={selection.bulkProgressUpdating}
        externalReady={externalLib.externalSession?.status === 'ready'}
        startingTransfer={externalLib.startingTransfer}
        onMarkFavorite={() => selection.bulkFavorite(true)}
        onUnmarkFavorite={() => selection.bulkFavorite(false)}
        onAddToCollection={() => setShowCollectionModal(true)}
        onMarkRead={() => selection.bulkProgress(true)}
        onMarkUnread={() => selection.bulkProgress(false)}
        onTransfer={transfer.requestTransfer}
      />

      <ExternalLibraryDrawer
        open={externalDrawerOpen}
        onClose={() => setExternalDrawerOpen(false)}
        externalPath={externalLib.externalPath}
        externalIgnoreExtension={externalLib.externalIgnoreExtension}
        externalSession={externalLib.externalSession}
        startingExternalScan={externalLib.startingExternalScan}
        externalBrowsing={externalLib.externalBrowsing}
        externalBrowseCurrent={externalLib.externalBrowseCurrent}
        externalBrowseParent={externalLib.externalBrowseParent}
        externalBrowseDirs={externalLib.externalBrowseDirs}
        externalBrowseDrives={externalLib.externalBrowseDrives}
        recentExternalPaths={externalLib.recentExternalPaths}
        externalVisibilitySummary={externalVisibilitySummary}
        onChangePath={externalLib.setExternalPath}
        onToggleIgnoreExtension={externalLib.setExternalIgnoreExtension}
        onOpenBrowse={externalLib.openExternalDirectoryBrowser}
        onCloseBrowse={externalLib.closeExternalDirectoryBrowser}
        onChooseCurrentBrowse={externalLib.chooseCurrentExternalDirectory}
        onNavigateBrowse={externalLib.navigateExternalDirectoryBrowser}
        onStartScan={externalLib.startExternalLibraryScan}
        onClearSession={externalLib.clearExternalSession}
      />

      {showCollectionModal && selection.selectedSeries.length > 0 && (
        <AddToCollectionModal
          seriesIds={selection.selectedSeries}
          onClose={() => setShowCollectionModal(false)}
          onSuccess={() => {
            showToast(t('home.selection.addToCollectionSuccess', { count: selection.selectedSeries.length }), 'success');
            selection.clearSelection();
          }}
        />
      )}

      <TransferConfirmModal
        open={transfer.showTransferConfirmModal}
        onClose={transfer.closeTransferModal}
        selectedCount={selection.selectedSeries.length}
        externalPath={externalLib.externalSession?.external_path}
        summary={transfer.pendingTransferSummary}
        submitting={externalLib.startingTransfer}
        onConfirm={transfer.confirmTransfer}
      />

      <LibraryScrapeModal scraping={scraping} />
    </div>
  );
}
