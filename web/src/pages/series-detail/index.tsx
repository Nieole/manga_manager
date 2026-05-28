import { useCallback, useMemo, useState } from 'react';
import { useNavigate, useOutletContext, useParams } from 'react-router-dom';
import { BookImage } from 'lucide-react';
import AddToCollectionModal from '../../components/AddToCollectionModal';
import { useToast } from '../../components/ToastProvider';
import { useI18n } from '../../i18n/LocaleProvider';

import { SeriesHeroBar } from './SeriesHeroBar';
import { SeriesQuickActions } from './SeriesQuickActions';
import { SeriesVolumeAccordion } from './SeriesVolumeAccordion';
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

export default function SeriesDetailPage() {
  const { t } = useI18n();
  const { seriesId } = useParams();
  const navigate = useNavigate();
  const { refreshTrigger } = useOutletContext<{ refreshTrigger: number }>() || { refreshTrigger: 0 };
  const { showToast } = useToast();

  const ctx = useSeriesContext({ seriesId, refreshTrigger });
  const { volumes, standaloneBooks, allBookIds } = useSeriesVolumes(ctx.books);
  const openVolumes = useSeriesOpenVolumes({ seriesId, knownVolumes: volumes.map((v) => v.name) });

  const totalSelectableCount = volumes.length + standaloneBooks.length + ctx.books.length;
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

  const coverUrl = useMemo(() => {
    const b = ctx.books.find((book) => book.cover_path?.Valid && book.cover_path?.String) || ctx.books[0];
    return b ? `/api/covers/${b.id}${b.updated_at ? `?v=${new Date(b.updated_at).getTime()}` : ''}` : null;
  }, [ctx.books]);

  const handleBack = useCallback(() => {
    if (ctx.series?.library_id) {
      navigate(`/library/${ctx.series.library_id}`);
    } else {
      navigate('/');
    }
  }, [navigate, ctx.series]);

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

  return (
    <div className="relative min-h-screen">
      {coverUrl && (
        <>
          <div className="fixed inset-0 z-0 bg-gray-950 pointer-events-none" />
          <div
            className="fixed inset-0 z-0 bg-cover bg-[center_top] bg-no-repeat blur-lg opacity-100 transform scale-105 pointer-events-none"
            style={{ backgroundImage: `url("${coverUrl}")` }}
          />
          <div className="fixed inset-0 z-0 bg-gray-950/70 pointer-events-none" />
          <div className="fixed inset-0 z-0 bg-gradient-to-t from-gray-950 via-gray-950/40 to-transparent pointer-events-none" />
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
              onClick={() => openSidePanel(failedTaskCount > 0 ? 'failed' : 'metadata')}
            />
          }
        />

        {ctx.books.length === 0 ? (
          <div className="rounded-2xl border border-gray-800 bg-komgaSurface/60 backdrop-blur-md p-10 text-center text-gray-500">
            <BookImage className="mx-auto w-10 h-10 text-gray-700 mb-3" />
            {t('series.content.empty')}
          </div>
        ) : (
          <div className="space-y-8">
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
              onCopyPath={handleCopyBookPath}
              seriesUpdatedAt={ctx.series?.updated_at}
            />
            {standaloneBooks.length > 0 && (
              <div>
                <h3 className="text-base font-semibold text-gray-200 mb-3">{t('series.content.standalone')}</h3>
                <SeriesBookGrid
                  books={standaloneBooks}
                  isSelectionMode={selection.isSelectionMode}
                  selectedBooks={selection.selectedBooks}
                  onCardClick={(b) => selection.toggleBook(b.id)}
                  onQuickToggleRead={handleQuickToggleBookRead}
                  onExportComicInfo={handleExportBookComicInfo}
                  onCopyPath={handleCopyBookPath}
                />
              </div>
            )}
          </div>
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
        providerLabel={scrape.searchProvider === 'bangumi' ? 'Bangumi' : scrape.searchProvider}
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
