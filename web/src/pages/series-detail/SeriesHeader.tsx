import { ArrowLeft, BookImage, Building2, Download, Edit, FolderHeart, FolderOpen, Globe, Info, RefreshCw, Star, Tag, User } from 'lucide-react';
import type { Author, Book, MetaTag, Series, SeriesLink } from './types';
import { normalizeSeriesStatus } from '../../i18n/status';
import { useI18n } from '../../i18n/LocaleProvider';

interface SeriesHeaderProps {
  coverUrl?: string | null;
  selectedVolume: string | null;
  seriesInfo: Series | null;
  books: Book[];
  volumes: Array<{ name: string; books: Book[] }>;
  standaloneBooks: Book[];
  activeVolumeBooks: Book[];
  tags: MetaTag[];
  authors: Author[];
  links: SeriesLink[];
  lockedFields: Set<string>;
  isSelectionMode: boolean;
  isOpeningDirectory: boolean;
  isRescanning: boolean;
  isScraping: boolean;
  scrapeMenuOpen: boolean;
  onBack: () => void;
  onToggleSelectionMode: () => void;
  onEdit: () => void;
  onAddToCollection: () => void;
  onOpenDirectory: () => void;
  onRescan: () => void;
  onToggleScrapeMenu: () => void;
  onCloseScrapeMenu: () => void;
  onScrape: (provider: string) => void;
}

export function SeriesHeader({
  coverUrl,
  selectedVolume,
  seriesInfo,
  books,
  volumes,
  standaloneBooks,
  activeVolumeBooks,
  tags,
  authors,
  links,
  lockedFields,
  isSelectionMode,
  isOpeningDirectory,
  isRescanning,
  isScraping,
  scrapeMenuOpen,
  onBack,
  onToggleSelectionMode,
  onEdit,
  onAddToCollection,
  onOpenDirectory,
  onRescan,
  onToggleScrapeMenu,
  onCloseScrapeMenu,
  onScrape,
}: SeriesHeaderProps) {
  const { t } = useI18n();

  return (
    <div className="mb-10 flex flex-col md:flex-row gap-8 items-start relative z-10 border-b border-gray-800/50 pb-8">
      {/* Cover Image */}
      <div className="shrink-0 w-48 md:w-64 lg:w-72 rounded-2xl shadow-xl overflow-hidden self-center md:self-start border border-white/10 bg-komgaSurface group relative">
          {coverUrl ? (
            <img src={coverUrl} alt={t('common.cover')} className="w-full h-auto object-cover aspect-[2/3] transition-transform duration-700 group-hover:scale-105" />
          ) : (
            <div className="w-full aspect-[2/3] flex flex-col items-center justify-center bg-komgaSurface/50">
              <BookImage className="w-16 h-16 text-gray-500 opacity-50 mb-4" />
              <span className="text-gray-400 text-sm font-medium">{t('common.none')}</span>
            </div>
          )}
          <div className="absolute inset-0 ring-1 ring-inset ring-white/10 rounded-2xl pointer-events-none"></div>
      </div>

      {/* Info Content */}
      <div className="flex-1 min-w-0 flex flex-col w-full">
        <button
          onClick={onBack}
          className="flex items-center text-gray-200 hover:text-white transition-colors text-sm font-medium mb-4 self-start bg-komgaSurface/80 hover:bg-komgaSurface px-3 py-1.5 rounded-lg border border-transparent hover:border-white/10 backdrop-blur-sm shadow-sm"
        >
          <ArrowLeft className="w-4 h-4 mr-1.5" />
          {selectedVolume ? t('series.header.backToSeries') : t('series.header.backToLibrary')}
        </button>

        <h2 className="text-3xl sm:text-4xl lg:text-5xl font-extrabold text-white tracking-tight break-words leading-tight mb-5 flex items-center">
            {selectedVolume ? (
              <>
                <FolderOpen className="w-10 h-10 mr-4 text-komgaPrimary" />
                {selectedVolume}
              </>
            ) : (
              seriesInfo?.title?.Valid ? seriesInfo.title.String : seriesInfo?.name || t('series.header.seriesOverview')
            )}
        </h2>

        {/* Badges */}
        {!selectedVolume && seriesInfo && (
          <div className="flex flex-wrap items-center gap-2 text-sm font-medium mb-6">
            {seriesInfo.rating?.Valid && (
              <span className="flex items-center text-komgaPrimary bg-komgaPrimary/10 px-3 py-1.5 rounded-lg border border-komgaPrimary/20 shadow-sm backdrop-blur-md transition-colors hover:bg-komgaPrimary/20">
                <Star className="w-4 h-4 mr-1.5 fill-current" />
                {seriesInfo.rating.Float64.toFixed(1)}
              </span>
            )}
            {seriesInfo.status?.Valid && (
              <span className="flex items-center text-komgaSecondary bg-komgaSecondary/10 px-3 py-1.5 rounded-lg border border-komgaSecondary/20 shadow-sm backdrop-blur-md transition-colors hover:bg-komgaSecondary/20">
                <Info className="w-4 h-4 mr-1.5" />
                {t(`status.${normalizeSeriesStatus(seriesInfo.status.String)}`)}
              </span>
            )}
            {seriesInfo.language?.Valid && (
              <span className="flex items-center text-gray-100 bg-white/10 px-3 py-1.5 rounded-lg border border-white/10 shadow-sm uppercase tracking-wider backdrop-blur-md transition-colors hover:bg-white/20">
                <Globe className="w-4 h-4 mr-1.5" />
                {seriesInfo.language.String}
              </span>
            )}
            {seriesInfo.publisher?.Valid && (
              <span className="flex items-center text-gray-100 bg-komgaSurface/80 px-3 py-1.5 rounded-lg border border-white/10 shadow-sm backdrop-blur-md transition-colors hover:bg-komgaSurface">
                <Building2 className="w-4 h-4 mr-1.5" />
                {seriesInfo.publisher.String}
              </span>
            )}
          </div>
        )}

        {/* Authors & Tags */}
        {!selectedVolume && (tags.length > 0 || authors.length > 0) && (
          <div className="flex flex-col gap-3 mb-6">
            {authors.length > 0 && (
              <div className="flex items-start gap-3">
                <User className="w-5 h-5 text-gray-400 shrink-0 mt-0.5" />
                <div className="flex flex-wrap gap-2">
                  {authors.map((author) => (
                    <span key={author.id} className="text-sm font-medium text-gray-100 bg-white/5 px-2.5 py-1 rounded-md border border-white/10 shadow-sm hover:bg-white/10 transition-colors backdrop-blur-md">
                      {author.name}
                      <span className="text-gray-400 ml-1.5 font-normal text-xs">({author.role})</span>
                    </span>
                  ))}
                </div>
              </div>
            )}
            {tags.length > 0 && (
              <div className="flex items-start gap-3">
                <Tag className="w-5 h-5 text-komgaPrimary/80 shrink-0 transform rotate-90 mt-0.5" />
                <div className="flex flex-wrap gap-2">
                  {tags.map((tag) => (
                    <span key={tag.id} className="text-xs font-semibold text-komgaSecondary bg-komgaSecondary/10 px-2.5 py-1.5 rounded-md border border-komgaSecondary/20 shadow-sm hover:bg-komgaSecondary/20 transition-colors cursor-pointer backdrop-blur-md">
                      {tag.name}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Abstract */}
        {!selectedVolume && seriesInfo?.summary?.Valid && (
          <p className="text-gray-100 text-sm leading-relaxed max-w-4xl line-clamp-3 hover:line-clamp-none transition-all cursor-pointer bg-komgaSurface/80 p-5 rounded-xl border border-white/5 relative group backdrop-blur-md shadow-sm mb-6">
            <span className="absolute -left-px top-1/2 -translate-y-1/2 w-1 h-3/4 bg-komgaPrimary/50 rounded-r-full group-hover:bg-komgaPrimary transition-colors opacity-50 group-hover:opacity-100"></span>
            {seriesInfo.summary.String}
          </p>
        )}

        {/* Lock Info & Links */}
        {!selectedVolume && (lockedFields.size > 0 || links.length > 0) && (
          <div className="flex flex-wrap gap-6 mb-6">
            {lockedFields.size > 0 && (
              <div className="flex-1 min-w-[280px] rounded-xl border border-komgaSecondary/20 bg-komgaSecondary/5 p-4 backdrop-blur-md">
                <p className="text-sm font-medium text-komgaSecondary mb-2">{t('series.header.lockedFields', { count: lockedFields.size })}</p>
                <div className="flex flex-wrap gap-2">
                  {Array.from(lockedFields).map((field) => (
                    <span key={field} className="rounded-md border border-komgaSecondary/20 bg-komgaSecondary/10 px-2 py-0.5 text-xs text-komgaSecondary">{field}</span>
                  ))}
                </div>
              </div>
            )}
            {links.length > 0 && (
               <div className="flex flex-wrap gap-2 items-center">
                 {links.map((link, index) => (
                    <a key={index} href={link.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center text-sm font-medium px-4 py-2 bg-white/5 hover:bg-komgaPrimary text-gray-100 hover:text-white border border-white/10 hover:border-komgaPrimary/50 rounded-lg transition-all shadow-sm backdrop-blur-md">
                      <Globe className="w-4 h-4 mr-2" />
                      {link.name}
                    </a>
                 ))}
               </div>
            )}
          </div>
        )}
        
        {/* Actions & Stats */}
        <div className="mt-auto pt-4 flex flex-wrap items-center justify-between gap-6">
          <div className="flex flex-wrap items-center gap-3">
             <button
                onClick={onToggleSelectionMode}
                className={`flex items-center px-4 py-2 text-sm font-semibold rounded-xl transition-all shadow-lg focus:outline-none backdrop-blur-md ${isSelectionMode ? 'bg-komgaPrimary text-white shadow-komgaPrimary/20 border border-komgaPrimary/50' : 'bg-komgaSurface text-gray-100 hover:bg-white/10 border border-white/10 hover:border-white/20'}`}
             >
                {isSelectionMode ? t('series.header.cancelBatch') : t('series.header.batchActions')}
             </button>
             
             {!selectedVolume && (
               <div className="flex items-center border border-white/10 rounded-xl shadow-sm bg-komgaSurface/80 backdrop-blur-md">
                 <button onClick={onEdit} className="p-2 text-gray-200 hover:text-white hover:bg-white/10 transition-colors rounded-l-xl" title={t('series.header.editMetadata')}>
                    <Edit className="w-4 h-4 m-0.5" />
                 </button>
                 <div className="w-px h-5 bg-white/10 mx-1" />
                 <button onClick={onAddToCollection} className="p-2 text-gray-200 hover:text-white hover:bg-white/10 transition-colors" title={t('series.header.addToCollection')}>
                    <FolderHeart className="w-4 h-4 m-0.5" />
                 </button>
                 <div className="w-px h-5 bg-white/10 mx-1" />
                 <button onClick={onOpenDirectory} disabled={isOpeningDirectory} className="p-2 text-gray-200 hover:text-komgaPrimary hover:bg-komgaPrimary/10 transition-colors disabled:opacity-50" title={t('series.header.openDirectory')}>
                    <FolderOpen className={`w-4 h-4 m-0.5 ${isOpeningDirectory ? 'animate-pulse text-komgaPrimary' : ''}`} />
                 </button>
                 <div className="w-px h-5 bg-white/10 mx-1" />
                 <button onClick={onRescan} disabled={isRescanning} className="p-2 text-gray-200 hover:text-komgaSecondary hover:bg-komgaSecondary/10 transition-colors disabled:opacity-50" title={t('series.header.rescan')}>
                    <RefreshCw className={`w-4 h-4 m-0.5 ${isRescanning ? 'animate-spin text-komgaSecondary' : ''}`} />
                 </button>
                 <div className="w-px h-5 bg-white/10 mx-1" />
                 <div className="relative flex">
                    <button onClick={onToggleScrapeMenu} disabled={isScraping} className="p-2 text-gray-200 hover:text-komgaPrimary hover:bg-komgaPrimary/10 transition-colors disabled:opacity-50 rounded-r-xl" title={t('series.header.scrape')}>
                      {isScraping ? <div className="w-4 h-4 m-0.5 animate-spin rounded-full border-2 border-komgaPrimary border-t-transparent" /> : <Download className="w-4 h-4 m-0.5" />}
                    </button>
                    {scrapeMenuOpen && !isScraping && (
                       <>
                         <div className="fixed inset-0 z-40" onClick={onCloseScrapeMenu} />
                         <div className="absolute right-0 bottom-full mb-2 w-48 bg-komgaSurface border border-white/10 rounded-xl shadow-2xl z-50 overflow-hidden animate-in fade-in zoom-in duration-200">
                           <div className="px-3 py-2 text-xs font-semibold text-gray-400 border-b border-white/5 bg-komgaSurface/50">{t('series.header.pickSource')}</div>
                           <button onClick={() => onScrape('bangumi')} className="w-full text-left px-4 py-3 text-sm font-medium text-gray-100 hover:bg-komgaPrimary hover:text-white transition-colors">{t('series.header.bangumiRecommended')}</button>
                           <button onClick={() => onScrape('ollama')} className="w-full text-left px-4 py-3 text-sm font-medium text-gray-100 hover:bg-komgaPrimary hover:text-white transition-colors border-t border-white/5">{t('series.header.ollama')}</button>
                         </div>
                       </>
                    )}
                 </div>
               </div>
             )}
          </div>
          
          <div className="text-gray-200 text-sm font-medium flex items-center gap-2 bg-komgaSurface/80 px-4 py-2 rounded-lg border border-white/5 backdrop-blur-sm self-stretch md:self-auto shadow-sm">
            <div className="w-2 h-2 rounded-full bg-komgaPrimary/80 animate-pulse hidden sm:block"></div>
            {selectedVolume
              ? t('series.header.volumeSummary', { books: activeVolumeBooks.length, pages: activeVolumeBooks.reduce((acc, book) => acc + book.page_count, 0) })
              : t('series.header.seriesSummary', { count: books.length, volumes: volumes.length, standalone: standaloneBooks.length })}
          </div>
        </div>

      </div>
    </div>
  );
}
