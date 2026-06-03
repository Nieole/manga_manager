import { useCallback, useEffect, useState } from 'react';
import axios from 'axios';
import type { SeriesRelation, SeriesRelationCandidate } from '../types';

function getApiErrorMessage(error: unknown, fallback: string) {
  if (axios.isAxiosError(error)) return error.response?.data?.error || error.message || fallback;
  if (error instanceof Error) return error.message;
  return fallback;
}

interface UseSeriesRelationsParams {
  seriesId: string | undefined;
  libraryId: number | undefined;
  relations: SeriesRelation[];
  setRelations: React.Dispatch<React.SetStateAction<SeriesRelation[]>>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesRelations({ seriesId, libraryId, relations, setRelations, showToast, t }: UseSeriesRelationsParams) {
  const [relationType, setRelationType] = useState('sequel');
  const [relationSearch, setRelationSearch] = useState('');
  const [relationCandidates, setRelationCandidates] = useState<SeriesRelationCandidate[]>([]);
  const [selectedTargetId, setSelectedTargetId] = useState<number | null>(null);
  const [isLoadingCandidates, setIsLoadingCandidates] = useState(false);
  const [isAdding, setIsAdding] = useState(false);

  useEffect(() => {
    if (!libraryId || !relationSearch.trim()) {
      setRelationCandidates([]);
      return;
    }
    let active = true;
    const timer = window.setTimeout(() => {
      setIsLoadingCandidates(true);
      axios
        .get<{ items?: SeriesRelationCandidate[] }>(`/api/series/search`, {
          params: { libraryId, q: relationSearch.trim(), limit: 20, page: 1, sortBy: 'name_asc' },
        })
        .then((res) => {
          if (!active) return;
          const existingIds = new Set(relations.map((r) => r.target_series_id));
          const items = (res.data.items || [])
            .filter((item) => item.id !== Number(seriesId))
            .filter((item) => !existingIds.has(item.id))
            .slice(0, 12);
          setRelationCandidates(items);
        })
        .catch(() => {
          if (active) setRelationCandidates([]);
        })
        .finally(() => {
          if (active) setIsLoadingCandidates(false);
        });
    }, 200);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [relationSearch, relations, seriesId, libraryId]);

  const onSearchChange = useCallback((value: string) => {
    setRelationSearch(value);
    setSelectedTargetId(null);
  }, []);

  const onSelectTarget = useCallback(
    (id: number) => {
      if (id === 0) {
        setSelectedTargetId(null);
        setRelationSearch('');
        return;
      }
      const target = relationCandidates.find((item) => item.id === id);
      setSelectedTargetId(id);
      setRelationSearch(target?.title?.Valid ? target.title.String : target?.name || '');
    },
    [relationCandidates],
  );

  const addRelation = useCallback(async () => {
    if (!seriesId || !selectedTargetId) return;
    setIsAdding(true);
    try {
      await axios.post(`/api/series/${seriesId}/relations`, {
        target_series_id: selectedTargetId,
        relation_type: relationType,
      });
      const res = await axios.get<SeriesRelation[]>(`/api/series/${seriesId}/relations`);
      setRelations(Array.isArray(res.data) ? res.data : []);
      setSelectedTargetId(null);
      setRelationSearch('');
      showToast(t('series.toast.relationAdded'), 'success');
    } catch (err) {
      showToast(getApiErrorMessage(err, t('series.toast.relationAddFailed')), 'error');
    } finally {
      setIsAdding(false);
    }
  }, [seriesId, selectedTargetId, relationType, setRelations, showToast, t]);

  const deleteRelation = useCallback(
    async (relation: SeriesRelation) => {
      try {
        await axios.delete(`/api/relations/${relation.id}`);
        setRelations((prev) => prev.filter((item) => item.id !== relation.id));
        showToast(t('series.toast.relationDeleted'), 'success');
      } catch (err) {
        showToast(getApiErrorMessage(err, t('series.toast.relationDeleteFailed')), 'error');
      }
    },
    [setRelations, showToast, t],
  );

  const updateRelation = useCallback(
    async (relation: SeriesRelation, newType: string) => {
      try {
        await axios.put(`/api/relations/${relation.id}`, { relation_type: newType });
        setRelations((prev) =>
          prev.map((item) => (item.id === relation.id ? { ...item, relation_type: newType } : item))
        );
        showToast(t('series.toast.relationUpdated') || 'Relation updated', 'success');
      } catch (err) {
        showToast(getApiErrorMessage(err, t('series.toast.relationUpdateFailed') || 'Failed to update relation'), 'error');
      }
    },
    [setRelations, showToast, t],
  );

  return {
    relationType,
    setRelationType,
    relationSearch,
    relationCandidates,
    selectedTargetId,
    isLoadingCandidates,
    isAdding,
    onSearchChange,
    onSelectTarget,
    addRelation,
    updateRelation,
    deleteRelation,
  };
}
