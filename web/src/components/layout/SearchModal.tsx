import { Image as ImageIcon, Search, X } from 'lucide-react';
import type { SearchHit } from './types';

interface SearchModalProps {
  open: boolean;
  searchQuery: string;
  searchResults: SearchHit[];
  selectedIndex: number;
  searchTarget: string;
  onClose: () => void;
  onSearchQueryChange: (value: string) => void;
  onSearchKeyDown: (e: React.KeyboardEvent) => void;
  onResetSearch: () => void;
  onSearchTargetChange: (value: string) => void;
  onSelectResult: (hit: SearchHit) => void;
  onHighlightIndex: (index: number) => void;
}

export function SearchModal({
  open,
  searchQuery,
  searchResults,
  selectedIndex,
  searchTarget,
  onClose,
  onSearchQueryChange,
  onSearchKeyDown,
  onResetSearch,
  onSearchTargetChange,
  onSelectResult,
  onHighlightIndex,
}: SearchModalProps) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh] px-4">
      <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-full max-w-2xl bg-komgaSurface border border-gray-800 rounded-xl shadow-2xl flex flex-col max-h-[70vh] animate-in fade-in zoom-in-95 duration-200">
        <div className="flex items-center px-4 border-b border-gray-800 shrink-0">
          <Search className="w-5 h-5 text-gray-400" />
          <input
            autoFocus
            type="text"
            placeholder="输入关键字搜索..."
            value={searchQuery}
            onChange={(e) => onSearchQueryChange(e.target.value)}
            onKeyDown={onSearchKeyDown}
            className="flex-1 bg-transparent border-none py-4 px-4 text-white focus:outline-none focus:ring-0 text-lg placeholder-gray-500"
          />
          {searchQuery && (
            <button onClick={onResetSearch} className="p-1 text-gray-500 hover:text-white rounded-md transition-colors">
              <X className="w-5 h-5" />
            </button>
          )}
        </div>

        <div className="flex items-center px-4 py-2 border-b border-gray-800 space-x-2 shrink-0 bg-gray-900/30">
          <span className="text-xs text-gray-500 mr-2">范围:</span>
          <button
            onClick={() => onSearchTargetChange('all')}
            className={`px-3 py-1 text-xs font-medium rounded-full transition-colors ${searchTarget === 'all' ? 'bg-komgaPrimary text-white' : 'bg-gray-800 text-gray-400 hover:text-white'}`}
          >
            全部
          </button>
          <button
            onClick={() => onSearchTargetChange('series')}
            className={`px-3 py-1 text-xs font-medium rounded-full transition-colors ${searchTarget === 'series' ? 'bg-komgaPrimary text-white' : 'bg-gray-800 text-gray-400 hover:text-white'}`}
          >
            仅系列
          </button>
          <button
            onClick={() => onSearchTargetChange('book')}
            className={`px-3 py-1 text-xs font-medium rounded-full transition-colors ${searchTarget === 'book' ? 'bg-komgaPrimary text-white' : 'bg-gray-800 text-gray-400 hover:text-white'}`}
          >
            仅册文件
          </button>
        </div>

        <div className="overflow-y-auto flex-1 p-2">
          {searchResults.length > 0 && searchQuery.trim() !== '' ? (
            searchResults.map((hit, index: number) => {
              const isSeries = hit.fields?.type === 'series' || hit.id.startsWith('s_');
              const coverPath = hit.fields?.cover_path;

              return (
                <div
                  key={hit.id}
                  onClick={() => onSelectResult(hit)}
                  onMouseEnter={() => onHighlightIndex(index)}
                  className={`flex items-center gap-4 px-4 py-3 cursor-pointer rounded-lg transition-all ${index === selectedIndex ? 'bg-komgaPrimary/10 border-l-4 border-komgaPrimary shadow-md' : 'hover:bg-gray-800/50 border-l-4 border-transparent'}`}
                >
                  <div className="w-12 h-18 sm:w-14 sm:h-20 bg-gray-900 rounded-md overflow-hidden flex-shrink-0 border border-gray-800 shadow-sm relative group-hover:border-komgaPrimary/30 transition-colors">
                    {coverPath ? (
                      <img
                        src={`/api/thumbnails/${coverPath}`}
                        alt="preview"
                        className="w-full h-full object-cover transition-transform group-hover:scale-110"
                        onError={(e) => {
                          (e.target as HTMLImageElement).src = '';
                          const nextSibling = (e.target as HTMLImageElement).nextElementSibling as HTMLElement | null;
                          if (nextSibling) nextSibling.style.display = 'flex';
                          (e.target as HTMLImageElement).style.display = 'none';
                        }}
                      />
                    ) : null}
                    <div className={`absolute inset-0 items-center justify-center bg-gray-800 flex ${coverPath ? 'hidden' : ''}`}>
                      <ImageIcon className="w-6 h-6 text-gray-700" />
                    </div>
                  </div>

                  <div className="flex-1 min-w-0 flex flex-col justify-center">
                    <div className="flex items-center space-x-2 mb-1">
                      {isSeries ? (
                        <span className="px-1.5 py-0.5 rounded bg-blue-500/20 text-blue-400 text-[10px] font-bold tracking-wider shrink-0 border border-blue-500/30 uppercase">
                          系列
                        </span>
                      ) : (
                        <span className="px-1.5 py-0.5 rounded bg-emerald-500/20 text-emerald-400 text-[10px] font-bold tracking-wider shrink-0 border border-emerald-500/30 uppercase">
                          单册
                        </span>
                      )}
                      <div className="text-base font-bold text-gray-100 truncate group-hover:text-komgaPrimary transition-colors">
                        {hit.fields?.title || hit.id}
                      </div>
                    </div>
                    <div className="text-xs text-gray-500 truncate flex items-center gap-2">
                      {isSeries ? (
                        <span>浏览整个系列内容</span>
                      ) : (
                        <>
                          <span className="text-komgaPrimary font-medium truncate max-w-[150px]">{hit.fields?.series_name || '未知系列'}</span>
                          <span className="text-gray-700">•</span>
                          <span>进入详情页阅读</span>
                        </>
                      )}
                    </div>
                  </div>
                  <div className="hidden sm:flex flex-col items-end shrink-0 ml-2">
                    <span className="text-[10px] text-gray-600 font-mono">SCORE</span>
                    <span className={`text-xs font-bold ${(hit.score ?? 0) > 0.5 ? 'text-komgaPrimary' : 'text-gray-500'}`}>
                      {hit.score?.toFixed(2) ?? '0.00'}
                    </span>
                  </div>
                </div>
              );
            })
          ) : searchQuery.trim() !== '' ? (
            <div className="py-14 text-center text-gray-500 text-sm">未找到符合条件的漫画</div>
          ) : (
            <div className="py-8 text-center text-gray-600 text-sm flex flex-col items-center">
              <Search className="w-8 h-8 mb-3 opacity-20" />
              支持全局模糊检索、键盘上下方向键导航
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
