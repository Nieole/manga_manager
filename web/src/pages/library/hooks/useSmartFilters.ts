import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient } from '../../../api/client';
import { DEFAULT_PAGE_SIZE, type SavedSmartFilter } from '../types';
import { EMPTY_ADVANCED_FILTERS, type AdvancedFilters } from './libraryFilterParams';
import { normalizeRemoteSmartFilter, smartFilterRequestBody } from './smartFilterNormalize';

interface UseSmartFiltersParams {
  libId: string | undefined;
  onError: (msg: string) => void;
  onSaved: () => void;
  onApplied: (filter: SavedSmartFilter) => void;
}

// 保存视图时要收下的当前筛选状态。advanced 与 active* 同等重要——它同样决定视图的成员，
// 漏收就会存出一个条件比用户所见更宽的视图。
interface CurrentFilterState {
  activeTag: string | null;
  activeAuthor: string | null;
  activeStatus: string | null;
  activeLetter: string | null;
  advanced: AdvancedFilters;
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
  ensureLoaded: () => void;
}

function smartFilterStorageKey(libraryId: string) {
  return `lib_smart_filters_${libraryId}`;
}

function smartFilterMigrationKey(libraryId: string) {
  return `lib_smart_filters_migrated_${libraryId}`;
}

function smartFilterCacheKey(libraryId: string) {
  return `lib_smart_filters_cache_${libraryId}`;
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

function readCachedSmartFilters(libraryId: string): SavedSmartFilter[] {
  try {
    const saved = localStorage.getItem(smartFilterCacheKey(libraryId));
    if (!saved) return [];
    const parsed = JSON.parse(saved) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed as SavedSmartFilter[];
  } catch {
    return [];
  }
}

function writeCachedSmartFilters(libraryId: string, items: SavedSmartFilter[]) {
  try {
    localStorage.setItem(smartFilterCacheKey(libraryId), JSON.stringify(items));
  } catch {
    // 配额或隐私模式下写入失败不影响 UI
  }
}

export function useSmartFilters({
  libId,
  onError,
  onSaved,
  onApplied,
}: UseSmartFiltersParams): UseSmartFiltersResult {
  const [savedSmartFilters, setSavedSmartFilters] = useState<SavedSmartFilter[]>([]);
  const loadedLibIdRef = useRef<string | null>(null);

  // 切库或挂载：仅用 localStorage 缓存即时填充，不发任何网络请求
  useEffect(() => {
    if (!libId) {
       
      setSavedSmartFilters([]);
      loadedLibIdRef.current = null;
      return;
    }
     
    setSavedSmartFilters(readCachedSmartFilters(libId));
    loadedLibIdRef.current = null;
  }, [libId]);

  // ensureLoaded：在用户真正展开"智能筛选视图"面板时再去拉服务端列表
  // 同一资料库同一会话内只会调用一次（StrictMode 双倍 effect 也只触发一次远端请求）
  const ensureLoaded = useCallback(() => {
    if (!libId) return;
    if (loadedLibIdRef.current === libId) return;
    loadedLibIdRef.current = libId;
    (async () => {
      try {
        const legacyItems = readSavedSmartFilters(libId);
        const migrated = localStorage.getItem(smartFilterMigrationKey(libId)) === 'true';
        if (!migrated && legacyItems.length > 0) {
          await Promise.all(
            legacyItems.map((item) =>
              apiClient.post(`/api/libraries/${libId}/smart-filters/`, smartFilterRequestBody(item)),
            ),
          );
          localStorage.setItem(smartFilterMigrationKey(libId), 'true');
        }

        const res = await apiClient.get<SavedSmartFilter[]>(`/api/libraries/${libId}/smart-filters/`);
        const normalized = (res.data || []).map(normalizeRemoteSmartFilter);
        setSavedSmartFilters(normalized);
        writeCachedSmartFilters(libId, normalized);
      } catch (err) {
        console.error('Failed to load smart filters', err);
        loadedLibIdRef.current = null;
        setSavedSmartFilters(readSavedSmartFilters(libId));
      }
    })();
  }, [libId]);

  const saveSmartFilter = useCallback(
    async (rawName: string, current: CurrentFilterState) => {
      if (!libId) return;
      const name = rawName.trim() || `Filter ${savedSmartFilters.length + 1}`;
      const previous = savedSmartFilters;
      const filter: SavedSmartFilter = {
        id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        name,
        activeTag: current.activeTag,
        activeAuthor: current.activeAuthor,
        activeStatus: current.activeStatus,
        activeLetter: current.activeLetter,
        ...current.advanced,
        sortByField: current.sortByField,
        sortDir: current.sortDir,
        pageSize: current.pageSize,
        createdAt: new Date().toISOString(),
      };
      setSavedSmartFilters((prev) =>
        [filter, ...prev.filter((item) => item.name !== name)].slice(0, 20),
      );
      try {
        const res = await apiClient.post<SavedSmartFilter>(
          `/api/libraries/${libId}/smart-filters/`,
          smartFilterRequestBody(filter),
        );
        const saved = normalizeRemoteSmartFilter(res.data);
        setSavedSmartFilters((current) => {
          const next = [saved, ...current.filter((item) => item.name !== saved.name)].slice(0, 20);
          writeCachedSmartFilters(libId, next);
          return next;
        });
        localStorage.setItem(smartFilterMigrationKey(libId), 'true');
        onSaved();
      } catch (err) {
        console.error('Failed to save smart filter', err);
        // 与 deleteSmartFilter 一样要回滚乐观插入：留下的条目带的是客户端 id，后端并不认识它，
        // 用户去删只会拿到 404，再被删除那侧的回滚原样放回来，只能刷新页面才消得掉。
        setSavedSmartFilters(previous);
        onError('home.smartFilters.saveFailed');
      }
    },
    [libId, savedSmartFilters, onSaved, onError],
  );

  const deleteSmartFilter = useCallback(
    async (id: string) => {
      if (!libId) return;
      const previous = savedSmartFilters;
      setSavedSmartFilters((prev) => {
        const next = prev.filter((item) => item.id !== id);
        writeCachedSmartFilters(libId, next);
        return next;
      });
      try {
        await apiClient.delete(`/api/smart-filters/${id}`);
      } catch (err) {
        console.error('Failed to delete smart filter', err);
        setSavedSmartFilters(previous);
        writeCachedSmartFilters(libId, previous);
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
      ...EMPTY_ADVANCED_FILTERS,
      sortByField: 'name',
      sortDir: 'asc',
      pageSize: DEFAULT_PAGE_SIZE,
      createdAt: new Date().toISOString(),
    });
  }, [onApplied]);

  return { savedSmartFilters, saveSmartFilter, deleteSmartFilter, applySmartFilter, resetSmartFilters, ensureLoaded };
}
