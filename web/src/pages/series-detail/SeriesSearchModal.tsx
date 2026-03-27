import { ArrowLeft, BookImage, Building2, Download, Edit, FolderOpen, Globe, Info, Star, X } from 'lucide-react';
import type { SearchResult } from './types';

interface SeriesSearchModalProps {
  open: boolean;
  modalSearchQuery: string;
  isScraping: boolean;
  searchResults: SearchResult[];
  currentOffset: number;
  searchTotal: number;
  onClose: () => void;
  onSearchQueryChange: (value: string) => void;
  onReSearch: (offset?: number) => void;
  onApplyMetadata: (metadata: SearchResult) => void;
}

export function SeriesSearchModal({
  open,
  modalSearchQuery,
  isScraping,
  searchResults,
  currentOffset,
  searchTotal,
  onClose,
  onSearchQueryChange,
  onReSearch,
  onApplyMetadata,
}: SeriesSearchModalProps) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center p-4 bg-black/90 backdrop-blur-md animate-in fade-in duration-300">
      <div className="bg-komgaSurface border border-gray-800 rounded-3xl w-full max-w-5xl overflow-hidden shadow-2xl flex flex-col max-h-[85vh] scale-in-center animate-in zoom-in-95 duration-300">
        <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between p-6 border-b border-gray-800 bg-gray-900/50 gap-4">
          <div className="flex-1 w-full">
            <h3 className="text-xl font-bold text-white flex items-center gap-3">
              <Globe className="w-5 h-5 text-komgaPrimary" />
              选择最佳匹配条目
            </h3>
            <div className="mt-4 flex gap-2 w-full max-w-xl">
              <div className="relative flex-1">
                <input
                  type="text"
                  value={modalSearchQuery}
                  onChange={(e) => onSearchQueryChange(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && onReSearch(0)}
                  placeholder="输入搜索关键词..."
                  className="w-full bg-gray-900 border border-gray-700 rounded-xl px-4 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all pl-10"
                />
                <Edit className="w-4 h-4 text-gray-500 absolute left-3 top-1/2 -translate-y-1/2" />
              </div>
              <button
                onClick={() => onReSearch(0)}
                disabled={isScraping}
                className="bg-komgaPrimary hover:bg-komgaPrimary/80 disabled:opacity-50 text-white px-4 py-2 rounded-xl text-sm font-medium transition-all shadow-lg shadow-komgaPrimary/20 flex items-center gap-2 shrink-0"
              >
                {isScraping ? (
                  <div className="w-4 h-4 animate-spin rounded-full border-2 border-white/30 border-t-white" />
                ) : (
                  <Download className="w-4 h-4" />
                )}
                重新搜索
              </button>
            </div>
          </div>
          <button onClick={onClose} className="p-2 text-gray-400 hover:text-white hover:bg-gray-800 rounded-full transition-all shrink-0 self-start sm:self-center">
            <X className="w-7 h-7" />
          </button>
        </div>

        <div className="p-6 overflow-y-auto space-y-4 flex-1 custom-scrollbar">
          {searchResults.length > 0 ? (
            searchResults.map((result, idx) => (
              <div
                key={idx}
                onClick={() => onApplyMetadata(result)}
                className="group flex gap-6 p-6 rounded-2xl bg-gray-900/40 border border-gray-800 hover:border-komgaPrimary/50 hover:bg-komgaPrimary/5 transition-all cursor-pointer relative overflow-hidden active:scale-[0.99] min-h-[180px]"
              >
                <div className="w-28 sm:w-36 shrink-0 aspect-[3/4] bg-gray-800 rounded-xl overflow-hidden border border-gray-700 shadow-xl self-start">
                  {result.CoverURL ? (
                    <img src={result.CoverURL} alt={result.Title} className="w-full h-full object-cover group-hover:scale-110 transition-transform duration-700" />
                  ) : (
                    <div className="w-full h-full flex items-center justify-center">
                      <BookImage className="w-12 h-12 text-gray-700" />
                    </div>
                  )}
                </div>
                <div className="flex-1 min-w-0 flex flex-col justify-start">
                  <div className="flex justify-between items-start gap-4">
                    <div className="min-w-0 flex-1">
                      <h4 className="text-xl font-bold text-white group-hover:text-komgaPrimary transition-colors leading-tight">
                        {result.Title}
                      </h4>
                      {result.OriginalTitle && result.OriginalTitle !== result.Title && (
                        <p className="text-sm text-gray-500 truncate mt-1 italic">{result.OriginalTitle}</p>
                      )}
                    </div>
                    {result.Rating > 0 && (
                      <div className="flex items-center text-yellow-500 text-sm font-bold shrink-0 bg-yellow-400/10 px-2 py-1 rounded-lg border border-yellow-500/20 shadow-sm">
                        <Star className="w-4 h-4 mr-1 fill-current" />
                        {result.Rating.toFixed(1)}
                      </div>
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
                        {result.VolumeCount} 卷/册
                      </p>
                    )}
                  </div>
                  <div className="mt-4 flex flex-wrap gap-2">
                    {result.Tags?.slice(0, 8).map((tag) => (
                      <span key={tag} className="text-[11px] bg-gray-800/60 text-gray-400 px-2.5 py-1 rounded-full border border-gray-700/50 hover:border-gray-600 transition-colors">
                        {tag}
                      </span>
                    ))}
                  </div>
                  <p className="text-gray-400 text-sm mt-4 line-clamp-3 leading-relaxed italic border-l-2 border-komgaPrimary/30 pl-4 py-1">
                    {result.Summary || '暂无简介...'}
                  </p>
                  <div className="mt-6 flex items-center justify-between">
                    <span className="text-xs text-gray-600 font-mono tracking-wider">SOURCE ID: {result.SourceID}</span>
                    <span className="text-sm font-bold text-komgaPrimary opacity-0 group-hover:opacity-100 translate-x-4 group-hover:translate-x-0 transition-all duration-300 flex items-center gap-2 bg-komgaPrimary/10 px-4 py-1 rounded-full border border-komgaPrimary/20">
                      应用当前条目 <ArrowLeft className="w-4 h-4 rotate-180" />
                    </span>
                  </div>
                </div>
                <div className="absolute top-0 right-0 w-24 h-24 bg-komgaPrimary/5 -translate-y-12 translate-x-12 rotate-45 group-hover:translate-x-8 group-hover:-translate-y-8 transition-transform duration-700"></div>
              </div>
            ))
          ) : (
            <div className="flex flex-col items-center justify-center py-20 text-gray-500 gap-4">
              <Globe className="w-16 h-16 opacity-20" />
              <p>未找到匹配条目，请尝试修改关键词重新搜索</p>
            </div>
          )}
        </div>

        <div className="p-6 border-t border-gray-800 bg-gray-900/50 flex flex-col sm:flex-row items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <button
              onClick={() => onReSearch(Math.max(0, currentOffset - 20))}
              disabled={isScraping || currentOffset === 0}
              className="px-4 py-2 bg-gray-800 hover:bg-gray-700 disabled:opacity-30 rounded-xl text-sm text-gray-300 transition-colors"
            >
              上一页
            </button>
            <span className="text-gray-500 text-sm">
              第 {Math.floor(currentOffset / 20) + 1} / {Math.max(1, Math.ceil(searchTotal / 20))} 页
            </span>
            <button
              onClick={() => onReSearch(currentOffset + 20)}
              disabled={isScraping || currentOffset + 20 >= searchTotal}
              className="px-4 py-2 bg-gray-800 hover:bg-gray-700 disabled:opacity-30 rounded-xl text-sm text-gray-300 transition-colors"
            >
              下一页
            </button>
          </div>
          <p className="text-gray-500 text-xs flex items-center gap-2 italic">
            <Info className="w-4 h-4" />
            请点击匹配最准确的条目以更新当前系列的元数据
          </p>
        </div>
      </div>
    </div>
  );
}
