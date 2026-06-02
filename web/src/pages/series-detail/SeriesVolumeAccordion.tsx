import { ChevronDown, CheckCircle2, FolderOpen } from 'lucide-react';
import type { Book } from './types';
import type { VolumeItem } from './hooks/useSeriesVolumes';
import { SeriesBookGrid } from './SeriesBookGrid';
import { useI18n } from '../../i18n/LocaleProvider';

interface SeriesVolumeAccordionProps {
  volumes: VolumeItem[];
  isOpen: (name: string) => boolean;
  onToggle: (name: string) => void;
  isSelectionMode: boolean;
  selectedVolumes: string[];
  selectedBooks: number[];
  onToggleVolumeSelection: (name: string) => void;
  onCardClick: (book: Book) => void;
  onQuickToggleVolumeRead: (volume: VolumeItem, makeRead: boolean) => void;
  onQuickToggleBookRead: (book: Book, makeRead: boolean) => void;
  onExportComicInfo: (book: Book) => void;
  onCopyPath: (book: Book) => void;
  seriesUpdatedAt?: string;
}

export function SeriesVolumeAccordion({
  volumes,
  isOpen,
  onToggle,
  isSelectionMode,
  selectedVolumes,
  selectedBooks,
  onToggleVolumeSelection,
  onCardClick,
  onQuickToggleVolumeRead,
  onQuickToggleBookRead,
  onExportComicInfo,
  onCopyPath,
  seriesUpdatedAt,
}: SeriesVolumeAccordionProps) {
  const { t } = useI18n();
  if (volumes.length === 0) return null;

  return (
    <div className="space-y-3">
      <div className="space-y-2">
        {volumes.map((volume) => {
          const open = isOpen(volume.name);
          const selected = selectedVolumes.includes(volume.name);
          const isFullyRead = volume.total_pages > 0 && volume.read_pages >= volume.total_pages;
          const progressPct = volume.total_pages > 0 ? Math.min(100, (volume.read_pages / volume.total_pages) * 100) : 0;
          const coverSrc =
            volume.cover_path?.Valid && volume.cover_path?.String && volume.cover_book_id
              ? `/api/covers/${volume.cover_book_id}${seriesUpdatedAt ? `?v=${new Date(seriesUpdatedAt).getTime()}` : ''}`
              : null;

          return (
            <div
              key={volume.name}
              className={`rounded-xl sm:rounded-2xl overflow-hidden bg-gray-950/40 backdrop-blur-xl border border-white/5 shadow-xl transition-all duration-300 ${
                selected ? 'ring-2 ring-komgaPrimary shadow-komgaPrimary/20 bg-komgaPrimary/5' : 'hover:bg-white/5 hover:border-white/10 hover:shadow-2xl'
              }`}
            >
              <div className="flex items-center gap-3 sm:gap-5 p-3 sm:p-5">
                {isSelectionMode && (
                  <button
                    type="button"
                    onClick={() => onToggleVolumeSelection(volume.name)}
                    className={`w-5 h-5 rounded-full border-2 flex items-center justify-center shrink-0 transition-colors ${
                      selected ? 'bg-komgaPrimary border-komgaPrimary' : 'border-gray-500'
                    }`}
                    aria-label={selected ? t('series.selection.unselectVolume') : t('series.selection.selectVolume')}
                  >
                    {selected && <span className="text-white text-xs font-bold leading-none select-none">&#10003;</span>}
                  </button>
                )}
                <div className="w-12 h-16 sm:w-16 sm:h-24 rounded-lg sm:rounded-xl overflow-hidden border border-white/10 bg-gray-950/50 shadow-inner shrink-0 flex items-center justify-center relative group-hover:scale-105 transition-transform duration-500">
                  {coverSrc ? (
                    <img src={coverSrc} alt={volume.name} className="w-full h-full object-cover" loading="lazy" />
                  ) : (
                    <FolderOpen className="w-6 h-6 text-gray-600 drop-shadow-md" />
                  )}
                  <div className="absolute inset-0 ring-1 ring-inset ring-white/10 rounded-xl pointer-events-none" />
                </div>
                <button
                  type="button"
                  onClick={() => onToggle(volume.name)}
                  className="flex-1 min-w-0 text-left group cursor-pointer"
                >
                  <div className="flex items-center justify-between gap-2 sm:gap-3">
                    <h4 className="font-bold text-base sm:text-xl text-white truncate drop-shadow-md group-hover:text-komgaPrimary transition-colors">{volume.name}</h4>
                    <div className={`p-1 sm:p-1.5 rounded-full bg-white/5 border border-white/5 transition-all duration-300 ${open ? 'rotate-180 bg-white/10' : ''}`}>
                      <ChevronDown className="w-4 h-4 shrink-0 text-gray-300" />
                    </div>
                  </div>
                  <div className="mt-1.5 sm:mt-2 flex flex-wrap items-center gap-2 sm:gap-4 text-[10px] sm:text-xs font-semibold text-gray-400 uppercase tracking-wide">
                    <span className="bg-black/20 px-1.5 py-0.5 rounded-md">{t('series.content.bookCount', { count: volume.books.length })}</span>
                    <span className="bg-black/20 px-1.5 py-0.5 rounded-md">{t('series.content.pageCount', { count: volume.total_pages })}</span>
                    {volume.total_pages > 0 && (
                      <span className={`px-1.5 py-0.5 rounded-md ${isFullyRead ? 'bg-green-500/20 text-green-300' : 'bg-komgaPrimary/20 text-komgaPrimary'}`}>
                        {Math.round(progressPct)}%
                      </span>
                    )}
                  </div>
                  {volume.total_pages > 0 && (
                    <div className="mt-2.5 h-1.5 rounded-full bg-gray-950/50 border border-white/5 overflow-hidden shadow-inner">
                      <div
                        className={`h-full transition-all duration-700 ease-out ${isFullyRead ? 'bg-linear-to-r from-green-500 to-green-400 shadow-[0_0_10px_rgba(34,197,94,0.5)]' : 'bg-linear-to-r from-komgaPrimary to-komgaPrimaryHover shadow-[0_0_10px_rgba(var(--rgb-komga-primary),0.5)]'}`}
                        style={{ width: `${progressPct}%` }}
                      />
                    </div>
                  )}
                </button>
                {!isSelectionMode && (
                  <button
                    type="button"
                    onClick={() => onQuickToggleVolumeRead(volume, !isFullyRead)}
                    className="p-1.5 sm:p-2 rounded-lg text-gray-400 hover:text-green-400 hover:bg-green-400/10 transition-colors shrink-0"
                    title={isFullyRead ? t('series.content.markVolumeUnread') : t('series.content.markVolumeRead')}
                  >
                    <CheckCircle2 className={`w-4 h-4 ${isFullyRead ? 'text-green-400 fill-green-400/20' : ''}`} />
                  </button>
                )}
              </div>

              {open && (
                <div className="border-t border-white/5 p-3 sm:p-5 bg-gray-950/20 shadow-inner">
                  <SeriesBookGrid
                    books={volume.books}
                    isSelectionMode={isSelectionMode}
                    selectedBooks={selectedBooks}
                    onCardClick={onCardClick}
                    onQuickToggleRead={onQuickToggleBookRead}
                    onExportComicInfo={onExportComicInfo}
                    onCopyPath={onCopyPath}
                  />
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
