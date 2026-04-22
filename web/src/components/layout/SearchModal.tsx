import { Image as ImageIcon, Search, X } from 'lucide-react';
import type { SearchHit } from './types';
import { ModalShell } from '../ui/ModalShell';
import { useI18n } from '../../i18n/LocaleProvider';

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
  const { t } = useI18n();
  if (!open) return null;

  return (
    <ModalShell
      open={open}
      onClose={onClose}
      title={t('searchModal.title')}
      description={t('searchModal.description')}
      icon={<Search className="h-5 w-5" />}
      size="standard"
      placement="top"
      bodyClassName="overflow-hidden p-0"
      panelClassName="max-h-[78vh]"
    >
      <div className="flex h-full flex-col">
        <div className="flex items-center border-b border-gray-800 px-4 shrink-0">
          <Search className="w-5 h-5 text-gray-400" />
          <input
            autoFocus
            type="text"
            placeholder={t('searchModal.placeholder')}
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

        <div className="flex items-center space-x-2 border-b border-gray-800 bg-gray-950/40 px-4 py-3 shrink-0">
          <span className="text-xs text-gray-500 mr-2">{t('searchModal.scope')}</span>
          <button
            onClick={() => onSearchTargetChange('all')}
            className={`px-3 py-1 text-xs font-medium rounded-full transition-colors ${searchTarget === 'all' ? 'bg-komgaPrimary text-white' : 'bg-gray-800 text-gray-400 hover:text-white'}`}
          >
            {t('searchModal.scope.all')}
          </button>
          <button
            onClick={() => onSearchTargetChange('series')}
            className={`px-3 py-1 text-xs font-medium rounded-full transition-colors ${searchTarget === 'series' ? 'bg-komgaPrimary text-white' : 'bg-gray-800 text-gray-400 hover:text-white'}`}
          >
            {t('searchModal.scope.series')}
          </button>
          <button
            onClick={() => onSearchTargetChange('book')}
            className={`px-3 py-1 text-xs font-medium rounded-full transition-colors ${searchTarget === 'book' ? 'bg-komgaPrimary text-white' : 'bg-gray-800 text-gray-400 hover:text-white'}`}
          >
            {t('searchModal.scope.book')}
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
                          {t('searchModal.type.series')}
                        </span>
                      ) : (
                        <span className="px-1.5 py-0.5 rounded bg-emerald-500/20 text-emerald-400 text-[10px] font-bold tracking-wider shrink-0 border border-emerald-500/30 uppercase">
                          {t('searchModal.type.book')}
                        </span>
                      )}
                      <div className="text-base font-bold text-gray-100 truncate group-hover:text-komgaPrimary transition-colors">
                        {hit.fields?.title || hit.id}
                      </div>
                    </div>
                    <div className="text-xs text-gray-500 truncate flex items-center gap-2">
                      {isSeries ? (
                        <span>{t('searchModal.seriesAction')}</span>
                      ) : (
                        <>
                          <span className="text-komgaPrimary font-medium truncate max-w-[150px]">{hit.fields?.series_name || t('searchModal.unknownSeries')}</span>
                          <span className="text-gray-700">•</span>
                          <span>{t('searchModal.bookAction')}</span>
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
            <div className="py-14 text-center text-gray-500 text-sm">{t('searchModal.empty')}</div>
          ) : (
            <div className="py-8 text-center text-gray-600 text-sm flex flex-col items-center">
              <Search className="w-8 h-8 mb-3 opacity-20" />
              {t('searchModal.hint')}
            </div>
          )}
        </div>
      </div>
    </ModalShell>
  );
}
