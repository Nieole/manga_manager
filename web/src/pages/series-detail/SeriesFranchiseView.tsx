/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

import React, { useEffect, useState } from 'react';
import { useI18n } from '../../i18n/LocaleProvider';
import { Link } from 'react-router-dom';
import type { FranchiseRelation } from './types';
import { Share2 } from 'lucide-react';
import axios from 'axios';

interface SeriesFranchiseViewProps {
  seriesId: number;
}

export const SeriesFranchiseView: React.FC<SeriesFranchiseViewProps> = ({ seriesId }) => {
  const { t } = useI18n();
  const [franchiseRelations, setFranchiseRelations] = useState<FranchiseRelation[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    let active = true;
    setIsLoading(true);
    axios.get(`/api/series/${seriesId}/franchise`)
      .then(res => {
        if (active) setFranchiseRelations(res.data || []);
      })
      .catch(console.error)
      .finally(() => {
        if (active) setIsLoading(false);
      });
    return () => { active = false; };
  }, [seriesId]);

  if (isLoading) {
    return <div className="animate-pulse h-32 bg-white/5 rounded-2xl w-full"></div>;
  }

  if (!franchiseRelations || franchiseRelations.length === 0) {
    return null; // Don't show if there are no relations (isolated series)
  }

  // Find all unique series in the connected component
  const seriesMap = new Map<number, { id: number; name: string; cover_path: string; isCurrent: boolean; relationLabel?: string }>();
  
  const INVERSE_RELATIONS: Record<string, string> = {
    'sequel': 'prequel',
    'prequel': 'sequel',
    'spinoff': 'parent_story',
    'parent_story': 'spinoff',
    'side_story': 'parent_story',
    'adaptation': 'source',
    'remake': 'original',
    'same_universe': 'same_universe',
    'alternative_version': 'alternative_version',
    'alternate_story': 'alternate_story',
    'crossover': 'crossover',
    'one_shot': 'serialization',
    'serialization': 'one_shot',
    'doujinshi': 'original',
    'anthology': 'anthology',
    'parent': 'spinoff',
    'source': 'adaptation',
    'original': 'remake'
  };
  
  franchiseRelations.forEach(rel => {
    if (!seriesMap.has(rel.source_series_id)) {
      seriesMap.set(rel.source_series_id, {
        id: rel.source_series_id,
        name: rel.source_series_name,
        cover_path: rel.source_cover_path,
        isCurrent: rel.source_series_id === seriesId
      });
    }
    if (!seriesMap.has(rel.target_series_id)) {
      seriesMap.set(rel.target_series_id, {
        id: rel.target_series_id,
        name: rel.target_series_name,
        cover_path: rel.target_cover_path,
        isCurrent: rel.target_series_id === seriesId
      });
    }

    if (rel.source_series_id === seriesId) {
      const target = seriesMap.get(rel.target_series_id)!;
      if (!target.relationLabel) target.relationLabel = rel.relation_type;
    } else if (rel.target_series_id === seriesId) {
      const source = seriesMap.get(rel.source_series_id)!;
      if (!source.relationLabel) source.relationLabel = INVERSE_RELATIONS[rel.relation_type] || rel.relation_type;
    }
  });

  const allSeries = Array.from(seriesMap.values());
  // If there's only one series in the "franchise" (which shouldn't happen with our query unless graph is single node), hide it.
  if (allSeries.length <= 1) return null;

  // Let's sort them so the current series is first, or maybe alphabetically.
  // A better visual approach: show them as a horizontal scrollable row of cards.
  allSeries.sort((a, b) => {
    if (a.isCurrent) return -1;
    if (b.isCurrent) return 1;
    return a.name.localeCompare(b.name);
  });

  return (
    <div className="mt-12 space-y-4">
      <div className="flex items-center gap-3 border-b border-white/10 pb-4">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-komgaPrimary/10">
          <Share2 className="h-5 w-5 text-komgaPrimary" />
        </div>
        <div className="flex-1">
          <h2 className="text-xl font-bold tracking-tight text-gray-100">
            {t('series.franchise.title')}
          </h2>
          <p className="text-sm text-gray-400">
            {t('series.franchise.description')}
          </p>
        </div>
        <Link
          to={`/series/${seriesId}/franchise-graph`}
          className="rounded-full bg-white/5 px-4 py-2 text-sm font-medium text-gray-300 transition-colors hover:bg-white/10 hover:text-white"
        >
          {t('series.franchise.viewGraph')}
        </Link>
      </div>

      <div className="relative w-full">
        <div className="flex snap-x snap-mandatory gap-4 overflow-x-auto pb-4 scrollbar-thin scrollbar-track-transparent scrollbar-thumb-white/10">
          {allSeries.map((s) => (
            <Link
              key={s.id}
              to={`/series/${s.id}`}
              className={`group relative flex w-36 shrink-0 snap-start flex-col gap-2 rounded-xl p-2 transition-all hover:bg-white/5 ${
                s.isCurrent ? 'ring-2 ring-komgaPrimary bg-komgaPrimary/5' : ''
              }`}
            >
              <div className="aspect-[2/3] w-full overflow-hidden rounded-lg bg-gray-900 shadow-sm border border-white/10">
                {s.cover_path ? (
                  <img
                    src={`/api/thumbnails/${s.cover_path}`}
                    alt={s.name}
                    className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
                    loading="lazy"
                  />
                ) : (
                  <div className="flex h-full w-full items-center justify-center text-gray-700">
                    <span className="text-xs">No Cover</span>
                  </div>
                )}
              </div>
              <div className="flex flex-col">
                <span
                  className={`line-clamp-2 text-sm font-medium ${
                    s.isCurrent ? 'text-komgaPrimary' : 'text-gray-200 group-hover:text-white'
                  }`}
                  title={s.name}
                >
                  {s.name}
                </span>
                {s.isCurrent ? (
                  <span className="mt-1 text-[10px] font-semibold uppercase tracking-wider text-komgaPrimary/80">
                    {t('series.franchise.current')}
                  </span>
                ) : (
                  <span className="mt-1 text-[10px] font-semibold uppercase tracking-wider text-gray-500">
                    {s.relationLabel ? t(`series.relations.type.${s.relationLabel}`, undefined, s.relationLabel) : t('series.relations.type.related')}
                  </span>
                )}
              </div>
            </Link>
          ))}
        </div>
      </div>
    </div>
  );
};
