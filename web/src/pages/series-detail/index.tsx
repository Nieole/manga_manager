/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

/**
 * 业务流程：系列详情把系列主体、卷册、单本、元数据审核、关系编辑和继续阅读入口组织成单系列工作区。
 * 数据边界：卷选择最终要展开为书籍 ID，元数据候选要经过审核，关系变更要刷新图谱和侧栏状态。
 * 维护风险：编辑态与展示态如果不同步，会导致用户保存后仍看到旧标签、旧封面或旧关系。
 */

import { useCallback, useMemo, useState } from 'react';
import { useNavigate, useOutletContext, useParams, useSearchParams } from 'react-router-dom';
import { BookImage, List, Grid, FolderOpen, ArrowLeft } from 'lucide-react';
import AddToCollectionModal from '../../components/AddToCollectionModal';
import { useToast } from '../../components/ToastProvider';
import { useI18n } from '../../i18n/LocaleProvider';
import { apiClient, getApiErrorMessage } from '../../api/client';

import { SeriesHeroBar } from './SeriesHeroBar';
import { SeriesQuickActions } from './SeriesQuickActions';
import { SeriesVolumeAccordion } from './SeriesVolumeAccordion';
import { SeriesVolumeGrid } from './SeriesVolumeGrid';
import { SeriesBookGrid } from './SeriesBookGrid';
import { SeriesSelectionBar } from './SeriesSelectionBar';
import { SeriesSidePanel, SeriesSidePanelBadge } from './SeriesSidePanel';
import { SeriesMetadataEditorModal } from './SeriesMetadataEditorModal';
import { SeriesSearchModal } from './SeriesSearchModal';
import type { Book } from './types';
import { useSeriesContext } from './hooks/useSeriesContext';
import { useSeriesSelection } from './hooks/useSeriesSelection';
import { useSeriesScrape } from './hooks/useSeriesScrape';
import { useSeriesEdit } from './hooks/useSeriesEdit';
import { useSeriesActions } from './hooks/useSeriesActions';
import { useSeriesMetadataReview } from './hooks/useSeriesMetadataReview';
import { useSeriesRelations } from './hooks/useSeriesRelations';
import { useSeriesProgress } from './hooks/useSeriesProgress';
import { useSeriesFailedTasks } from './hooks/useSeriesFailedTasks';
import { useSeriesVolumes } from './hooks/useSeriesVolumes';
import { useSeriesOpenVolumes } from './hooks/useSeriesOpenVolumes';
import { buildContinueCta } from './hooks/useSeriesContinue';
import { SeriesFranchiseView } from './SeriesFranchiseView';

/**
 * 业务注释：SeriesDetailPage 是前端系列详情链路，负责卷册聚合、元数据审核、关系维护和阅读入口的页面、组件或工具入口，负责把领域状态转换为用户可操作的界面行为。
 * 调整时应同时检查加载态、空态、错误态、主题适配和调用方传入的业务语义。
 */
export default function SeriesDetailPage() {
  const { t } = useI18n();
  const { seriesId } = useParams();
  const navigate = useNavigate();
  const { refreshTrigger } = useOutletContext<{ refreshTrigger: number }>() || { refreshTrigger: 0 };
  const { showToast } = useToast();

  // 系列详情页把多个后端端点折叠为一个上下文：系列主体、书籍、标签、作者、关系、待审核元数据和继续阅读信息。
  // 页面组件只消费 ctx，避免各个子面板各自请求导致刷新顺序不一致或保存后局部状态过期。
  const ctx = useSeriesContext({ seriesId, refreshTrigger });
  const { volumes, standaloneBooks, allBookIds } = useSeriesVolumes(ctx.books);
  const openVolumes = useSeriesOpenVolumes({ seriesId, knownVolumes: volumes.map((v) => v.name) });

  const [searchParams, setSearchParams] = useSearchParams();
  const activeVolumeName = searchParams.get('volume');

  const [volumeViewMode, setVolumeViewMode] = useState<'accordion' | 'grid'>(
    () => (localStorage.getItem('komga-volume-view-mode') as 'accordion' | 'grid') || 'accordion'
  );

  const handleVolumeViewModeChange = useCallback((mode: 'accordion' | 'grid') => {
    setVolumeViewMode(mode);
    localStorage.setItem('komga-volume-view-mode', mode);
  }, []);

  const totalSelectableCount = volumes.length + standaloneBooks.length + ctx.books.length;
  // 选择模型同时支持“选卷”和“选单本”，最终提交前统一展开成书籍 ID，保证批量已读、加入合集等操作只面向后端书籍契约。
  const selection = useSeriesSelection({
    totalCount: totalSelectableCount,
    collectAllIds: useCallback(() => allBookIds, [allBookIds]),
  });

  const progress = useSeriesProgress({ reload: ctx.reload, showToast, t });
  const actions = useSeriesActions({ seriesId, showToast, t });
  const scrape = useSeriesScrape({ seriesId, series: ctx.series, reload: ctx.reload, showToast, t });
  const edit = useSeriesEdit({
    seriesId,
    series: ctx.series,
    tags: ctx.tags,
    authors: ctx.authors,
    links: ctx.links,
    reload: ctx.reload,
    showToast,
    t,
  });
  const metaReview = useSeriesMetadataReview({ reload: ctx.reload, showToast, t });
  const relations = useSeriesRelations({
    seriesId,
    libraryId: ctx.series?.library_id,
    relations: ctx.relations,
    setRelations: ctx.setRelations,
    showToast,
    t,
  });
  const failedTasks = useSeriesFailedTasks({ seriesId, setFailedTasks: ctx.setFailedTasks, showToast, t });

  const [showCollectionModal, setShowCollectionModal] = useState(false);
  const [sidePanelOpen, setSidePanelOpen] = useState(false);
  const [sidePanelTab, setSidePanelTab] = useState<'relations' | 'metadata' | 'failed'>('relations');

  const continueCta = useMemo(() => buildContinueCta(ctx.continueInfo, ctx.books), [ctx.continueInfo, ctx.books]);

  // 背景封面优先使用已有封面路径的书籍，并带 updated_at 版本参数，让封面重建后浏览器可以刷新视觉背景。
  const coverUrl = useMemo(() => {
    const b = ctx.books.find((book) => book.cover_path?.Valid && book.cover_path?.String) || ctx.books[0];
    return b ? `/api/covers/${b.id}${b.updated_at ? `?v=${new Date(b.updated_at).getTime()}` : ''}` : null;
  }, [ctx.books]);

  const handleBack = useCallback(() => {
    if (window.history.state && window.history.state.idx > 0) {
      navigate(-1);
    } else if (ctx.series?.library_id) {
      navigate(`/library/${ctx.series.library_id}`);
    } else {
      navigate('/');
    }
  }, [navigate, ctx.series]);

  // 批量操作前把卷选择和单本选择去重合并，避免同一本既被卷选中又被单独选中时重复写进度。
  const collectSelectedBookIds = useCallback(() => {
    const volumeBookIds = volumes.filter((v) => selection.selectedVolumes.includes(v.name)).flatMap((v) => v.books.map((b) => b.id));
    return Array.from(new Set([...selection.selectedBooks, ...volumeBookIds]));
  }, [volumes, selection.selectedVolumes, selection.selectedBooks]);

  const handleBulkMark = useCallback(
    async (isRead: boolean) => {
      const ids = collectSelectedBookIds();
      if (ids.length === 0) return;
      await progress.bulkUpdate(isRead, ids);
      selection.setIsSelectionMode(false);
      selection.clear();
    },
    [collectSelectedBookIds, progress, selection],
  );

  const handleQuickToggleBookRead = useCallback(
    (book: Book, makeRead: boolean) => {
      void progress.quickToggleBook(book.id, makeRead);
    },
    [progress],
  );

  const handleQuickToggleVolumeRead = useCallback(
    (volume: { books: Book[] }, makeRead: boolean) => {
      void progress.bulkUpdate(makeRead, volume.books.map((b) => b.id));
    },
    [progress],
  );

  const handleExportBookComicInfo = useCallback((book: Book) => {
    window.location.href = `/api/books/${book.id}/comicinfo.xml`;
  }, []);

  // 写回 ComicInfo：修改用户原始归档（仅 cbz/zip，原子替换、不备份），二次确认后触发。
  const handleWriteBookComicInfo = useCallback(
    async (book: Book) => {
      if (!window.confirm(t('series.book.writeComicInfoConfirm'))) return;
      try {
        await apiClient.post(`/api/books/${book.id}/comicinfo`);
        showToast(t('series.book.writeComicInfoDone'), 'success');
      } catch (err) {
        showToast(getApiErrorMessage(err, t('series.book.writeComicInfoFailed')), 'error');
      }
    },
    [showToast, t],
  );

  // 整系列写回：对所有可写归档（cbz/zip）写入 ComicInfo，rar/cbr 跳过。二次确认后触发。
  const handleWriteSeriesComicInfo = useCallback(async () => {
    if (!seriesId) return;
    if (!window.confirm(t('series.header.writeComicInfoConfirm'))) return;
    try {
      const res = await apiClient.post<{ written: number; skipped: number; failed: number }>(
        `/api/series/${seriesId}/comicinfo`,
      );
      showToast(
        t('series.header.writeComicInfoDone', {
          written: res.data.written,
          skipped: res.data.skipped,
          failed: res.data.failed,
        }),
        res.data.failed > 0 ? 'error' : 'success',
      );
    } catch (err) {
      showToast(getApiErrorMessage(err, t('series.book.writeComicInfoFailed')), 'error');
    }
  }, [seriesId, showToast, t]);

  const handleCopyBookPath = useCallback(
    async (book: Book) => {
      try {
        await navigator.clipboard.writeText(`book#${book.id} ${book.name}`);
        showToast(t('series.book.pathCopied'), 'success');
      } catch {
        showToast(t('series.book.copyPathFailed'), 'error');
      }
    },
    [showToast, t],
  );

  const taskTypeLabel = useCallback(
    (type: string) => {
      switch (type) {
        case 'scan_series':
          return t('task.type.scan_series');
        case 'scrape':
          return t('task.type.scrape');
        default:
          return type;
      }
    },
    [t],
  );

  const openSidePanel = useCallback((tab: 'relations' | 'metadata' | 'failed') => {
    setSidePanelTab(tab);
    setSidePanelOpen(true);
  }, []);

  const pendingMetadataCount = ctx.metadataReviews.length;
  const failedTaskCount = ctx.failedTasks.length;

  if (ctx.loading && !ctx.series) {
    return (
      <div className="text-center py-20 text-gray-500 animate-pulse">{t('series.content.loading')}</div>
    );
  }

  // 主数据加载失败（非加载中且无 series）：给出错误 + 重试，而非把 null series 传给整页导致破损。
  if (!ctx.series) {
    return (
      <div className="text-center py-20">
        <p className="text-sm text-red-400">
          {t('series.content.loadFailed')}
          {ctx.error ? `：${ctx.error}` : ''}
        </p>
        <button
          type="button"
          onClick={ctx.retry}
          className="mt-3 inline-flex items-center gap-2 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-1.5 text-sm text-red-400 transition-colors hover:bg-red-500/20"
        >
          {t('common.retry')}
        </button>
      </div>
    );
  }

  return (
    <div className="relative min-h-screen">
      {coverUrl && (
        <>
          <div className="fixed inset-0 z-0 bg-gray-950 pointer-events-none" />
          <div
            className="fixed inset-0 z-0 bg-cover bg-position-[center_top] bg-no-repeat blur-[80px] opacity-50 transform scale-110 pointer-events-none saturate-150"
            style={{ backgroundImage: `url("${coverUrl}")` }}
          />
          <div className="fixed inset-0 z-0 bg-linear-to-t from-gray-950 via-gray-950/80 to-transparent pointer-events-none" />
          <div className="fixed inset-0 z-0 bg-linear-to-b from-gray-950/60 via-transparent to-transparent pointer-events-none" />
        </>
      )}

      <div className="relative z-10 p-4 sm:p-6 lg:p-10">
        <SeriesHeroBar
          series={ctx.series}
          tags={ctx.tags}
          authors={ctx.authors}
          links={ctx.links}
          continueInfo={ctx.continueInfo}
          continueCta={continueCta}
          bookCount={ctx.books.length}
          volumeCount={volumes.length}
          standaloneCount={standaloneBooks.length}
          coverUrl={coverUrl}
          onBack={handleBack}
          selectionToggle={{
            isSelectionMode: selection.isSelectionMode,
            toggle: selection.toggleSelectionMode,
          }}
          rightSlot={
            <SeriesQuickActions
              onEdit={() => edit.setIsEditing(true)}
              onAddToCollection={() => setShowCollectionModal(true)}
              onExportComicInfo={() => {
                if (seriesId) window.location.href = `/api/series/${seriesId}/comicinfo.zip`;
              }}
              onWriteComicInfo={handleWriteSeriesComicInfo}
              onOpenDirectory={actions.openDirectory}
              onRescan={actions.rescan}
              onScrape={scrape.handleScrape}
              scrapeMenuOpen={scrape.scrapeMenuOpen}
              onToggleScrapeMenu={() => scrape.setScrapeMenuOpen((v) => !v)}
              onCloseScrapeMenu={() => scrape.setScrapeMenuOpen(false)}
              isOpeningDirectory={actions.isOpeningDirectory}
              isRescanning={actions.isRescanning}
              isScraping={scrape.isScraping}
            />
          }
          badgeSlot={
            <SeriesSidePanelBadge
              pendingMetadata={pendingMetadataCount}
              failedCount={failedTaskCount}
              onClick={() => openSidePanel(failedTaskCount > 0 ? 'failed' : pendingMetadataCount > 0 ? 'metadata' : 'relations')}
            />
          }
        />

        {ctx.books.length === 0 ? (
          <div className="rounded-4xl border border-white/5 bg-gray-950/40 backdrop-blur-xl p-16 text-center text-gray-400 shadow-2xl">
            <BookImage className="mx-auto w-16 h-16 text-gray-600 mb-6 opacity-60 drop-shadow-md" />
            <h3 className="text-xl font-bold text-gray-200">{t('series.content.empty')}</h3>
          </div>
        ) : (
          <div className="space-y-8">
            {activeVolumeName ? (
              <div className="space-y-4">
                <div className="flex items-center gap-4">
                  <button
                    onClick={() => {
                      const newParams = new URLSearchParams(searchParams);
                      newParams.delete('volume');
                      setSearchParams(newParams);
                    }}
                    className="p-2 rounded-full bg-white/5 hover:bg-white/10 text-gray-300 hover:text-white transition-colors border border-white/5 shadow-xs"
                  >
                    <ArrowLeft className="w-5 h-5" />
                  </button>
                  <h3 className="text-lg font-bold text-white flex items-center gap-2">
                    <FolderOpen className="w-5 h-5 text-komgaPrimary" />
                    {activeVolumeName}
                  </h3>
                </div>
                <SeriesBookGrid
                  books={volumes.find((v) => v.name === activeVolumeName)?.books || []}
                  isSelectionMode={selection.isSelectionMode}
                  selectedBooks={selection.selectedBooks}
                  onCardClick={(b) => selection.toggleBook(b.id)}
                  onQuickToggleRead={handleQuickToggleBookRead}
                  onExportComicInfo={handleExportBookComicInfo}
                  onWriteComicInfo={handleWriteBookComicInfo}
                  onCopyPath={handleCopyBookPath}
                />
              </div>
            ) : (
              <>
                {volumes.length > 0 && (
                  <div className="space-y-3">
                    <div className="flex items-center justify-between mb-4 ml-1">
                      <h3 className="text-sm font-extrabold text-white tracking-widest uppercase flex items-center gap-2 drop-shadow-md">
                        <FolderOpen className="w-5 h-5 text-komgaPrimary drop-shadow-[0_0_8px_rgba(var(--rgb-komga-primary),0.5)]" />
                        {t('series.content.volumes')}
                      </h3>
                      <div className="flex items-center gap-1 bg-black/40 rounded-lg p-1 border border-white/5 shadow-inner">
                        <button
                          onClick={() => handleVolumeViewModeChange('accordion')}
                          className={`p-1.5 rounded-md transition-all ${
                            volumeViewMode === 'accordion' ? 'bg-white/10 text-komgaPrimary shadow-xs' : 'text-gray-500 hover:text-gray-300'
                          }`}
                        >
                          <List className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => handleVolumeViewModeChange('grid')}
                          className={`p-1.5 rounded-md transition-all ${
                            volumeViewMode === 'grid' ? 'bg-white/10 text-komgaPrimary shadow-xs' : 'text-gray-500 hover:text-gray-300'
                          }`}
                        >
                          <Grid className="w-4 h-4" />
                        </button>
                      </div>
                    </div>

                    {volumeViewMode === 'accordion' ? (
                      <SeriesVolumeAccordion
                        volumes={volumes}
                        isOpen={openVolumes.isOpen}
                        onToggle={openVolumes.toggle}
                        isSelectionMode={selection.isSelectionMode}
                        selectedVolumes={selection.selectedVolumes}
                        selectedBooks={selection.selectedBooks}
                        onToggleVolumeSelection={selection.toggleVolume}
                        onCardClick={(b) => selection.toggleBook(b.id)}
                        onQuickToggleVolumeRead={handleQuickToggleVolumeRead}
                        onQuickToggleBookRead={handleQuickToggleBookRead}
                        onExportComicInfo={handleExportBookComicInfo}
                        onWriteComicInfo={handleWriteBookComicInfo}
                        onCopyPath={handleCopyBookPath}
                        seriesUpdatedAt={ctx.series?.updated_at}
                      />
                    ) : (
                      <SeriesVolumeGrid
                        volumes={volumes}
                        isSelectionMode={selection.isSelectionMode}
                        selectedVolumes={selection.selectedVolumes}
                        onToggleVolumeSelection={selection.toggleVolume}
                        onCardClick={(vName) => {
                          const newParams = new URLSearchParams(searchParams);
                          newParams.set('volume', vName);
                          setSearchParams(newParams);
                        }}
                        onQuickToggleVolumeRead={handleQuickToggleVolumeRead}
                        seriesUpdatedAt={ctx.series?.updated_at}
                      />
                    )}
                  </div>
                )}
                
                {standaloneBooks.length > 0 && (
                  <div>
                    <h3 className="text-sm font-extrabold text-white tracking-widest uppercase flex items-center gap-2 drop-shadow-md mb-4 ml-1">
                      <BookImage className="w-5 h-5 text-cyan-400 drop-shadow-[0_0_8px_rgba(34,211,238,0.5)]" />
                      {t('series.content.standalone')}
                    </h3>
                    <SeriesBookGrid
                      books={standaloneBooks}
                      isSelectionMode={selection.isSelectionMode}
                      selectedBooks={selection.selectedBooks}
                      onCardClick={(b) => selection.toggleBook(b.id)}
                      onQuickToggleRead={handleQuickToggleBookRead}
                      onExportComicInfo={handleExportBookComicInfo}
                      onWriteComicInfo={handleWriteBookComicInfo}
                      onCopyPath={handleCopyBookPath}
                    />
                  </div>
                )}
              </>
            )}
          </div>
        )}
        {seriesId && (
          <SeriesFranchiseView seriesId={Number(seriesId)} />
        )}
      </div>

      <SeriesSelectionBar
        visible={selection.isSelectionMode}
        selectedCount={selection.selectedCount}
        allSelected={selection.allSelected}
        onSelectAllOrNone={selection.selectAllOrNone}
        onMarkRead={() => handleBulkMark(true)}
        onMarkUnread={() => handleBulkMark(false)}
        busy={progress.busy}
      />

      <SeriesSidePanel
        open={sidePanelOpen}
        onClose={() => setSidePanelOpen(false)}
        activeTab={sidePanelTab}
        onTabChange={setSidePanelTab}
        relations={ctx.relations}
        relationCandidates={relations.relationCandidates}
        relationType={relations.relationType}
        relationSearch={relations.relationSearch}
        selectedTargetId={relations.selectedTargetId}
        isAddingRelation={relations.isAdding}
        isLoadingCandidates={relations.isLoadingCandidates}
        onRelationTypeChange={relations.setRelationType}
        onRelationSearchChange={relations.onSearchChange}
        onSelectTarget={relations.onSelectTarget}
        onAddRelation={relations.addRelation}
        onUpdateRelation={relations.updateRelation}
        onDeleteRelation={relations.deleteRelation}
        metadataReviews={ctx.metadataReviews}
        metadataProvenance={ctx.metadataProvenance}
        busyMetadataReviewId={metaReview.busyMetadataReviewId}
        onApplyMetadataReview={metaReview.apply}
        onRejectMetadataReview={metaReview.reject}
        failedTasks={ctx.failedTasks}
        retryingTaskKey={failedTasks.retryingTaskKey}
        onRetryFailedTask={failedTasks.retry}
        taskTypeLabel={taskTypeLabel}
      />

      <SeriesMetadataEditorModal
        open={edit.isEditing}
        allTags={edit.allTags}
        allAuthors={edit.allAuthors}
        editForm={edit.editForm}
        lockedFields={edit.lockedFields}
        onClose={() => edit.setIsEditing(false)}
        onSave={edit.save}
        onToggleLock={edit.toggleLock}
        onFormChange={edit.onFormChange}
      />

      <SeriesSearchModal
        open={scrape.showSearchModal}
        modalSearchQuery={scrape.modalSearchQuery}
        isScraping={scrape.isScraping}
        searchResults={scrape.searchResults}
        currentOffset={scrape.currentOffset}
        searchTotal={scrape.searchTotal}
        onClose={scrape.closeSearchModal}
        providerLabel={scrape.searchProvider === 'bangumi' ? 'Bangumi' : 'AI/LLM'}
        currentSeries={ctx.series}
        currentTags={ctx.tags}
        lockedFields={edit.lockedFields}
        selectedResult={scrape.selectedSearchResult}
        onSelectMetadata={scrape.setSelectedSearchResult}
        onSearchQueryChange={scrape.setModalSearchQuery}
        onReSearch={scrape.handleModalReSearch}
        onApplyMetadata={scrape.handleApplyMetadata}
      />

      {showCollectionModal && seriesId && (
        <AddToCollectionModal
          seriesIds={[Number(seriesId)]}
          onClose={() => setShowCollectionModal(false)}
          onSuccess={() => showToast(t('series.toast.addedToCollection'), 'success')}
        />
      )}
    </div>
  );
}
