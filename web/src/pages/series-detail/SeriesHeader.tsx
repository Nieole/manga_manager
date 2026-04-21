import { ArrowLeft, Building2, Download, Edit, FolderHeart, FolderOpen, Globe, Info, RefreshCw, Star } from 'lucide-react';
import type { Author, Book, MetaTag, Series, SeriesLink } from './types';

interface SeriesHeaderProps {
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
  return (
    <div className="mb-6 flex justify-between items-end border-b border-gray-800 pb-4">
      <div>
        <button
          onClick={onBack}
          className="flex items-center text-gray-400 hover:text-white transition-colors text-sm font-medium mb-4"
        >
          <ArrowLeft className="w-4 h-4 mr-1" />
          {selectedVolume ? '返回系列总览' : '返回资源库'}
        </button>
        <h2 className="text-2xl sm:text-3xl font-bold text-white tracking-tight flex flex-col sm:flex-row sm:items-center gap-4">
          <div className="flex items-center break-all sm:break-normal">
            {selectedVolume ? (
              <>
                <FolderOpen className="w-8 h-8 mr-3 text-komgaPrimary" />
                {selectedVolume}
              </>
            ) : (
              seriesInfo?.title?.Valid ? seriesInfo.title.String : seriesInfo?.name || '系列总览'
            )}
            {seriesInfo && (
              <div className="flex flex-wrap items-center gap-2 mt-2 sm:mt-0 w-full sm:w-auto sm:ml-4">
                <button
                  onClick={onToggleSelectionMode}
                  className={`flex-1 sm:flex-none px-3 py-1.5 text-sm font-medium rounded-lg transition-colors border focus:outline-none ${isSelectionMode ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-md' : 'bg-transparent border-gray-700 text-gray-400 hover:border-gray-500 hover:text-white'}`}
                >
                  {isSelectionMode ? '取消选择' : '批量操作'}
                </button>
                {!selectedVolume && (
                  <>
                    <button
                      onClick={onEdit}
                      className="p-1.5 text-gray-500 hover:text-komgaPrimary bg-gray-900 border border-gray-800 hover:bg-komgaPrimary/10 rounded transition-colors"
                      title="编辑元数据"
                    >
                      <Edit className="w-5 h-5" />
                    </button>
                    <button
                      onClick={onAddToCollection}
                      className="p-1.5 text-gray-500 hover:text-komgaPrimary bg-gray-900 border border-gray-800 hover:bg-komgaPrimary/10 rounded transition-colors"
                      title="添加到合集"
                    >
                      <FolderHeart className="w-5 h-5" />
                    </button>
                    <button
                      onClick={onOpenDirectory}
                      disabled={isOpeningDirectory}
                      className="p-1.5 text-gray-500 hover:text-amber-400 bg-gray-900 border border-gray-800 hover:bg-amber-400/10 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                      title="在文件管理器中打开系列目录"
                    >
                      <FolderOpen className={`w-5 h-5 ${isOpeningDirectory ? 'animate-pulse text-amber-400' : ''}`} />
                    </button>
                    <button
                      onClick={onRescan}
                      disabled={isRescanning}
                      className="p-1.5 text-gray-500 hover:text-blue-400 bg-gray-900 border border-gray-800 hover:bg-blue-400/10 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                      title="重新扫描该系列"
                    >
                      <RefreshCw className={`w-5 h-5 ${isRescanning ? 'animate-spin text-blue-400' : ''}`} />
                    </button>
                    <div className="relative">
                      <button
                        onClick={onToggleScrapeMenu}
                        disabled={isScraping}
                        className="p-1.5 text-gray-500 hover:text-green-400 bg-gray-900 border border-gray-800 hover:bg-green-400/10 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                        title="刮削元数据"
                      >
                        {isScraping ? (
                          <div className="w-5 h-5 animate-spin rounded-full border-2 border-green-400 border-t-transparent" />
                        ) : (
                          <Download className="w-5 h-5" />
                        )}
                      </button>

                      {scrapeMenuOpen && !isScraping && (
                        <>
                          <div className="fixed inset-0 z-40" onClick={onCloseScrapeMenu} />
                          <div className="absolute right-0 mt-2 w-48 bg-gray-800 border border-gray-700 rounded-lg shadow-xl z-50 overflow-hidden animate-in fade-in zoom-in duration-200">
                            <div className="px-3 py-2 text-xs font-semibold text-gray-400 border-b border-gray-700 bg-gray-900">
                              选择刮削来源
                            </div>
                            <button
                              onClick={() => onScrape('bangumi')}
                              className="w-full text-left px-4 py-3 text-sm text-gray-200 hover:bg-komgaPrimary hover:text-white transition-colors"
                            >
                              Bangumi (推荐)
                            </button>
                            <button
                              onClick={() => onScrape('ollama')}
                              className="w-full text-left px-4 py-3 text-sm text-gray-200 hover:bg-komgaPrimary hover:text-white transition-colors border-t border-gray-700"
                            >
                              Ollama LLM
                            </button>
                          </div>
                        </>
                      )}
                    </div>
                  </>
                )}
              </div>
            )}
          </div>

          {!selectedVolume && seriesInfo && (
            <div className="flex flex-wrap items-center gap-2 text-sm font-normal mt-2 sm:mt-0">
              {seriesInfo.rating?.Valid && (
                <span className="flex items-center text-yellow-500 bg-yellow-500/10 px-2.5 py-1 rounded-md border border-yellow-500/20 shadow-sm">
                  <Star className="w-4 h-4 mr-1 fill-current" />
                  {seriesInfo.rating.Float64.toFixed(1)}
                </span>
              )}
              {seriesInfo.status?.Valid && (
                <span className="flex items-center text-green-400 bg-green-400/10 px-2.5 py-1 rounded-md border border-green-400/20 shadow-sm">
                  <Info className="w-4 h-4 mr-1" />
                  {seriesInfo.status.String}
                </span>
              )}
              {seriesInfo.language?.Valid && (
                <span className="flex items-center text-blue-400 bg-blue-400/10 px-2.5 py-1 rounded-md border border-blue-400/20 shadow-sm uppercase font-semibold tracking-wider">
                  <Globe className="w-4 h-4 mr-1" />
                  {seriesInfo.language.String}
                </span>
              )}
              {seriesInfo.publisher?.Valid && (
                <span className="flex items-center text-purple-400 bg-purple-400/10 px-2.5 py-1 rounded-md border border-purple-400/20 shadow-sm">
                  <Building2 className="w-4 h-4 mr-1" />
                  {seriesInfo.publisher.String}
                </span>
              )}
            </div>
          )}
        </h2>

        {!selectedVolume && seriesInfo?.summary?.Valid && (
          <p className="text-gray-400 mt-5 text-sm leading-relaxed max-w-4xl line-clamp-3 hover:line-clamp-none transition-all cursor-pointer bg-gray-900/50 p-4 rounded-xl border border-gray-800/50 relative group">
            <span className="absolute -left-2 top-4 w-1 h-1/2 bg-gray-700 rounded-full group-hover:bg-komgaPrimary transition-colors opacity-0 group-hover:opacity-100"></span>
            {seriesInfo.summary.String}
          </p>
        )}

        {!selectedVolume && lockedFields.size > 0 && (
          <div className="mt-4 rounded-xl border border-amber-500/20 bg-amber-500/10 p-4 max-w-4xl">
            <p className="text-sm font-medium text-amber-100">已锁定 {lockedFields.size} 个字段，后续刮削不会覆盖这些内容。</p>
            <div className="mt-3 flex flex-wrap gap-2">
              {Array.from(lockedFields).map((field) => (
                <span
                  key={field}
                  className="rounded-full border border-amber-500/20 bg-black/20 px-3 py-1 text-xs text-amber-200"
                >
                  {field}
                </span>
              ))}
            </div>
          </div>
        )}

        {!selectedVolume && links.length > 0 && (
          <div className="mt-5 flex flex-wrap gap-3">
            {links.map((link, index) => (
              <a
                key={index}
                href={link.url}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center text-xs font-semibold px-4 py-2 bg-gray-800 hover:bg-komgaPrimary hover:text-white text-gray-300 border border-gray-700/50 hover:border-komgaPrimary/50 rounded-lg transition-all shadow-sm group"
              >
                {link.name}
              </a>
            ))}
          </div>
        )}

        {!selectedVolume && (tags.length > 0 || authors.length > 0) && (
          <div className="mt-5 flex flex-col gap-3">
            {authors.length > 0 && (
              <div className="flex items-start gap-3">
                <Info className="w-4 h-4 text-gray-500 mt-1 shrink-0" />
                <div className="flex flex-wrap gap-2">
                  {authors.map((author) => (
                    <span
                      key={author.id}
                      className="text-xs text-gray-300 bg-gray-800/80 px-2 py-1 rounded-md border border-gray-700 shadow-sm hover:bg-gray-700 transition-colors"
                    >
                      {author.name}
                      <span className="text-gray-500 ml-1.5 inline-block scale-90">({author.role})</span>
                    </span>
                  ))}
                </div>
              </div>
            )}
            {tags.length > 0 && (
              <div className="flex items-start gap-3">
                <Info className="w-4 h-4 text-komgaPrimary/60 mt-1 shrink-0" />
                <div className="flex flex-wrap gap-2">
                  {tags.map((tag) => (
                    <span
                      key={tag.id}
                      className="text-xs text-komgaPrimary bg-komgaPrimary/10 px-2 py-1 rounded-md border border-komgaPrimary/20 shadow-sm hover:bg-komgaPrimary/20 transition-colors cursor-pointer"
                    >
                      {tag.name}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}

        <p className="text-gray-500 mt-6 text-sm font-medium flex items-center gap-2">
          <div className="w-1.5 h-1.5 rounded-full bg-komgaPrimary/50"></div>
          {selectedVolume
            ? `含 ${activeVolumeBooks.length} 话 · 总共 ${activeVolumeBooks.reduce((acc, book) => acc + book.page_count, 0)} 页`
            : `共 ${books.length} 项资源 (${volumes.length} 卷, ${standaloneBooks.length} 独立册)`}
        </p>
      </div>
    </div>
  );
}
