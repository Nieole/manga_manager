import { BookImage, FolderOpen, CheckCircle2 } from 'lucide-react';
import type { Book, NullString } from './types';
import { useI18n } from '../../i18n/LocaleProvider';

interface VolumeItem {
  name: string;
  books: Book[];
  cover_path?: NullString;
  cover_book_id?: number;
  total_pages: number;
  read_pages: number;
}

interface SeriesContentSectionProps {
  loading: boolean;
  selectedVolume: string | null;
  activeVolumeBooks: Book[];
  volumes: VolumeItem[];
  standaloneBooks: Book[];
  books: Book[];
  isSelectionMode: boolean;
  selectedVolumes: string[];
  seriesUpdatedAt?: string;
  onSelectVolume: (name: string) => void;
  onToggleSelectedVolume: (name: string) => void;
  onQuickMarkVolumeRead: (e: React.MouseEvent, volumeName: string, isRead: boolean) => void;
  renderBookCard: (book: Book) => React.ReactNode;
}

export function SeriesContentSection({
  loading,
  selectedVolume,
  activeVolumeBooks,
  volumes,
  standaloneBooks,
  books,
  isSelectionMode,
  selectedVolumes,
  seriesUpdatedAt,
  onSelectVolume,
  onToggleSelectedVolume,
  onQuickMarkVolumeRead,
  renderBookCard,
}: SeriesContentSectionProps) {
  const { t } = useI18n();
  if (loading) {
    return <div className="text-center py-20 text-gray-500 animate-pulse">{t('series.content.loading')}</div>;
  }

  if (selectedVolume) {
    return <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4 sm:gap-6">{activeVolumeBooks.map(renderBookCard)}</div>;
  }

  return (
    <div className="space-y-10">
      {volumes.length > 0 && (
        <div>
          <h3 className="text-lg font-semibold text-gray-300 mb-4 flex items-center">
            <FolderOpen className="w-5 h-5 mr-2 text-komgaPrimary" /> {t('series.content.volumes')}
          </h3>
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4 sm:gap-6">
            {volumes.map((volume) => {
              const isSelected = selectedVolumes.includes(volume.name);
              const handleVolClick = () => {
                if (isSelectionMode) {
                  onToggleSelectedVolume(volume.name);
                } else {
                  onSelectVolume(volume.name);
                }
              };

              return (
                <div
                  key={volume.name}
                  onClick={handleVolClick}
                  className={`group flex flex-col rounded-xl overflow-hidden bg-gray-900 border ${isSelected ? 'border-komgaPrimary ring-2 ring-komgaPrimary shadow-lg shadow-komgaPrimary/20' : 'border-gray-800 hover:border-komgaPrimary/50 hover:bg-gray-800'} transition-all duration-300 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10 cursor-pointer`}
                >
                  <div className="aspect-[3/4] w-full bg-komgaDark flex items-center justify-center relative overflow-hidden">
                    {isSelectionMode && (
                      <div className="absolute top-2 left-2 z-30">
                        <div className={`w-5 h-5 rounded-full border-2 flex items-center justify-center transition-colors ${isSelected ? 'bg-komgaPrimary border-komgaPrimary' : 'bg-black/50 border-gray-400'}`}>
                          {isSelected && <span className="text-white text-xs font-bold leading-none select-none">✓</span>}
                        </div>
                      </div>
                    )}
                    {volume.cover_path?.Valid && volume.cover_path?.String && volume.cover_book_id ? (
                      <img
                        src={`/api/covers/${volume.cover_book_id}${seriesUpdatedAt ? `?v=${new Date(seriesUpdatedAt).getTime()}` : ''}`}
                        className="absolute inset-0 w-full h-full object-cover opacity-80 transition-transform duration-500 group-hover:scale-105"
                        alt={t('common.cover')}
                        loading="lazy"
                      />
                    ) : (
                      <FolderOpen className="w-16 h-16 text-gray-700 opacity-50 group-hover:text-komgaPrimary transition-colors relative z-10" />
                    )}

                    {!isSelectionMode && (
                      <button
                        onClick={(e) => onQuickMarkVolumeRead(e, volume.name, !(volume.read_pages >= volume.total_pages))}
                        className="absolute top-2 right-2 z-30 p-1.5 rounded-full bg-black/60 border border-white/10 text-white/40 hover:text-green-400 hover:bg-green-400/20 hover:border-green-400/40 transition-all opacity-0 group-hover:opacity-100 backdrop-blur"
                        title={volume.read_pages >= volume.total_pages ? t('series.content.markVolumeUnread') : t('series.content.markVolumeRead')}
                      >
                        <CheckCircle2 className={`w-4 h-4 ${volume.read_pages >= volume.total_pages ? 'text-green-400 fill-green-400/20' : ''}`} />
                      </button>
                    )}

                    <div className="absolute inset-0 bg-gradient-to-t from-gray-900/90 via-gray-900/30 to-transparent flex items-end p-3 z-10 pointer-events-none">
                      <div className="w-full flex justify-between items-center text-xs font-semibold text-gray-300">
                        <span>{t('series.content.bookCount', { count: volume.books.length })}</span>
                        <span>{t('series.content.pageCount', { count: volume.total_pages })}</span>
                      </div>
                    </div>

                    {volume.total_pages > 0 && volume.read_pages > 0 && (
                      <div className="absolute inset-x-0 bottom-0 h-1 bg-gray-800/40 z-20">
                        <div
                          className={`h-full transition-all duration-500 ${volume.read_pages >= volume.total_pages ? 'bg-green-500' : 'bg-komgaPrimary'}`}
                          style={{ width: `${Math.min(100, (volume.read_pages / volume.total_pages) * 100)}%` }}
                        />
                      </div>
                    )}

                    {!isSelectionMode && (
                      <div className="absolute top-2 left-2 bg-komgaPrimary/90 text-white text-[10px] uppercase font-bold px-2 py-0.5 rounded shadow-lg opacity-80 group-hover:opacity-100 transition-opacity z-20">
                        Volume
                      </div>
                    )}
                  </div>

                  <div className="p-4 flex-1">
                    <h4 className="text-sm font-bold text-gray-200 line-clamp-2 leading-snug group-hover:text-komgaPrimary transition-colors">{volume.name}</h4>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {standaloneBooks.length > 0 && (
        <div>
          <h3 className="text-lg font-semibold text-gray-300 mb-4 flex items-center">
            <BookImage className="w-5 h-5 mr-2 text-komgaPrimary" /> {t('series.content.standalone')}
          </h3>
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4 sm:gap-6">{standaloneBooks.map(renderBookCard)}</div>
        </div>
      )}

      {books.length === 0 && <div className="text-center py-20 text-gray-500">{t('series.content.empty')}</div>}
    </div>
  );
}
