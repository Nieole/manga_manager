import { ArrowDown, ArrowUp } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';

interface HomeToolbarProps {
  totalSeries: number;
  hasSeries: boolean;
  isSelectionMode: boolean;
  allCurrentPageSelected?: boolean;
  selectedCount?: number;
  sortByField: string;
  sortDir: string;
  onToggleSelectionMode: () => void;
  onToggleSelectCurrentPage?: () => void;
  onSortFieldChange: (value: string) => void;
  onToggleSortDir: () => void;
}

export function HomeToolbar({
  totalSeries,
  hasSeries,
  isSelectionMode,
  allCurrentPageSelected = false,
  selectedCount = 0,
  sortByField,
  sortDir,
  onToggleSelectionMode,
  onToggleSelectCurrentPage,
  onSortFieldChange,
  onToggleSortDir,
}: HomeToolbarProps) {
  const { t } = useI18n();

  return (
    <div className="mb-6 flex flex-col sm:flex-row sm:justify-between sm:items-end gap-4 border-b border-gray-800/30 pb-4">
      <div>
        <h2 className="text-2xl sm:text-3xl font-bold text-white tracking-tight mb-1">{t('home.toolbar.title')}</h2>
        <p className="text-gray-400 text-xs sm:text-sm">{t('home.toolbar.resultCount', { count: totalSeries })}</p>
      </div>
      <div className="flex flex-wrap items-center gap-2 sm:gap-3 mt-4 sm:mt-0 w-full sm:w-auto justify-between sm:justify-end">
        {hasSeries && (
          <button
            onClick={onToggleSelectionMode}
            className={`px-3 py-1.5 text-xs sm:text-sm font-medium rounded-lg transition-colors border focus:outline-none flex-shrink-0 ${isSelectionMode ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-md' : 'bg-transparent border-white/10 text-gray-400 hover:border-white/20 hover:text-white'}`}
          >
            {isSelectionMode ? t('home.toolbar.cancelSelection') : t('home.toolbar.batchActions')}
          </button>
        )}
        {isSelectionMode && hasSeries && onToggleSelectCurrentPage && (
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
          className="bg-komgaSurface border border-white/10 text-gray-100 text-sm rounded-lg focus:ring-komgaPrimary focus:border-komgaPrimary block p-2 outline-none transition-colors cursor-pointer hover:border-white/20 shadow-sm"
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
          className="p-2 bg-komgaSurface border border-white/10 hover:border-white/20 rounded-lg text-gray-400 hover:text-komgaPrimary transition-colors flex items-center justify-center shadow-sm"
          title={sortDir === 'asc' ? t('home.toolbar.sortAsc') : t('home.toolbar.sortDesc')}
        >
          {sortDir === 'asc' ? <ArrowUp className="w-5 h-5" /> : <ArrowDown className="w-5 h-5" />}
        </button>
      </div>
    </div>
  );
}
