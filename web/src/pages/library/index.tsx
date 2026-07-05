/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useOutletContext, useParams } from 'react-router-dom';

import AddToCollectionModal from '../../components/AddToCollectionModal';
import BulkEditSeriesModal from '../../components/BulkEditSeriesModal';
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
import { type PaginationMode, type Series, type ViewMode } from './types';
import { useLibraryFilters, supportsCursorPagination, hasAdvancedFilters } from './hooks/useLibraryFilters';
import { useScrapeProviders } from '../../hooks/useScrapeProviders';
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
const VIEW_MODE_KEY = 'manga_manager_library_view_mode';

export default function LibraryPage() {
  const { libId } = useParams<{ libId: string }>();
  const { showToast } = useToast();
  const { t } = useI18n();
  const { refreshTrigger } = useOutletContext<{ refreshTrigger: number }>() || { refreshTrigger: 0 };

  // 资料库页面的筛选状态需要同时满足三件事：URL 可回放、后端查询可复现、浏览器刷新后用户选择不丢失。
  // 因此筛选、排序、分页和智能视图都集中在 hook 层管理，页面只负责把它们编排到工具栏、列表和分页控件。
  const filters = useLibraryFilters({ libId });
  const scrapeProviders = useScrapeProviders();
  const {
    activeTag,
    activeAuthor,
    activeStatus,
    activeLetter,
    advanced,
    sortByField,
    sortDir,
    keyword,
    page,
    pageSize,
    settingsReady,
    setActiveTag,
    setActiveAuthor,
    setActiveStatus,
    setActiveLetter,
    setAdvancedFilters,
    setSortByField,
    setSortDir,
    setKeyword,
    setPage,
    setPageSize,
    applySnapshot,
    resetAll,
  } = filters;

  // 分页模式是纯前端体验偏好，不能影响后端数据契约；后端仍以 page/pageSize/cursor 返回稳定结果。
  const [paginationMode, setPaginationMode] = useState<PaginationMode>(() => {
    const stored = localStorage.getItem(PAGINATION_MODE_KEY);
    return stored === 'infinite' ? 'infinite' : 'paged';
  });
  useEffect(() => {
    localStorage.setItem(PAGINATION_MODE_KEY, paginationMode);
  }, [paginationMode]);

  // 视图密度也是纯前端体验偏好，与分页模式同样只影响渲染布局，不改后端契约。
  const [viewMode, setViewMode] = useState<ViewMode>(() => {
    const stored = localStorage.getItem(VIEW_MODE_KEY);
    return stored === 'compact' || stored === 'list' ? stored : 'grid';
  });
  useEffect(() => {
    localStorage.setItem(VIEW_MODE_KEY, viewMode);
  }, [viewMode]);

  const [externalDrawerOpen, setExternalDrawerOpen] = useState(false);
  const [showCollectionModal, setShowCollectionModal] = useState(false);
  const [showBulkEditModal, setShowBulkEditModal] = useState(false);
  const searchInputRef = useRef<HTMLInputElement>(null!);

  // 防抖：输入停止 300ms 后才更新 keyword 触发后端查询
  const [debouncedKeyword, setDebouncedKeyword] = useState('');
  useEffect(() => {
    const id = window.setTimeout(() => setDebouncedKeyword(keyword.trim()), 300);
    return () => window.clearTimeout(id);
  }, [keyword]);

  const showError = useCallback((messageKey: string) => showToast(t(messageKey), 'error'), [showToast, t]);

  // ===== series 数据 =====
  // 这里是资料库主查询的唯一入口，所有筛选项都要在这里汇聚，避免列表、分页器和批量操作读取到不同版本的数据。
  const seriesData = useLibrarySeries({
    libId,
    page,
    pageSize,
    activeTag,
    activeAuthor,
    activeStatus,
    activeLetter,
    advanced,
    sortByField,
    sortDir,
    refreshTrigger,
    enabled: settingsReady && debouncedKeyword === keyword.trim(),
    keyword: debouncedKeyword,
    appendMode: paginationMode === 'infinite',
  });
  const { allSeries, totalSeries, loading, error: seriesError, pageCursorMap, resetPagination, refetchCurrentPage, retry: retrySeries, patchSeries } = seriesData;

  // 翻页：filter 或 keyword 变化时重置到第 1 页
  useEffect(() => {
    if (!settingsReady) return;
    setPage(1);
    resetPagination();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTag, activeAuthor, activeStatus, activeLetter, advanced, sortByField, sortDir, pageSize, debouncedKeyword]);

  // 切换分页模式时回到第 1 页：无限滚动应从头开始累积，避免带着分页模式的中间页码进入追加流。
  useEffect(() => {
    if (!settingsReady) return;
    setPage(1);
    resetPagination();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paginationMode]);

  // ===== 选择 =====
  const selection = useLibrarySelection({
    allSeries: allSeries,
    onChanged: refetchCurrentPage,
    onError: showError,
  });

  // ===== 外部库 =====
  const allSeriesIds = useMemo(() => allSeries.map((s) => s.id), [allSeries]);
  const externalLib = useExternalLibrary({ libId, refreshTrigger, allSeriesIds, onError: showError });

  // ===== 智能筛选 =====
  // 智能筛选保存的是一组业务视图快照，应用时必须重置关键字，避免“视图条件 + 临时搜索词”叠加后让用户误以为视图失效。
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
        keyword: '',
        sortByField: filter.sortByField,
        sortDir: filter.sortDir,
        pageSize: filter.pageSize,
      });
      if (filter.id !== 'reset') {
        showToast(t('home.smartFilters.applied', { name: filter.name }), 'success');
      }
    },
  });

  const hasAnyFilter = Boolean(activeTag || activeAuthor || activeStatus || activeLetter || keyword.trim()) || hasAdvancedFilters(advanced);

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

  // 稳定化传给 LibraryCard 的刮削菜单回调，配合 LibraryCard 的 React.memo，避免库页（扫描/刷新期）
  // 重渲染时大量卡片跟着重算。先解构出稳定的函数（useState setter / useCallback）作为局部依赖，
  // 既满足 exhaustive-deps，又不会因依赖整个每渲染新建的 scraping 对象而失去 memoization。
  const { setScrapeMenuOpenId, startScrape } = scraping;
  const handleOpenScrapeMenu = useCallback((s: Series) => setScrapeMenuOpenId(s.id), [setScrapeMenuOpenId]);
  const handleCloseScrapeMenu = useCallback(() => setScrapeMenuOpenId(null), [setScrapeMenuOpenId]);
  const handleChooseScrapeProvider = useCallback((s: Series, provider: string) => startScrape(s, provider), [startScrape]);

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
  const hasMore = paginationMode === 'infinite' && allSeries.length < totalSeries;

  return (
    <div className="px-4 sm:px-6 py-6">
      <LibraryHeader
        libraryId={libId}
        totalSeries={totalSeries}
        hasSeries={allSeries.length > 0}
        isSelectionMode={selection.isSelectionMode}
        allCurrentPageSelected={selection.allCurrentPageSelected}
        selectedCount={selection.selectedSeries.length}
        sortByField={sortByField}
        sortDir={sortDir}
        searchValue={keyword}
        searchInputRef={searchInputRef}
        externalSessionActive={Boolean(externalLib.externalSession)}
        viewMode={viewMode}
        onSearchChange={setKeyword}
        onToggleSelectionMode={selection.toggleSelectionMode}
        onToggleSelectCurrentPage={selection.toggleSelectCurrentPage}
        onSortFieldChange={setSortByField}
        onToggleSortDir={() => setSortDir(sortDir === 'asc' ? 'desc' : 'asc')}
        onOpenExternal={() => setExternalDrawerOpen(true)}
        onViewModeChange={setViewMode}
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
        onExpand={smartFilters.ensureLoaded}
      />

      <LibraryFilterBar
        allStatuses={ALL_STATUSES}
        allTags={allTags}
        allAuthors={allAuthors}
        activeStatus={activeStatus}
        activeTag={activeTag}
        activeAuthor={activeAuthor}
        activeLetter={activeLetter}
        advanced={advanced}
        filterOptionsLoading={filterOptionsLoading}
        smartFilterChips={smartFilterChips}
        hasAnyFilter={hasAnyFilter}
        onStatusChange={setActiveStatus}
        onTagChange={setActiveTag}
        onAuthorChange={setActiveAuthor}
        onLetterChange={setActiveLetter}
        onAdvancedChange={setAdvancedFilters}
        onResetFilters={resetAll}
        onFiltersOpen={loadFilterOptions}
        onTagSearch={searchTagOptions}
        onAuthorSearch={searchAuthorOptions}
      />

      {seriesError !== null && (
        <div className="mb-4 flex items-center justify-between gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-500">
          <span>
            {t('library.loadSeriesFailed')}
            {seriesError ? `：${seriesError}` : ''}
          </span>
          <button
            type="button"
            onClick={retrySeries}
            className="inline-flex shrink-0 items-center gap-2 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-1.5 text-red-500 transition-colors hover:bg-red-500/20"
          >
            {t('common.retry')}
          </button>
        </div>
      )}

      <LibraryGrid
        series={allSeries}
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
        viewMode={viewMode}
        onCardClick={handleCardClick}
        onToggleFavorite={handleToggleFavorite}
        onRescan={handleRescanSeries}
        onOpenScrapeMenu={handleOpenScrapeMenu}
        onCloseScrapeMenu={handleCloseScrapeMenu}
        onChooseScrapeProvider={handleChooseScrapeProvider}
        scrapeProviders={scrapeProviders}
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
        onBulkEdit={() => setShowBulkEditModal(true)}
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

      {showBulkEditModal && selection.selectedSeries.length > 0 && (
        <BulkEditSeriesModal
          seriesIds={selection.selectedSeries}
          onClose={() => setShowBulkEditModal(false)}
          onError={(msg) => showToast(msg, 'error')}
          onSuccess={(updated) => {
            showToast(t('bulkEdit.success', { count: updated }), 'success');
            selection.clearSelection();
            refetchCurrentPage();
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
