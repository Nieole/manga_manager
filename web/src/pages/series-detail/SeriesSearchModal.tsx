import { ArrowLeft, BookImage, Building2, CheckCircle2, Download, Edit, FolderOpen, Globe, Info, Lock, Sparkles, Star } from 'lucide-react';
import type { MetaTag, SearchResult, Series } from './types';
import { ModalShell } from '../../components/ui/ModalShell';
import { modalInputClass, modalPrimaryButtonClass, modalSecondaryButtonClass, modalSectionClass } from '../../components/ui/modalStyles';
import { useI18n } from '../../i18n/LocaleProvider';

interface SeriesSearchModalProps {
  open: boolean;
  providerLabel: string;
  modalSearchQuery: string;
  isScraping: boolean;
  searchResults: SearchResult[];
  currentOffset: number;
  searchTotal: number;
  currentSeries: Series | null;
  currentTags: MetaTag[];
  lockedFields: Set<string>;
  selectedResult: SearchResult | null;
  onClose: () => void;
  onSearchQueryChange: (value: string) => void;
  onReSearch: (offset?: number) => void;
  onSelectMetadata: (metadata: SearchResult | null) => void;
  onApplyMetadata: (metadata: SearchResult) => void;
}

interface PreviewField {
  key: string;
  label: string;
  currentValue: string;
  nextValue: string;
}

export function SeriesSearchModal({
  open,
  providerLabel,
  modalSearchQuery,
  isScraping,
  searchResults,
  currentOffset,
  searchTotal,
  currentSeries,
  currentTags,
  lockedFields,
  selectedResult,
  onClose,
  onSearchQueryChange,
  onReSearch,
  onSelectMetadata,
  onApplyMetadata,
}: SeriesSearchModalProps) {
  const { t } = useI18n();
  if (!open) return null;

  const previewFields: PreviewField[] = selectedResult && currentSeries ? [
    {
      key: 'title',
      label: t('series.searchModal.field.title'),
      currentValue: currentSeries.title?.Valid ? currentSeries.title.String : currentSeries.name,
      nextValue: selectedResult.Title || t('series.searchModal.notProvided'),
    },
    {
      key: 'summary',
      label: t('series.searchModal.field.summary'),
      currentValue: currentSeries.summary?.Valid ? currentSeries.summary.String : t('series.searchModal.notFilled'),
      nextValue: selectedResult.Summary || t('series.searchModal.notProvided'),
    },
    {
      key: 'publisher',
      label: t('series.searchModal.field.publisher'),
      currentValue: currentSeries.publisher?.Valid ? currentSeries.publisher.String : t('series.searchModal.notFilled'),
      nextValue: selectedResult.Publisher || t('series.searchModal.notProvided'),
    },
    {
      key: 'status',
      label: t('series.searchModal.field.status'),
      currentValue: currentSeries.status?.Valid ? currentSeries.status.String : t('series.searchModal.notFilled'),
      nextValue: selectedResult.Status || t('series.searchModal.notProvided'),
    },
    {
      key: 'rating',
      label: t('series.searchModal.field.rating'),
      currentValue: currentSeries.rating?.Valid ? currentSeries.rating.Float64.toFixed(1) : t('series.searchModal.notFilled'),
      nextValue: selectedResult.Rating > 0 ? selectedResult.Rating.toFixed(1) : t('series.searchModal.notProvided'),
    },
    {
      key: 'tags',
      label: t('series.searchModal.field.tags'),
      currentValue: currentTags.length > 0 ? currentTags.map((tag) => tag.name).join(' / ') : t('series.searchModal.notFilled'),
      nextValue: selectedResult.Tags?.length ? selectedResult.Tags.join(' / ') : t('series.searchModal.notProvided'),
    },
  ] : [];

  const changedFieldCount = previewFields.filter((field) => field.currentValue !== field.nextValue && field.nextValue !== t('series.searchModal.notProvided')).length;

  return (
    <ModalShell
      open={open}
      onClose={onClose}
      title={t('series.searchModal.title')}
      description={t('series.searchModal.description', { provider: providerLabel || t('series.searchModal.externalSource') })}
      icon={<Globe className="h-5 w-5" />}
      size="wide"
      zIndexClassName="z-[60]"
      panelClassName="max-w-7xl"
      bodyClassName="min-h-0 overflow-hidden p-0"
      headerContent={
        <div className="flex w-full max-w-xl gap-2">
          <div className="relative flex-1">
            <input
              type="text"
              value={modalSearchQuery}
              onChange={(e) => onSearchQueryChange(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && onReSearch(0)}
              placeholder={t('series.searchModal.searchPlaceholder')}
              className={`${modalInputClass} pl-10`}
            />
            <Edit className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500" />
          </div>
          <button
            onClick={() => onReSearch(0)}
            disabled={isScraping}
            className={`${modalPrimaryButtonClass} shrink-0`}
          >
            {isScraping ? (
              <div className="h-4 w-4 animate-spin rounded-full border-2 border-white/30 border-t-white" />
            ) : (
              <Download className="h-4 w-4" />
            )}
            {t('series.searchModal.research')}
          </button>
        </div>
      }
      footer={
        <div className="flex w-full flex-col items-center justify-between gap-4 lg:flex-row">
          <div className="flex items-center gap-3">
            <button
              onClick={() => onReSearch(Math.max(0, currentOffset - 20))}
              disabled={isScraping || currentOffset === 0}
              className={modalSecondaryButtonClass}
            >
              {t('home.pagination.prev')}
            </button>
            <span className="text-sm text-gray-500">
              {t('home.pagination.currentPage', { page: Math.floor(currentOffset / 20) + 1, total: Math.max(1, Math.ceil(searchTotal / 20)) })}
            </span>
            <button
              onClick={() => onReSearch(currentOffset + 20)}
              disabled={isScraping || currentOffset + 20 >= searchTotal}
              className={modalSecondaryButtonClass}
            >
              {t('home.pagination.next')}
            </button>
          </div>
          {selectedResult ? (
            <div className="flex w-full flex-col sm:w-auto sm:flex-row items-center gap-3 sm:gap-4 lg:w-auto lg:ml-auto">
              <div className="text-sm text-gray-400 hidden sm:block text-right">
                <p className="mb-0.5 leading-relaxed">{t('series.searchModal.lockHint')}</p>
                <p className="text-xs text-gray-500">{t('series.searchModal.compareHint')}</p>
              </div>
              <button
                onClick={() => onApplyMetadata(selectedResult)}
                disabled={isScraping}
                className={`${modalPrimaryButtonClass} w-full sm:w-auto shrink-0 shadow-lg shadow-komgaPrimary/20 hover:shadow-xl hover:shadow-komgaPrimary/30 px-6 py-2.5 justify-center`}
              >
                {isScraping ? <div className="w-4 h-4 animate-spin rounded-full border-2 border-white/30 border-t-white" /> : <CheckCircle2 className="w-5 h-5" />}
                <span className="font-bold">{t('series.searchModal.apply')}</span>
              </button>
            </div>
          ) : (
            <p className="flex items-center gap-2 text-xs italic text-gray-500">
              <Info className="h-4 w-4" />
              {t('series.searchModal.footerHint')}
            </p>
          )}
        </div>
      }
    >
        <div className="flex h-full min-h-0 flex-col flex-1 xl:grid xl:grid-cols-[1.1fr_0.9fr]">
          <div className={`border-gray-800 min-h-0 min-w-0 xl:border-r ${selectedResult ? 'hidden xl:block' : 'block flex-1'}`}>
            <div className="p-4 sm:p-6 overflow-y-auto space-y-4 h-full xl:max-h-full">
              {searchResults.length > 0 ? (
                searchResults.map((result, idx) => {
                  const isSelected = selectedResult?.SourceID === result.SourceID && selectedResult?.Title === result.Title;
                  return (
                    <button
                      key={`${result.SourceID}-${idx}`}
                      type="button"
                      onClick={() => onSelectMetadata(result)}
                      className={`group w-full text-left flex gap-5 p-5 rounded-2xl border transition-all cursor-pointer relative overflow-hidden ${isSelected ? 'border-komgaPrimary bg-komgaPrimary/10 shadow-lg shadow-komgaPrimary/10' : 'bg-gray-900/40 border-gray-800 hover:border-komgaPrimary/40 hover:bg-komgaPrimary/5'}`}
                    >
                      <div className="w-24 sm:w-28 shrink-0 aspect-[3/4] bg-gray-800 rounded-xl overflow-hidden border border-gray-700 shadow-xl self-start">
                        {result.CoverURL ? (
                          <img src={result.CoverURL} alt={result.Title} className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500" />
                        ) : (
                          <div className="w-full h-full flex items-center justify-center">
                            <BookImage className="w-10 h-10 text-gray-700" />
                          </div>
                        )}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-start justify-between gap-3">
                          <div className="min-w-0">
                            <h4 className="text-lg font-bold text-white leading-tight break-words">{result.Title}</h4>
                            {result.OriginalTitle && result.OriginalTitle !== result.Title && (
                              <p className="text-sm text-gray-500 truncate mt-1 italic">{result.OriginalTitle}</p>
                            )}
                          </div>
                          {isSelected && (
                            <span className="inline-flex items-center gap-1 rounded-full border border-komgaPrimary/30 bg-komgaPrimary/10 px-2 py-1 text-xs text-komgaPrimary">
                              <CheckCircle2 className="w-3.5 h-3.5" />
                              {t('series.searchModal.selected')}
                            </span>
                          )}
                        </div>

                        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 mt-3">
                          {result.Publisher && (
                            <p className="text-purple-400 text-xs font-semibold flex items-center gap-1.5 bg-purple-400/5 px-2 py-1 rounded border border-purple-400/10">
                              <Building2 className="w-3.5 h-3.5" />
                              {result.Publisher}
                            </p>
                          )}
                          {result.ReleaseDate && (
                            <p className="text-blue-400 text-xs font-semibold flex items-center gap-1.5 bg-blue-400/5 px-2 py-1 rounded border border-blue-400/10">
                              <Info className="w-3.5 h-3.5" />
                              {result.ReleaseDate}
                            </p>
                          )}
                          {result.VolumeCount > 0 && (
                            <p className="text-green-400 text-xs font-semibold flex items-center gap-1.5 bg-green-400/5 px-2 py-1 rounded border border-green-400/10">
                              <FolderOpen className="w-3.5 h-3.5" />
                              {t('series.searchModal.volumeCount', { count: result.VolumeCount })}
                            </p>
                          )}
                          {result.Rating > 0 && (
                            <p className="text-yellow-400 text-xs font-semibold flex items-center gap-1.5 bg-yellow-400/5 px-2 py-1 rounded border border-yellow-400/10">
                              <Star className="w-3.5 h-3.5 fill-current" />
                              {result.Rating.toFixed(1)}
                            </p>
                          )}
                        </div>

                        <div className="mt-3 flex flex-wrap gap-2">
                          {result.Tags?.slice(0, 6).map((tag) => (
                            <span key={tag} className="text-[11px] bg-gray-800/60 text-gray-400 px-2.5 py-1 rounded-full border border-gray-700/50 truncate max-w-full">
                              {tag}
                            </span>
                          ))}
                        </div>

                        <p className="text-gray-400 text-sm mt-4 line-clamp-3 leading-relaxed">
                          {result.Summary || t('series.searchModal.noSummary')}
                        </p>
                      </div>
                    </button>
                  );
                })
              ) : (
                <div className="flex flex-col items-center justify-center py-20 text-gray-500 gap-4">
                  <Globe className="w-16 h-16 opacity-20" />
                  <p>{t('series.searchModal.noResult')}</p>
                </div>
              )}
            </div>
          </div>

          <div className={`flex-col bg-gray-950/30 min-w-0 h-full ${selectedResult ? 'flex flex-1' : 'hidden xl:flex'}`}>
            {selectedResult ? (
              <>
                <div className="flex-1 overflow-y-auto p-4 sm:p-6 space-y-5">
                  <button
                    type="button"
                    onClick={() => onSelectMetadata(null)}
                    className="xl:hidden flex items-center gap-2 text-sm text-gray-400 hover:text-white mb-2 transition-colors"
                  >
                    <ArrowLeft className="w-4 h-4" />
                    {t('common.back') || 'Back'}
                  </button>
                  <div className="rounded-2xl border border-gray-800 bg-gray-900/60 p-4 sm:p-5">
                    <div className="flex items-start justify-between gap-4">
                      <div>
                        <p className="text-xs uppercase tracking-[0.18em] text-gray-500">{t('series.searchModal.preview')}</p>
                        <h4 className="mt-2 text-xl font-bold text-white">{selectedResult.Title}</h4>
                        <p className="text-sm text-gray-500 mt-1">{t('series.searchModal.source', { provider: providerLabel || t('series.searchModal.externalSource'), id: selectedResult.SourceID })}</p>
                      </div>
                      <div className="rounded-full border border-komgaPrimary/20 bg-komgaPrimary/10 px-3 py-1 text-sm text-komgaPrimary">
                        {t('series.searchModal.changedFields', { count: changedFieldCount })}
                      </div>
                    </div>
                  </div>

                  <div className={`${modalSectionClass} bg-gray-900/40 p-5`}>
                    <div className="flex items-center gap-2 mb-4 text-white">
                      <Sparkles className="w-4 h-4 text-komgaPrimary" />
                      <h5 className="font-semibold">{t('series.searchModal.diffTitle')}</h5>
                    </div>
                    <div className="space-y-3">
                      {previewFields.map((field) => {
                        const locked = lockedFields.has(field.key);
                        const changed = field.currentValue !== field.nextValue && field.nextValue !== t('series.searchModal.notProvided');
                        return (
                          <div key={field.key} className={`rounded-xl border p-4 ${changed ? 'border-komgaPrimary/20 bg-komgaPrimary/5' : 'border-gray-800 bg-black/10'}`}>
                            <div className="flex items-center justify-between gap-3 mb-3">
                              <span className="text-sm font-medium text-white">{field.label}</span>
                              {locked ? (
                                <span className="inline-flex items-center gap-1 rounded-full border border-amber-500/20 bg-amber-500/10 px-2 py-1 text-xs text-amber-200">
                                  <Lock className="w-3 h-3" />
                                  {t('series.searchModal.locked')}
                                </span>
                              ) : changed ? (
                                <span className="rounded-full border border-komgaPrimary/20 bg-komgaPrimary/10 px-2 py-1 text-xs text-komgaPrimary">{t('series.searchModal.willUpdate')}</span>
                              ) : (
                                <span className="rounded-full border border-gray-700 bg-gray-800 px-2 py-1 text-xs text-gray-400">{t('series.searchModal.noChange')}</span>
                              )}
                            </div>
                            <div className="grid gap-3 md:grid-cols-2">
                              <div>
                                <p className="text-[11px] uppercase tracking-[0.16em] text-gray-500 mb-1">{t('series.searchModal.current')}</p>
                                <div className="rounded-lg border border-gray-800 bg-black/20 px-3 py-2 text-sm text-gray-300 whitespace-pre-wrap break-words">
                                  {field.currentValue}
                                </div>
                              </div>
                              <div>
                                <p className="text-[11px] uppercase tracking-[0.16em] text-gray-500 mb-1">{t('series.searchModal.next')}</p>
                                <div className="rounded-lg border border-gray-800 bg-black/20 px-3 py-2 text-sm text-gray-200 whitespace-pre-wrap break-words">
                                  {field.nextValue}
                                </div>
                              </div>
                            </div>
                          </div>
                        );
                      })}
                  </div>
                </div>
              </div>
            </>
            ) : (
              <div className="flex-1 flex items-center justify-center p-6">
                <div className="flex h-full w-full min-h-[360px] items-center justify-center rounded-2xl border border-dashed border-gray-800 bg-gray-900/20 p-6 text-center text-gray-500">
                  {t('series.searchModal.pickCandidate')}
                </div>
              </div>
            )}
          </div>
        </div>
    </ModalShell>
  );
}
