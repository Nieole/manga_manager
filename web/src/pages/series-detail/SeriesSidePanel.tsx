import { useState, useRef, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { AlertTriangle, GitCompareArrows, Loader2, RefreshCw, Repeat2, X } from 'lucide-react';
import type { MetadataProvenance, MetadataReview, SeriesFailedTask, SeriesRelation, SeriesRelationCandidate } from './types';
import { useI18n } from '../../i18n/LocaleProvider';
import { SeriesRelationsPanel } from './SeriesRelationsPanel';
import { SeriesMetadataReviewPanel } from './SeriesMetadataReviewPanel';

interface SeriesSidePanelProps {
  open: boolean;
  onClose: () => void;
  activeTab: 'relations' | 'metadata' | 'failed';
  onTabChange: (tab: 'relations' | 'metadata' | 'failed') => void;

  relations: SeriesRelation[];
  relationCandidates: SeriesRelationCandidate[];
  relationType: string;
  relationSearch: string;
  selectedTargetId: number | null;
  isAddingRelation: boolean;
  isLoadingCandidates: boolean;
  onRelationTypeChange: (value: string) => void;
  onRelationSearchChange: (value: string) => void;
  onSelectTarget: (id: number) => void;
  onAddRelation: () => void;
  onDeleteRelation: (relation: SeriesRelation) => void;

  metadataReviews: MetadataReview[];
  metadataProvenance: MetadataProvenance[];
  busyMetadataReviewId: number | null;
  onApplyMetadataReview: (id: number) => void;
  onRejectMetadataReview: (id: number) => void;

  failedTasks: SeriesFailedTask[];
  retryingTaskKey: string | null;
  onRetryFailedTask: (taskKey: string) => void;
  taskTypeLabel: (type: string) => string;
}

export function SeriesSidePanel(props: SeriesSidePanelProps) {
  const { t, formatDateTime } = useI18n();
  if (typeof document === 'undefined') return null;

  const counts = {
    relations: props.relations.length,
    metadata: props.metadataReviews.length,
    failed: props.failedTasks.length,
  };

  const [drawerWidth, setDrawerWidth] = useState(() => {
    const saved = localStorage.getItem('komga-series-sidepanel-width');
    return saved ? parseInt(saved, 10) : 576;
  });
  const isDragging = useRef(false);

  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!isDragging.current) return;
      const newWidth = document.documentElement.clientWidth - e.clientX;
      setDrawerWidth(Math.max(320, Math.min(newWidth, 1200))); // Min 320, Max 1200
    };

    const handleMouseUp = () => {
      if (isDragging.current) {
        setDrawerWidth((currentWidth) => {
          localStorage.setItem('komga-series-sidepanel-width', currentWidth.toString());
          return currentWidth;
        });
      }
      isDragging.current = false;
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
    };
  }, []);

  const handleMouseDown = () => {
    isDragging.current = true;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  };

  return createPortal(
    <div className={`fixed inset-0 z-[80] ${props.open ? '' : 'pointer-events-none'}`} aria-hidden={!props.open}>
      <div
        role="presentation"
        onClick={props.onClose}
        className={`absolute inset-0 backdrop-blur-sm transition-opacity duration-300 ${props.open ? 'opacity-100' : 'opacity-0'}`}
        style={{
          background:
            'radial-gradient(circle at top, rgb(var(--theme-glow) / 0.16), transparent 35%), linear-gradient(to bottom, rgb(var(--theme-overlay-top) / 0.78), rgb(var(--theme-overlay-bottom) / 0.88))',
        }}
      />
      <aside
        className={`absolute top-0 right-0 h-full bg-komgaSurface shadow-2xl transition-transform duration-300 ${
          props.open ? 'translate-x-0' : 'translate-x-full'
        }`}
        style={{ width: drawerWidth, maxWidth: '100vw' }}
      >
        <div
          onMouseDown={handleMouseDown}
          className="absolute top-0 left-0 bottom-0 w-2 -ml-1 cursor-col-resize hover:bg-komgaPrimary/50 transition-colors z-50 border-l border-gray-800"
        />
        <div className="flex h-full flex-col">
          <header className="flex items-center justify-between border-b border-gray-800 px-5 py-4">
            <div className="flex items-center gap-2 text-white">
              <GitCompareArrows className="h-5 w-5 text-komgaPrimary" />
              <h3 className="text-lg font-semibold">{t('series.sidePanel.title')}</h3>
            </div>
            <button
              type="button"
              onClick={props.onClose}
              className="p-2 rounded-lg text-gray-400 hover:text-white hover:bg-white/10"
              aria-label={t('common.close')}
            >
              <X className="w-5 h-5" />
            </button>
          </header>

          <nav className="flex border-b border-gray-800 text-sm">
            <TabButton
              active={props.activeTab === 'relations'}
              onClick={() => props.onTabChange('relations')}
              label={t('series.sidePanel.tabs.relations')}
              count={counts.relations}
            />
            <TabButton
              active={props.activeTab === 'metadata'}
              onClick={() => props.onTabChange('metadata')}
              label={t('series.sidePanel.tabs.metadata')}
              count={counts.metadata}
            />
            <TabButton
              active={props.activeTab === 'failed'}
              onClick={() => props.onTabChange('failed')}
              label={t('series.sidePanel.tabs.failed')}
              count={counts.failed}
              danger
            />
          </nav>

          <div className="flex-1 overflow-y-auto p-5">
            {props.activeTab === 'relations' && (
              <SeriesRelationsPanel
                relations={props.relations}
                candidates={props.relationCandidates}
                relationType={props.relationType}
                relationSearch={props.relationSearch}
                selectedTargetId={props.selectedTargetId}
                isAdding={props.isAddingRelation}
                isLoadingCandidates={props.isLoadingCandidates}
                onRelationTypeChange={props.onRelationTypeChange}
                onSearchChange={props.onRelationSearchChange}
                onSelectTarget={props.onSelectTarget}
                onAddRelation={props.onAddRelation}
                onDeleteRelation={props.onDeleteRelation}
              />
            )}
            {props.activeTab === 'metadata' && (
              props.metadataReviews.length === 0 && props.metadataProvenance.length === 0 ? (
                <div className="text-center py-10 text-sm text-gray-500">
                  {t('series.sidePanel.metadataEmpty')}
                </div>
              ) : (
                <SeriesMetadataReviewPanel
                  reviews={props.metadataReviews}
                  provenance={props.metadataProvenance}
                  busyReviewId={props.busyMetadataReviewId}
                  onApply={props.onApplyMetadataReview}
                  onReject={props.onRejectMetadataReview}
                />
              )
            )}
            {props.activeTab === 'failed' && (
              <div className="space-y-3">
                {props.failedTasks.length === 0 ? (
                  <div className="text-center py-10 text-sm text-gray-500">
                    {t('series.sidePanel.failedEmpty')}
                  </div>
                ) : (
                  props.failedTasks.map((task) => (
                    <div key={task.key} className="rounded-xl border border-red-500/20 bg-red-500/5 p-4">
                      <div className="mb-2 flex items-center gap-2 text-xs text-red-200/70">
                        <AlertTriangle className="w-3.5 h-3.5" />
                        <span>{props.taskTypeLabel(task.type)}</span>
                        {task.scope_name && <span>{task.scope_name}</span>}
                      </div>
                      <p className="text-sm font-medium text-white">{task.message}</p>
                      {task.error && <p className="mt-2 text-sm text-red-100/80">{task.error}</p>}
                      <div className="mt-3 flex items-center justify-between text-xs">
                        <span className="text-red-100/40">{formatDateTime(task.updated_at)}</span>
                        {task.retryable && (
                          <button
                            type="button"
                            onClick={() => props.onRetryFailedTask(task.key)}
                            disabled={props.retryingTaskKey === task.key}
                            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-red-500/10 hover:bg-red-500/20 border border-red-500/30 text-red-100 transition-colors disabled:opacity-60"
                          >
                            {props.retryingTaskKey === task.key ? (
                              <Loader2 className="w-3.5 h-3.5 animate-spin" />
                            ) : (
                              <Repeat2 className="w-3.5 h-3.5" />
                            )}
                            {t('series.failedTasks.retry')}
                          </button>
                        )}
                      </div>
                    </div>
                  ))
                )}
              </div>
            )}
          </div>
        </div>
      </aside>
    </div>,
    document.body,
  );
}

function TabButton({
  active,
  onClick,
  label,
  count,
  danger,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  count: number;
  danger?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex-1 px-4 py-3 transition-colors border-b-2 inline-flex items-center justify-center gap-2 ${
        active
          ? danger
            ? 'border-red-500 text-red-200 bg-red-500/5'
            : 'border-komgaPrimary text-white bg-white/5'
          : 'border-transparent text-gray-400 hover:text-white hover:bg-white/5'
      }`}
    >
      <span>{label}</span>
      {count > 0 && (
        <span className={`text-[10px] font-semibold px-1.5 py-0.5 rounded ${danger ? 'bg-red-500/20 text-red-200' : 'bg-komgaPrimary/20 text-komgaPrimary'}`}>
          {count}
        </span>
      )}
    </button>
  );
}

interface SeriesSidePanelBadgeProps {
  pendingMetadata: number;
  failedCount: number;
  onClick: () => void;
}

export function SeriesSidePanelBadge({ pendingMetadata, failedCount, onClick }: SeriesSidePanelBadgeProps) {
  const { t } = useI18n();
  if (pendingMetadata === 0 && failedCount === 0) return null;
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-2 px-3 py-1.5 rounded-lg border border-amber-400/30 bg-amber-400/10 text-xs font-medium text-amber-200 hover:bg-amber-400/20 transition-colors"
    >
      <RefreshCw className="w-3.5 h-3.5" />
      {pendingMetadata > 0 && t('series.sidePanel.badge.metadata', { count: pendingMetadata })}
      {pendingMetadata > 0 && failedCount > 0 && <span className="text-amber-300/60">·</span>}
      {failedCount > 0 && (
        <span className="text-red-200">{t('series.sidePanel.badge.failed', { count: failedCount })}</span>
      )}
    </button>
  );
}
