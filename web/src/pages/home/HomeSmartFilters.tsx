import { useState } from 'react';
import { BookmarkPlus, FilterX, Trash2 } from 'lucide-react';
import type { SavedSmartFilter } from './types';
import { useI18n } from '../../i18n/LocaleProvider';
import { normalizeSeriesStatus } from '../../i18n/status';

interface HomeSmartFiltersProps {
  savedFilters: SavedSmartFilter[];
  activeTag: string | null;
  activeAuthor: string | null;
  activeStatus: string | null;
  activeLetter: string | null;
  sortByField: string;
  sortDir: string;
  pageSize: number;
  onSave: (name: string) => void;
  onApply: (filter: SavedSmartFilter) => void;
  onDelete: (id: string) => void;
  onReset: () => void;
}

function hasActiveFilter(props: Pick<HomeSmartFiltersProps, 'activeTag' | 'activeAuthor' | 'activeStatus' | 'activeLetter' | 'sortByField' | 'sortDir' | 'pageSize'>) {
  return Boolean(
    props.activeTag
    || props.activeAuthor
    || props.activeStatus
    || props.activeLetter
    || props.sortByField !== 'name'
    || props.sortDir !== 'asc'
    || props.pageSize !== 30
  );
}

export function HomeSmartFilters({
  savedFilters,
  activeTag,
  activeAuthor,
  activeStatus,
  activeLetter,
  sortByField,
  sortDir,
  pageSize,
  onSave,
  onApply,
  onDelete,
  onReset,
}: HomeSmartFiltersProps) {
  const { t } = useI18n();
  const [name, setName] = useState('');
  const active = hasActiveFilter({ activeTag, activeAuthor, activeStatus, activeLetter, sortByField, sortDir, pageSize });
  const chips = [
    activeStatus ? t(`status.${normalizeSeriesStatus(activeStatus)}`) : null,
    activeTag ? t('home.smartFilters.chipTag', { value: activeTag }) : null,
    activeAuthor ? t('home.smartFilters.chipAuthor', { value: activeAuthor }) : null,
    activeLetter ? t('home.smartFilters.chipLetter', { value: activeLetter }) : null,
    t('home.smartFilters.chipSort', { field: t(`home.toolbar.sort.${sortByField}`), dir: sortDir === 'asc' ? t('home.smartFilters.dir.asc') : t('home.smartFilters.dir.desc') }),
    t('home.smartFilters.chipPageSize', { count: pageSize }),
  ].filter(Boolean);

  const handleSave = () => {
    onSave(name);
    setName('');
  };

  return (
    <div className="mb-6 rounded-2xl border border-white/10 bg-komgaSurface/70 p-4 shadow-sm backdrop-blur-md">
      <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold text-white">{t('home.smartFilters.title')}</h3>
            {active && (
              <span className="rounded-full border border-komgaPrimary/30 bg-komgaPrimary/10 px-2 py-0.5 text-[11px] font-semibold text-komgaPrimary">
                {t('home.smartFilters.currentActive')}
              </span>
            )}
          </div>
          <p className="mt-1 text-xs leading-5 text-gray-400">{t('home.smartFilters.description')}</p>
          <div className="mt-3 flex flex-wrap gap-2">
            {chips.map((chip) => (
              <span key={chip} className="rounded-lg border border-white/10 bg-gray-950/60 px-2.5 py-1 text-[11px] text-gray-300">
                {chip}
              </span>
            ))}
          </div>
        </div>

        <div className="flex w-full flex-col gap-3 xl:w-[520px]">
          <div className="flex flex-col gap-2 sm:flex-row">
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={t('home.smartFilters.namePlaceholder')}
              className="min-w-0 flex-1 rounded-xl border border-white/10 bg-gray-950 px-3 py-2 text-sm text-gray-100 outline-none transition-colors placeholder:text-gray-600 focus:border-komgaPrimary"
            />
            <button
              onClick={handleSave}
              className="inline-flex items-center justify-center gap-2 rounded-xl border border-komgaPrimary/30 bg-komgaPrimary/10 px-4 py-2 text-sm font-semibold text-komgaPrimary hover:bg-komgaPrimary/20"
            >
              <BookmarkPlus className="h-4 w-4" />
              {t('home.smartFilters.save')}
            </button>
            <button
              onClick={onReset}
              className="inline-flex items-center justify-center gap-2 rounded-xl border border-white/10 bg-gray-950 px-4 py-2 text-sm font-medium text-gray-300 hover:text-white"
            >
              <FilterX className="h-4 w-4" />
              {t('home.smartFilters.reset')}
            </button>
          </div>

          {savedFilters.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {savedFilters.map((filter) => (
                <div key={filter.id} className="group inline-flex overflow-hidden rounded-xl border border-white/10 bg-gray-950/70">
                  <button
                    onClick={() => onApply(filter)}
                    className="px-3 py-2 text-left text-xs font-medium text-gray-200 hover:bg-komgaPrimary/10 hover:text-white"
                    title={filter.name}
                  >
                    {filter.name}
                  </button>
                  <button
                    onClick={() => onDelete(filter.id)}
                    className="border-l border-white/10 px-2 text-gray-500 hover:bg-red-500/10 hover:text-red-300"
                    title={t('home.smartFilters.delete')}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-xs text-gray-500">{t('home.smartFilters.empty')}</p>
          )}
        </div>
      </div>
    </div>
  );
}
