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
      <h3 className="text-base font-semibold text-gray-200 flex items-center gap-2">
        <FolderOpen className="w-5 h-5 text-komgaPrimary" />
        {t('series.content.volumes')}
      </h3>
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
              className={`rounded-xl border overflow-hidden bg-komgaSurface/70 backdrop-blur-sm transition-colors ${
                selected ? 'border-komgaPrimary' : 'border-gray-800'
              }`}
            >
              <div className="flex items-center gap-3 p-3">
                {isSelectionMode && (
                  <button
                    type="button"
                    onClick={() => onToggleVolumeSelection(volume.name)}
                    className={`w-5 h-5 rounded-full border-2 flex items-center justify-center shrink-0 transition-colors ${
                      selected ? 'bg-komgaPrimary border-komgaPrimary' : 'border-gray-500'
                    }`}
                    aria-label={selected ? t('series.selection.unselectVolume') : t('series.selection.selectVolume')}
                  >
                    {selected && <span className="text-white text-xs font-bold leading-none select-none">✓</span>}
                  </button>
                )}
                <div className="w-12 h-16 rounded-lg overflow-hidden border border-white/10 bg-gray-900 shrink-0 flex items-center justify-center">
                  {coverSrc ? (
                    <img src={coverSrc} alt={volume.name} className="w-full h-full object-cover" loading="lazy" />
                  ) : (
                    <FolderOpen className="w-6 h-6 text-gray-700" />
                  )}
                </div>
                <button
                  type="button"
                  onClick={() => onToggle(volume.name)}
                  className="flex-1 min-w-0 text-left"
                >
                  <div className="flex items-center justify-between gap-3">
                    <h4 className="font-medium text-gray-100 truncate">{volume.name}</h4>
                    <ChevronDown className={`w-4 h-4 shrink-0 text-gray-400 transition-transform ${open ? 'rotate-180' : ''}`} />
                  </div>
                  <div className="mt-1.5 flex items-center gap-3 text-xs text-gray-400">
                    <span>{t('series.content.bookCount', { count: volume.books.length })}</span>
                    <span>{t('series.content.pageCount', { count: volume.total_pages })}</span>
                    {volume.total_pages > 0 && (
                      <span className={isFullyRead ? 'text-green-400' : 'text-komgaPrimary'}>
                        {Math.round(progressPct)}%
                      </span>
                    )}
                  </div>
                  {volume.total_pages > 0 && (
                    <div className="mt-1.5 h-1 rounded-full bg-gray-800 overflow-hidden">
                      <div
                        className={`h-full transition-all duration-500 ${isFullyRead ? 'bg-green-500' : 'bg-komgaPrimary'}`}
                        style={{ width: `${progressPct}%` }}
                      />
                    </div>
                  )}
                </button>
                {!isSelectionMode && (
                  <button
                    type="button"
                    onClick={() => onQuickToggleVolumeRead(volume, !isFullyRead)}
                    className="p-2 rounded-lg text-gray-400 hover:text-green-400 hover:bg-green-400/10 transition-colors shrink-0"
                    title={isFullyRead ? t('series.content.markVolumeUnread') : t('series.content.markVolumeRead')}
                  >
                    <CheckCircle2 className={`w-4 h-4 ${isFullyRead ? 'text-green-400 fill-green-400/20' : ''}`} />
                  </button>
                )}
              </div>

              {open && (
                <div className="border-t border-gray-800 p-4 bg-black/10">
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
