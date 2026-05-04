import { Link } from 'react-router-dom';
import { GitBranch, Link2, Plus, Trash2, X } from 'lucide-react';
import type { SeriesRelation, SeriesRelationCandidate } from './types';
import { useI18n } from '../../i18n/LocaleProvider';

interface SeriesRelationsPanelProps {
  relations: SeriesRelation[];
  candidates: SeriesRelationCandidate[];
  relationType: string;
  relationSearch: string;
  selectedTargetId: number | null;
  isAdding: boolean;
  isLoadingCandidates: boolean;
  onRelationTypeChange: (value: string) => void;
  onSearchChange: (value: string) => void;
  onSelectTarget: (id: number) => void;
  onAddRelation: () => void;
  onDeleteRelation: (relation: SeriesRelation) => void;
}

const RELATION_TYPES = ['sequel', 'prequel', 'spinoff', 'side_story', 'adaptation', 'remake', 'same_universe'];

function relationLabelKey(type: string) {
  return `series.relations.type.${type}`;
}

export function SeriesRelationsPanel({
  relations,
  candidates,
  relationType,
  relationSearch,
  selectedTargetId,
  isAdding,
  isLoadingCandidates,
  onRelationTypeChange,
  onSearchChange,
  onSelectTarget,
  onAddRelation,
  onDeleteRelation,
}: SeriesRelationsPanelProps) {
  const { t } = useI18n();
  const selectedTarget = candidates.find((item) => item.id === selectedTargetId) || null;

  return (
    <section className="relative z-20 mb-8 rounded-2xl border border-white/10 bg-komgaSurface/80 p-5 shadow-xl backdrop-blur-md">
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h3 className="flex items-center gap-2 text-lg font-semibold text-white">
            <GitBranch className="h-5 w-5 text-komgaPrimary" />
            {t('series.relations.title')}
          </h3>
          <p className="mt-1 text-sm text-gray-400">{t('series.relations.description')}</p>
        </div>
        <span className="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs text-gray-300">
          {t('series.relations.count', { count: relations.length })}
        </span>
      </div>

      {relations.length > 0 ? (
        <div className="mb-5 flex flex-wrap gap-2">
          {relations.map((relation) => (
            <div key={relation.id} className="group inline-flex items-center overflow-hidden rounded-xl border border-white/10 bg-gray-950/50">
              <Link
                to={`/series/${relation.target_series_id}`}
                className="inline-flex items-center gap-2 px-3 py-2 text-sm text-gray-100 hover:bg-komgaPrimary/10 hover:text-white"
              >
                <Link2 className="h-4 w-4 text-komgaPrimary" />
                <span className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-gray-300">
                  {t(relationLabelKey(relation.relation_type))}
                </span>
                <span className="font-medium">{relation.target_series_name}</span>
              </Link>
              <button
                onClick={() => onDeleteRelation(relation)}
                className="self-stretch border-l border-white/10 px-2 text-gray-500 hover:bg-red-500/10 hover:text-red-300"
                title={t('series.relations.delete')}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          ))}
        </div>
      ) : (
        <div className="mb-5 rounded-xl border border-dashed border-white/10 bg-gray-950/30 px-4 py-5 text-sm text-gray-400">
          {t('series.relations.empty')}
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-[160px_1fr_auto]">
        <select
          value={relationType}
          onChange={(event) => onRelationTypeChange(event.target.value)}
          className="rounded-xl border border-white/10 bg-gray-950 px-3 py-2 text-sm text-gray-100 outline-none focus:border-komgaPrimary"
        >
          {RELATION_TYPES.map((type) => (
            <option key={type} value={type}>
              {t(relationLabelKey(type))}
            </option>
          ))}
        </select>

        <div className="relative">
          <input
            value={relationSearch}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder={t('series.relations.searchPlaceholder')}
            className="w-full rounded-xl border border-white/10 bg-gray-950 px-3 py-2 text-sm text-gray-100 outline-none focus:border-komgaPrimary"
          />
          {selectedTarget && (
            <button
              onClick={() => onSelectTarget(0)}
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded-lg p-1 text-gray-500 hover:bg-white/10 hover:text-white"
              title={t('common.clear')}
            >
              <X className="h-4 w-4" />
            </button>
          )}
          {!selectedTarget && relationSearch.trim() && (
            <div className="absolute left-0 right-0 top-full z-50 mt-2 max-h-80 overflow-y-auto rounded-xl border border-white/10 bg-gray-950 shadow-2xl">
              {isLoadingCandidates ? (
                <div className="px-4 py-4 text-sm text-gray-500">{t('common.loading')}</div>
              ) : candidates.length > 0 ? (
                candidates.map((candidate) => {
                  const displayTitle = candidate.title?.Valid ? candidate.title.String : candidate.name;
                  const hasAlias = candidate.title?.Valid && candidate.title.String !== candidate.name;
                  const coverUrl = candidate.cover_path?.Valid ? `/api/thumbnails/${candidate.cover_path.String}` : null;
                  return (
                    <button
                      key={candidate.id}
                      onClick={() => onSelectTarget(candidate.id)}
                      className="flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors hover:bg-komgaPrimary/10 border-b border-white/5 last:border-b-0"
                    >
                      <div className="h-12 w-9 shrink-0 overflow-hidden rounded-md border border-white/10 bg-gray-900">
                        {coverUrl ? (
                          <img src={coverUrl} alt="" className="h-full w-full object-cover" loading="lazy" />
                        ) : (
                          <div className="flex h-full w-full items-center justify-center text-gray-700 text-xs">?</div>
                        )}
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium text-gray-100">{displayTitle}</p>
                        {hasAlias && (
                          <p className="truncate text-xs text-gray-500">{candidate.name}</p>
                        )}
                      </div>
                      <span className="shrink-0 text-xs text-gray-600">#{candidate.id}</span>
                    </button>
                  );
                })
              ) : (
                <div className="px-4 py-4 text-sm text-gray-500">{t('series.relations.noCandidates')}</div>
              )}
            </div>
          )}
        </div>

        <button
          onClick={onAddRelation}
          disabled={!selectedTargetId || isAdding}
          className="inline-flex items-center justify-center gap-2 rounded-xl bg-komgaPrimary px-4 py-2 text-sm font-semibold text-white shadow-lg shadow-komgaPrimary/20 hover:bg-komgaPrimaryHover disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Plus className="h-4 w-4" />
          {isAdding ? t('common.saving') : t('series.relations.add')}
        </button>
      </div>
    </section>
  );
}
