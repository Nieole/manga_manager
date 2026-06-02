import { ArrowDown, ArrowUp, HardDrive, Search } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';

interface LibraryHeaderProps {
  totalSeries: number;
  hasSeries: boolean;
  isSelectionMode: boolean;
  allCurrentPageSelected: boolean;
  selectedCount: number;
  sortByField: string;
  sortDir: string;
  searchValue: string;
  searchInputRef: React.RefObject<HTMLInputElement>;
  externalSessionActive: boolean;
  onSearchChange: (value: string) => void;
  onToggleSelectionMode: () => void;
  onToggleSelectCurrentPage: () => void;
  onSortFieldChange: (value: string) => void;
  onToggleSortDir: () => void;
  onOpenExternal: () => void;
}

export function LibraryHeader({
  totalSeries,
  hasSeries,
  isSelectionMode,
  allCurrentPageSelected,
  selectedCount,
  sortByField,
  sortDir,
  searchValue,
  searchInputRef,
  externalSessionActive,
  onSearchChange,
  onToggleSelectionMode,
  onToggleSelectCurrentPage,
  onSortFieldChange,
  onToggleSortDir,
  onOpenExternal,
}: LibraryHeaderProps) {
  const { t } = useI18n();

  return (
    <div className="mb-6 flex flex-col gap-4 border-b border-gray-800/30 pb-4">
      <div className="flex flex-col sm:flex-row sm:justify-between sm:items-end gap-4">
        <div className="min-w-0">
          <h2 className="text-2xl sm:text-3xl font-bold text-white tracking-tight mb-1">{t('home.toolbar.title')}</h2>
          <p className="text-gray-400 text-xs sm:text-sm">{t('home.toolbar.resultCount', { count: totalSeries })}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2 sm:gap-3 w-full sm:w-auto justify-between sm:justify-end">
          <div className="relative w-full sm:w-64 order-1 sm:order-0">
            <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-500" />
            <input
              ref={searchInputRef}
              type="search"
              value={searchValue}
              onChange={(e) => onSearchChange(e.target.value)}
              placeholder={t('library.search.placeholder')}
              aria-label={t('library.search.placeholder')}
              className="w-full rounded-lg border border-gray-700 bg-gray-900 px-9 py-1.5 text-sm text-white outline-hidden placeholder:text-gray-500 transition-colors focus:border-komgaPrimary"
            />
            <kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 hidden rounded-sm border border-gray-700 bg-gray-800 px-1.5 py-0.5 text-[10px] font-medium text-gray-400 sm:inline">
              /
            </kbd>
          </div>
          <button
            onClick={onOpenExternal}
            className={`inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs sm:text-sm font-medium transition-colors ${
              externalSessionActive
                ? 'border-komgaPrimary bg-komgaPrimary/10 text-komgaPrimary'
                : 'border-white/10 bg-transparent text-gray-400 hover:border-white/20 hover:text-white'
            }`}
            title={t('home.external.title')}
          >
            <HardDrive className="h-4 w-4" />
            <span className="hidden sm:inline">{t('home.external.title')}</span>
          </button>
          {hasSeries && (
            <button
              onClick={onToggleSelectionMode}
              className={`px-3 py-1.5 text-xs sm:text-sm font-medium rounded-lg transition-colors border focus:outline-hidden shrink-0 ${
                isSelectionMode
                  ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-md'
                  : 'bg-transparent border-white/10 text-gray-400 hover:border-white/20 hover:text-white'
              }`}
            >
              {isSelectionMode ? t('home.toolbar.cancelSelection') : t('home.toolbar.batchActions')}
            </button>
          )}
          {isSelectionMode && hasSeries && (
            <button
              onClick={onToggleSelectCurrentPage}
              className="px-3 py-1.5 text-xs sm:text-sm font-medium rounded-lg transition-colors border border-white/10 text-gray-300 hover:border-white/20 hover:text-white bg-transparent"
            >
              {allCurrentPageSelected ? t('home.toolbar.unselectPage') : t('home.toolbar.selectPage')}
            </button>
          )}
          {isSelectionMode && selectedCount > 0 && (
            <span className="text-xs sm:text-sm text-komgaPrimary font-medium px-2">
              {t('home.toolbar.selectedCount', { count: selectedCount })}
            </span>
          )}
          <span className="text-xs sm:text-sm text-gray-400 font-medium ml-auto sm:ml-0">{t('home.toolbar.sortBy')}</span>
          <select
            value={sortByField}
            onChange={(e) => onSortFieldChange(e.target.value)}
            className="bg-komgaSurface border border-white/10 text-gray-100 text-sm rounded-lg focus:ring-komgaPrimary focus:border-komgaPrimary block p-2 outline-hidden transition-colors cursor-pointer hover:border-white/20 shadow-xs"
          >
            <option value="name">{t('home.toolbar.sort.name')}</option>
            <option value="created">{t('home.toolbar.sort.created')}</option>
            <option value="updated">{t('home.toolbar.sort.updated')}</option>
            <option value="rating">{t('home.toolbar.sort.rating')}</option>
            <option value="volumes">{t('home.toolbar.sort.volumes')}</option>
            <option value="books">{t('home.toolbar.sort.books')}</option>
            <option value="pages">{t('home.toolbar.sort.pages')}</option>
            <option value="read">{t('home.toolbar.sort.read')}</option>
            <option value="favorite">{t('home.toolbar.sort.favorite')}</option>
          </select>
          <button
            onClick={onToggleSortDir}
            className="p-2 bg-komgaSurface border border-white/10 hover:border-white/20 rounded-lg text-gray-400 hover:text-komgaPrimary transition-colors flex items-center justify-center shadow-xs"
            title={sortDir === 'asc' ? t('home.toolbar.sortAsc') : t('home.toolbar.sortDesc')}
          >
            {sortDir === 'asc' ? <ArrowUp className="w-5 h-5" /> : <ArrowDown className="w-5 h-5" />}
          </button>
        </div>
      </div>
    </div>
  );
}
