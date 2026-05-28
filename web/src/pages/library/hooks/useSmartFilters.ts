import { useCallback, useEffect, useState } from 'react';
import axios from 'axios';
import { DEFAULT_PAGE_SIZE, type SavedSmartFilter } from '../types';

interface UseSmartFiltersParams {
  libId: string | undefined;
  onError: (msg: string) => void;
  onSaved: () => void;
  onApplied: (filter: SavedSmartFilter) => void;
}

interface CurrentFilterState {
  activeTag: string | null;
  activeAuthor: string | null;
  activeStatus: string | null;
  activeLetter: string | null;
  sortByField: string;
  sortDir: string;
  pageSize: number;
}

interface UseSmartFiltersResult {
  savedSmartFilters: SavedSmartFilter[];
  saveSmartFilter: (rawName: string, current: CurrentFilterState) => Promise<void>;
  deleteSmartFilter: (id: string) => Promise<void>;
  applySmartFilter: (filter: SavedSmartFilter) => void;
  resetSmartFilters: () => void;
}

function smartFilterStorageKey(libraryId: string) {
  return `lib_smart_filters_${libraryId}`;
}

function smartFilterMigrationKey(libraryId: string) {
  return `lib_smart_filters_migrated_${libraryId}`;
}

function readSavedSmartFilters(libraryId: string): SavedSmartFilter[] {
  try {
    const saved = localStorage.getItem(smartFilterStorageKey(libraryId));
    if (!saved) return [];
    const parsed = JSON.parse(saved) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((item): item is SavedSmartFilter =>
      Boolean(
        item &&
          typeof item === 'object' &&
          'id' in item &&
          'name' in item &&
          typeof (item as SavedSmartFilter).id === 'string' &&
          typeof (item as SavedSmartFilter).name === 'string',
      ),
    );
  } catch {
    return [];
  }
}

function normalizeRemoteSmartFilter(item: SavedSmartFilter): SavedSmartFilter {
  return {
    ...item,
    id: String(item.id),
    activeTag: item.activeTag ?? null,
    activeAuthor: item.activeAuthor ?? null,
    activeStatus: item.activeStatus ?? null,
    activeLetter: item.activeLetter ?? null,
    sortByField: item.sortByField || 'name',
    sortDir: item.sortDir || 'asc',
    pageSize: item.pageSize || DEFAULT_PAGE_SIZE,
    createdAt: item.createdAt || new Date().toISOString(),
  };
}

export function useSmartFilters({
  libId,
  onError,
  onSaved,
  onApplied,
}: UseSmartFiltersParams): UseSmartFiltersResult {
  const [savedSmartFilters, setSavedSmartFilters] = useState<SavedSmartFilter[]>([]);

  // 切库或挂载：迁移 localStorage 旧数据 + 拉服务端列表
  useEffect(() => {
    if (!libId) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setSavedSmartFilters([]);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const legacyItems = readSavedSmartFilters(libId);
        const migrated = localStorage.getItem(smartFilterMigrationKey(libId)) === 'true';
        if (!migrated && legacyItems.length > 0) {
          await Promise.all(
            legacyItems.map((item) =>
              axios.post(`/api/libraries/${libId}/smart-filters/`, {
                name: item.name,
                activeTag: item.activeTag,
                activeAuthor: item.activeAuthor,
                activeStatus: item.activeStatus,
                activeLetter: item.activeLetter,
                sortByField: item.sortByField,
                sortDir: item.sortDir,
                pageSize: item.pageSize,
              }),
            ),
          );
          localStorage.setItem(smartFilterMigrationKey(libId), 'true');
        }

        const res = await axios.get<SavedSmartFilter[]>(`/api/libraries/${libId}/smart-filters/`);
        if (cancelled) return;
        setSavedSmartFilters((res.data || []).map(normalizeRemoteSmartFilter));
      } catch (err) {
        console.error('Failed to load smart filters', err);
        if (cancelled) return;
        setSavedSmartFilters(readSavedSmartFilters(libId));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [libId]);

  const saveSmartFilter = useCallback(
    async (rawName: string, current: CurrentFilterState) => {
      if (!libId) return;
      const name = rawName.trim() || `Filter ${savedSmartFilters.length + 1}`;
      const filter: SavedSmartFilter = {
        id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        name,
        activeTag: current.activeTag,
        activeAuthor: current.activeAuthor,
        activeStatus: current.activeStatus,
        activeLetter: current.activeLetter,
        sortByField: current.sortByField,
        sortDir: current.sortDir,
        pageSize: current.pageSize,
        createdAt: new Date().toISOString(),
      };
      setSavedSmartFilters((prev) =>
        [filter, ...prev.filter((item) => item.name !== name)].slice(0, 20),
      );
      try {
        const res = await axios.post<SavedSmartFilter>(`/api/libraries/${libId}/smart-filters/`, {
          name: filter.name,
          activeTag: filter.activeTag,
          activeAuthor: filter.activeAuthor,
          activeStatus: filter.activeStatus,
          activeLetter: filter.activeLetter,
          sortByField: filter.sortByField,
          sortDir: filter.sortDir,
          pageSize: filter.pageSize,
        });
        const saved = normalizeRemoteSmartFilter(res.data);
        setSavedSmartFilters((current) => [saved, ...current.filter((item) => item.name !== saved.name)].slice(0, 20));
        localStorage.setItem(smartFilterMigrationKey(libId), 'true');
        onSaved();
      } catch (err) {
        console.error('Failed to save smart filter', err);
        onError('home.smartFilters.saveFailed');
      }
    },
    [libId, savedSmartFilters.length, onSaved, onError],
  );

  const deleteSmartFilter = useCallback(
    async (id: string) => {
      if (!libId) return;
      const previous = savedSmartFilters;
      setSavedSmartFilters((prev) => prev.filter((item) => item.id !== id));
      try {
        await axios.delete(`/api/smart-filters/${id}`);
      } catch (err) {
        console.error('Failed to delete smart filter', err);
        setSavedSmartFilters(previous);
        onError('home.smartFilters.deleteFailed');
      }
    },
    [libId, savedSmartFilters, onError],
  );

  const applySmartFilter = useCallback(
    (filter: SavedSmartFilter) => {
      onApplied(filter);
    },
    [onApplied],
  );

  const resetSmartFilters = useCallback(() => {
    onApplied({
      id: 'reset',
      name: '',
      activeTag: null,
      activeAuthor: null,
      activeStatus: null,
      activeLetter: null,
      sortByField: 'name',
      sortDir: 'asc',
      pageSize: DEFAULT_PAGE_SIZE,
      createdAt: new Date().toISOString(),
    });
  }, [onApplied]);

  return { savedSmartFilters, saveSmartFilter, deleteSmartFilter, applySmartFilter, resetSmartFilters };
}
