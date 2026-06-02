import { useEffect, useMemo, useState } from 'react';
import { ChevronDown, ChevronUp, Filter, FilterX, Search, X } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { normalizeSeriesStatus } from '../../i18n/status';
import type { NamedOption } from './types';

interface LibraryFilterBarProps {
  allStatuses: string[];
  allTags: NamedOption[];
  allAuthors: NamedOption[];
  activeStatus: string | null;
  activeTag: string | null;
  activeAuthor: string | null;
  activeLetter: string | null;
  filterOptionsLoading?: boolean;
  smartFilterChips: string[];
  hasAnyFilter: boolean;
  onStatusChange: (value: string | null) => void;
  onTagChange: (value: string | null) => void;
  onAuthorChange: (value: string | null) => void;
  onLetterChange: (value: string | null) => void;
  onResetFilters: () => void;
  onFiltersOpen?: () => void;
  onTagSearch?: (query: string) => void;
  onAuthorSearch?: (query: string) => void;
}

const COLLAPSED_VISIBLE_COUNT = 15;
const EXPANDED_VISIBLE_COUNT = 150;

export function LibraryFilterBar({
  allStatuses,
  allTags,
  allAuthors,
  activeStatus,
  activeTag,
  activeAuthor,
  activeLetter,
  filterOptionsLoading = false,
  smartFilterChips,
  hasAnyFilter,
  onStatusChange,
  onTagChange,
  onAuthorChange,
  onLetterChange,
  onResetFilters,
  onFiltersOpen,
  onTagSearch,
  onAuthorSearch,
}: LibraryFilterBarProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [tagSearch, setTagSearch] = useState('');
  const [authorSearch, setAuthorSearch] = useState('');
  const [tagsExpanded, setTagsExpanded] = useState(false);
  const [authorsExpanded, setAuthorsExpanded] = useState(false);

  useEffect(() => {
    if (open && onFiltersOpen) onFiltersOpen();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    if (!tagsExpanded || !onTagSearch) return;
    const timer = window.setTimeout(() => onTagSearch(tagSearch.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [onTagSearch, tagSearch, tagsExpanded]);

  useEffect(() => {
    if (!authorsExpanded || !onAuthorSearch) return;
    const timer = window.setTimeout(() => onAuthorSearch(authorSearch.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [authorSearch, authorsExpanded, onAuthorSearch]);

  const processedTags = useMemo(() => allTags.map((tag) => ({ name: tag.name, lower: tag.name.toLowerCase() })), [allTags]);
  const processedAuthors = useMemo(
    () => allAuthors.map((a) => ({ name: a.name, lower: a.name.toLowerCase() })),
    [allAuthors],
  );
  const filteredTags = useMemo(() => {
    if (!tagSearch) return processedTags.map((tag) => tag.name);
    const lower = tagSearch.toLowerCase().trim();
    return processedTags.filter((tag) => tag.lower.includes(lower)).map((tag) => tag.name);
  }, [processedTags, tagSearch]);
  const filteredAuthors = useMemo(() => {
    if (!authorSearch) return processedAuthors.map((a) => a.name);
    const lower = authorSearch.toLowerCase().trim();
    return processedAuthors.filter((a) => a.lower.includes(lower)).map((a) => a.name);
  }, [processedAuthors, authorSearch]);

  const activeChips = useMemo(() => {
    const chips: { key: string; label: string; onClear: () => void }[] = [];
    if (activeStatus) {
      chips.push({
        key: `status-${activeStatus}`,
        label: t(`status.${normalizeSeriesStatus(activeStatus)}`),
        onClear: () => onStatusChange(null),
      });
    }
    if (activeTag) chips.push({ key: `tag-${activeTag}`, label: t('home.smartFilters.chipTag', { value: activeTag }), onClear: () => onTagChange(null) });
    if (activeAuthor)
      chips.push({
        key: `author-${activeAuthor}`,
        label: t('home.smartFilters.chipAuthor', { value: activeAuthor }),
        onClear: () => onAuthorChange(null),
      });
    if (activeLetter)
      chips.push({
        key: `letter-${activeLetter}`,
        label: t('home.smartFilters.chipLetter', { value: activeLetter }),
        onClear: () => onLetterChange(null),
      });
    return chips;
  }, [activeStatus, activeTag, activeAuthor, activeLetter, onAuthorChange, onLetterChange, onStatusChange, onTagChange, t]);

  const renderRow = (
    label: string,
    items: string[],
    activeValue: string | null,
    onChange: (value: string | null) => void,
    expanded: boolean,
    onToggleExpand: () => void,
    searchValue: string,
    onSearchChange: (value: string) => void,
    showSearchAndExpand: boolean,
    asyncSearchEnabled: boolean,
    formatLabel: (item: string) => string = (item) => item,
  ) => {
    const visibleCount = expanded ? EXPANDED_VISIBLE_COUNT : COLLAPSED_VISIBLE_COUNT;
    const visibleItems = items.slice(0, visibleCount);
    const truncated = items.length > visibleCount;
    return (
      <div className="flex flex-col lg:flex-row gap-3 py-5 border-t border-gray-800/30">
        <div className="flex items-center lg:w-32 shrink-0 h-9">
          <span className="text-gray-400 font-medium text-sm">{label}</span>
          {asyncSearchEnabled && filterOptionsLoading && expanded && (
            <span className="ml-2 text-[10px] text-gray-500">{t('common.loading')}</span>
          )}
        </div>
        <div className="flex-1">
          {showSearchAndExpand && expanded && (
            <div className="mb-3 relative max-w-sm">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-500" />
              <input
                value={searchValue}
                onChange={(e) => onSearchChange(e.target.value)}
                placeholder={t('home.filters.searchInList', { label })}
                className="w-full rounded-md border border-gray-700 bg-gray-900 px-7 py-1.5 text-xs text-white outline-hidden placeholder:text-gray-600 transition-colors focus:border-komgaPrimary"
              />
            </div>
          )}
          <div className="flex flex-wrap gap-1.5 items-center">
            <button
              onClick={() => onChange(null)}
              className={`px-3 py-1.5 text-xs font-semibold rounded-lg transition-all border ${
                activeValue === null
                  ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-xs'
                  : 'bg-transparent border-transparent text-gray-400 hover:bg-white/10 hover:text-white'
              }`}
            >
              {t('home.filters.all')}
            </button>
            {visibleItems.map((item) => (
              <button
                key={item}
                onClick={() => onChange(activeValue === item ? null : item)}
                className={`px-3 py-1.5 text-xs font-semibold rounded-lg transition-all border ${
                  activeValue === item
                    ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-xs'
                    : 'bg-transparent border-transparent text-gray-400 hover:bg-white/10 hover:text-white'
                }`}
              >
                {formatLabel(item)}
              </button>
            ))}
            {truncated && (
              <span className="text-[11px] text-gray-500 px-2">
                {t('home.filters.moreHidden', { count: items.length - visibleCount })}
              </span>
            )}
            {showSearchAndExpand && (
              <button
                onClick={onToggleExpand}
                className="px-2 py-1.5 text-xs text-gray-400 hover:text-white inline-flex items-center gap-1"
              >
                {expanded ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                {expanded ? t('home.filters.collapse') : t('home.filters.expandMore', { count: items.length - COLLAPSED_VISIBLE_COUNT })}
              </button>
            )}
          </div>
        </div>
      </div>
    );
  };

  return (
    <div className="mb-6 rounded-2xl border border-white/10 bg-komgaSurface/70 p-4 shadow-xs backdrop-blur-md">
      <div className="flex flex-wrap items-center gap-2">
        <button
          onClick={() => setOpen((p) => !p)}
          className="inline-flex items-center gap-2 rounded-lg border border-white/10 bg-gray-950/60 px-3 py-1.5 text-sm font-medium text-gray-200 hover:border-komgaPrimary hover:text-komgaPrimary transition-colors"
        >
          <Filter className="h-4 w-4" />
          {open ? t('library.filters.close') : t('library.filters.open')}
        </button>
        {activeChips.map((chip) => (
          <span
            key={chip.key}
            className="inline-flex items-center gap-1 rounded-full border border-komgaPrimary/30 bg-komgaPrimary/10 px-3 py-1 text-xs text-komgaPrimary whitespace-nowrap shrink-0"
          >
            {chip.label}
            <button
              onClick={chip.onClear}
              className="ml-1 rounded-full p-0.5 hover:bg-komgaPrimary/20"
              aria-label={t('library.filters.removeChip')}
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
        {smartFilterChips.map((chip) => (
          <span
            key={`smart-${chip}`}
            className="rounded-full border border-white/10 bg-gray-950/70 px-2.5 py-1 text-[11px] text-gray-400 whitespace-nowrap shrink-0"
          >
            {chip}
          </span>
        ))}
        {hasAnyFilter && (
          <button
            onClick={onResetFilters}
            className="ml-auto inline-flex items-center gap-1 rounded-lg border border-white/10 bg-gray-950 px-3 py-1.5 text-xs font-medium text-gray-300 hover:border-red-400/40 hover:text-red-300 transition-colors"
          >
            <FilterX className="h-3.5 w-3.5" />
            {t('library.filters.resetAll')}
          </button>
        )}
      </div>

      {open && (
        <div className="mt-4 border-t border-gray-800/40 pt-2">
          {renderRow(
            t('home.filters.status'),
            allStatuses,
            activeStatus,
            onStatusChange,
            false,
            () => undefined,
            '',
            () => undefined,
            false,
            false,
            (value) => t(`status.${normalizeSeriesStatus(value)}`),
          )}
          {renderRow(
            t('home.filters.tags'),
            filteredTags,
            activeTag,
            onTagChange,
            tagsExpanded,
            () => {
              setTagsExpanded((prev) => !prev);
              if (tagsExpanded) setTagSearch('');
            },
            tagSearch,
            setTagSearch,
            allTags.length > COLLAPSED_VISIBLE_COUNT,
            Boolean(onTagSearch),
          )}
          {renderRow(
            t('home.filters.authors'),
            filteredAuthors,
            activeAuthor,
            onAuthorChange,
            authorsExpanded,
            () => {
              setAuthorsExpanded((prev) => !prev);
              if (authorsExpanded) setAuthorSearch('');
            },
            authorSearch,
            setAuthorSearch,
            allAuthors.length > COLLAPSED_VISIBLE_COUNT,
            Boolean(onAuthorSearch),
          )}
          <div className="flex flex-col lg:flex-row gap-3 py-5 border-t border-gray-800/30">
            <div className="flex items-center lg:w-32 shrink-0 h-9">
              <span className="text-gray-400 font-medium text-sm">{t('home.filters.letter')}</span>
            </div>
            <div className="flex-1 flex flex-wrap gap-1.5 items-center">
              <button
                onClick={() => onLetterChange(null)}
                className={`px-3 py-1.5 text-xs font-semibold rounded-lg transition-all border ${
                  activeLetter === null
                    ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-xs'
                    : 'bg-transparent border-transparent text-gray-400 hover:bg-white/10 hover:text-white'
                }`}
              >
                {t('home.filters.all')}
              </button>
              {'#ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('').map((letter) => (
                <button
                  key={letter}
                  onClick={() => onLetterChange(activeLetter === letter ? null : letter)}
                  className={`w-8 h-8 flex items-center justify-center text-xs font-semibold rounded-lg transition-all border ${
                    activeLetter === letter
                      ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-xs'
                      : 'bg-transparent border-transparent text-gray-400 hover:bg-white/10 hover:text-white'
                  }`}
                >
                  {letter}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}