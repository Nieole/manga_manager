import { useState, useMemo } from 'react';
import { Search, ChevronDown, ChevronUp, Filter } from 'lucide-react';
import type { NamedOption } from './types';
import { useI18n } from '../../i18n/LocaleProvider';
import { normalizeSeriesStatus } from '../../i18n/status';

interface HomeFiltersProps {
  allStatuses: string[];
  allTags: NamedOption[];
  allAuthors: NamedOption[];
  activeStatus: string | null;
  activeTag: string | null;
  activeAuthor: string | null;
  activeLetter: string | null;
  onStatusChange: (value: string | null) => void;
  onTagChange: (value: string | null) => void;
  onAuthorChange: (value: string | null) => void;
  onLetterChange: (value: string | null) => void;
}

const COLLAPSED_VISIBLE_COUNT = 15;
const EXPANDED_VISIBLE_COUNT = 150;

export function HomeFilters({
  allStatuses,
  allTags,
  allAuthors,
  activeStatus,
  activeTag,
  activeAuthor,
  activeLetter,
  onStatusChange,
  onTagChange,
  onAuthorChange,
  onLetterChange,
}: HomeFiltersProps) {
  const { t } = useI18n();
  
  const [tagSearch, setTagSearch] = useState('');
  const [authorSearch, setAuthorSearch] = useState('');
  const [tagsExpanded, setTagsExpanded] = useState(false);
  const [authorsExpanded, setAuthorsExpanded] = useState(false);

  const processedTags = useMemo(() => allTags.map(t => ({ name: t.name, lower: t.name.toLowerCase() })), [allTags]);
  const processedAuthors = useMemo(() => allAuthors.map(a => ({ name: a.name, lower: a.name.toLowerCase() })), [allAuthors]);

  const filteredTags = useMemo(() => {
    if (!tagSearch) return processedTags.map(t => t.name);
    const lower = tagSearch.toLowerCase().trim();
    return processedTags.filter(t => t.lower.includes(lower)).map(t => t.name);
  }, [processedTags, tagSearch]);

  const filteredAuthors = useMemo(() => {
    if (!authorSearch) return processedAuthors.map(a => a.name);
    const lower = authorSearch.toLowerCase().trim();
    return processedAuthors.filter(a => a.lower.includes(lower)).map(a => a.name);
  }, [processedAuthors, authorSearch]);

  const renderFilterRow = (
    label: string,
    translateItem: ((item: string) => string) | null,
    filteredList: string[],
    activeValue: string | null, 
    onChange: (val: string | null) => void,
    expanded: boolean,
    onToggleExpand: () => void,
    searchValue: string,
    setSearchValue: (val: string) => void,
    expandable: boolean
  ) => {
    
    let displayList = filteredList;
    let hasMore = false;
    let omitCount = 0;

    if (expandable) {
      const activeIncluded = activeValue && filteredList.includes(activeValue) ? [activeValue] : [];
      const others = filteredList.filter(v => v !== activeValue);
      const combined = Array.from(new Set([...activeIncluded, ...others]));
      
      if (!expanded) {
        displayList = combined.slice(0, COLLAPSED_VISIBLE_COUNT);
      } else {
        if (combined.length > EXPANDED_VISIBLE_COUNT) {
          displayList = combined.slice(0, EXPANDED_VISIBLE_COUNT);
          hasMore = true;
          omitCount = combined.length - EXPANDED_VISIBLE_COUNT;
        } else {
          displayList = combined;
        }
      }
    }

    return (
      <div className="flex flex-col lg:flex-row gap-3 py-5 border-t border-gray-800/30 first:border-0 last:border-b-0">
        <div className="flex items-center justify-between lg:w-32 shrink-0 h-9">
          <span className="text-gray-400 font-medium text-sm">{label}</span>
        </div>
        
        <div className="flex-1 min-w-0 flex flex-col gap-3">
            {expandable && expanded && (
                <div className="relative max-w-sm mb-1">
                  <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                    <Search className="h-4 w-4 text-gray-400" />
                  </div>
                  <input
                    type="text"
                    className="select-text bg-white/5 border border-white/10 text-gray-100 text-sm rounded-lg focus:ring-komgaPrimary focus:border-komgaPrimary block w-full pl-9 p-2 transition-colors placeholder:text-gray-500 outline-none backdrop-blur-sm"
                    placeholder={t('home.filters.searchInList', { label })}
                    value={searchValue}
                    onChange={(e) => setSearchValue(e.target.value)}
                    onMouseDown={(e) => e.stopPropagation()}
                  />
                </div>
            )}
            
            <div className="flex flex-wrap items-center gap-2 relative transition-all">
              <button
                onClick={() => onChange(null)}
                className={`px-3 py-1.5 text-xs font-medium rounded-lg transition-all border ${activeValue === null ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-md' : 'bg-komgaSurface border-white/5 text-gray-400 hover:border-white/20 hover:text-white'}`}
              >
                {t('home.filters.all')}
              </button>

              {displayList.map((item) => (
                <button
                  key={item}
                  onClick={() => onChange(activeValue === item ? null : item)}
                  className={`px-3 py-1.5 text-xs font-medium rounded-lg transition-all border max-w-[200px] truncate ${activeValue === item ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-md' : 'bg-komgaSurface border-white/5 text-gray-200 hover:border-white/20 hover:text-white'}`}
                  title={item}
                >
                  {translateItem ? translateItem(item) : item}
                </button>
              ))}

              {filteredList.length === 0 && expanded && (
                 <span className="text-sm text-gray-500 ml-2">{t('home.filters.noMatch')}</span>
              )}

              {hasMore && expanded && (
                <span className="text-xs text-gray-500 ml-2 italic py-1.5 flex items-center">
                  {t('home.filters.moreHidden', { count: omitCount })}
                </span>
              )}

              {expandable && (
                <button
                  onClick={onToggleExpand}
                  className="flex items-center justify-center px-3 py-1 text-xs font-medium text-komgaPrimary hover:text-komgaPrimaryHover bg-transparent hover:bg-komgaPrimary/10 rounded-lg transition-colors ml-1 h-8"
                >
                  {expanded
                    ? <><ChevronUp className="w-3.5 h-3.5 mr-1" /> {t('home.filters.collapse')}</>
                    : <><ChevronDown className="w-3.5 h-3.5 mr-1" /> {t('home.filters.expandMore', { count: filteredList.length - displayList.length })}</>}
                </button>
              )}
            </div>
        </div>
      </div>
    );
  };

  const [isFiltersExpanded, setIsFiltersExpanded] = useState(false);

  return (
    <div className="mb-6 rounded-3xl bg-gradient-to-br from-gray-900/60 to-komgaSurface/80 border border-white/5 shadow-sm backdrop-blur-xl overflow-hidden transition-all">
      <div 
        className="px-5 sm:px-8 py-4 flex items-center justify-between cursor-pointer group"
        onClick={() => setIsFiltersExpanded(!isFiltersExpanded)}
      >
         <div className="flex items-center gap-2 text-gray-200 group-hover:text-white transition-colors">
            <Filter className="w-5 h-5 text-komgaPrimary" />
            <h3 className="text-base font-semibold tracking-wide">{t('home.filters.title')}</h3>
            {(activeStatus || activeTag || activeAuthor || activeLetter) && (
              <span className="ml-2 px-2 py-0.5 rounded-full bg-komgaPrimary/20 text-komgaPrimary text-[11px] font-bold border border-komgaPrimary/30 flex items-center shadow-lg shadow-komgaPrimary/10">
                <div className="w-1.5 h-1.5 rounded-full bg-komgaPrimary mr-1 animate-pulse"></div>
                {t('home.filters.active')}
              </span>
            )}
         </div>
         <div className="text-gray-500 group-hover:text-white transition-colors bg-white/5 rounded-full p-1 border border-white/5 group-hover:border-white/10">
            {isFiltersExpanded ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
         </div>
      </div>

      <div className={`transition-all duration-300 ease-in-out origin-top ${isFiltersExpanded ? 'opacity-100 max-h-[2000px] border-t border-white/5' : 'opacity-0 max-h-0'}`}>
        <div className="px-5 sm:px-8 py-2">
          {renderFilterRow(
            t('home.filters.status'),
            (item) => t(`status.${normalizeSeriesStatus(item)}`),
            allStatuses, 
            activeStatus, 
            onStatusChange, 
            false, 
            () => {}, 
            '', 
            () => {}, 
            false
          )}

          {allTags.length > 0 && renderFilterRow(
            t('home.filters.tags'),
            null,
            filteredTags, 
            activeTag, 
            onTagChange, 
            tagsExpanded, 
            () => {
                setTagsExpanded(!tagsExpanded);
                if (tagsExpanded) setTagSearch('');
            }, 
            tagSearch, 
            setTagSearch, 
            allTags.length > COLLAPSED_VISIBLE_COUNT
          )}

          {allAuthors.length > 0 && renderFilterRow(
            t('home.filters.authors'),
            null,
            filteredAuthors, 
            activeAuthor, 
            onAuthorChange, 
            authorsExpanded, 
            () => {
                setAuthorsExpanded(!authorsExpanded);
                if (authorsExpanded) setAuthorSearch('');
            }, 
            authorSearch, 
            setAuthorSearch, 
            allAuthors.length > COLLAPSED_VISIBLE_COUNT
          )}
          
          <div className="flex flex-col lg:flex-row gap-3 py-5 border-t border-gray-800/30">
            <div className="flex items-center lg:w-32 shrink-0 h-9">
              <span className="text-gray-400 font-medium text-sm">{t('home.filters.letter')}</span>
            </div>
            <div className="flex-1 flex flex-wrap gap-1.5 items-center">
                <button
                  onClick={() => onLetterChange(null)}
                  className={`px-3 py-1.5 text-xs font-semibold rounded-lg transition-all border ${activeLetter === null ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-sm' : 'bg-transparent border-transparent text-gray-400 hover:bg-white/10 hover:text-white'}`}
                >
                  {t('home.filters.all')}
                </button>
                {'#ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('').map((letter) => (
                  <button
                    key={letter}
                    onClick={() => onLetterChange(activeLetter === letter ? null : letter)}
                    className={`w-8 h-8 flex items-center justify-center text-xs font-semibold rounded-lg transition-all border ${activeLetter === letter ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-sm' : 'bg-transparent border-transparent text-gray-400 hover:bg-white/10 hover:text-white'}`}
                  >
                    {letter}
                  </button>
                ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
