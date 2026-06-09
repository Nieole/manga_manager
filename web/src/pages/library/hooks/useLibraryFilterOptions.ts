/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useCallback, useEffect, useState } from 'react';
import axios from 'axios';

import type { NamedOption } from '../types';

function includeActiveOption(options: NamedOption[], value: string | null) {
  if (!value || options.some((item) => item.name === value)) return options;
  return [{ name: value }, ...options];
}

interface UseLibraryFilterOptionsParams {
  activeTag: string | null;
  activeAuthor: string | null;
}

export function useLibraryFilterOptions({ activeTag, activeAuthor }: UseLibraryFilterOptionsParams) {
  const [allTags, setAllTags] = useState<NamedOption[]>([]);
  const [allAuthors, setAllAuthors] = useState<NamedOption[]>([]);
  const [filterOptionsLoaded, setFilterOptionsLoaded] = useState(false);
  const [filterOptionsLoading, setFilterOptionsLoading] = useState(false);

  const loadFilterOptions = useCallback(() => {
    if (filterOptionsLoaded || filterOptionsLoading) return;
    setFilterOptionsLoading(true);
    Promise.all([
      axios.get<NamedOption[]>('/api/tags/search?limit=30').catch(() => ({ data: [] as NamedOption[] })),
      axios.get<NamedOption[]>('/api/authors/search?limit=30').catch(() => ({ data: [] as NamedOption[] })),
    ])
      .then(([tRes, aRes]) => {
        const tNames = Array.isArray(tRes.data) ? tRes.data : [];
        const aList = Array.isArray(aRes.data) ? aRes.data : [];
        const map = new Map<string, NamedOption>();
        aList.forEach((a) => map.set(a.name, a));
        setAllTags(includeActiveOption(tNames, activeTag));
        setAllAuthors(includeActiveOption(Array.from(map.values()), activeAuthor));
        setFilterOptionsLoaded(true);
      })
      .catch((err) => console.error('Failed to load filter options', err))
      .finally(() => setFilterOptionsLoading(false));
  }, [filterOptionsLoaded, filterOptionsLoading, activeTag, activeAuthor]);

  useEffect(() => {
     
    if (activeTag || activeAuthor) loadFilterOptions();
  }, [activeTag, activeAuthor, loadFilterOptions]);

  const searchTagOptions = useCallback(
    (query: string) => {
      const params = new URLSearchParams();
      params.set('limit', query ? '50' : '30');
      if (query) params.set('q', query);
      axios
        .get<NamedOption[]>(`/api/tags/search?${params.toString()}`)
        .then((res) => {
          const items = Array.isArray(res.data) ? res.data : [];
          setAllTags(includeActiveOption(items, activeTag));
        })
        .catch((err) => console.error('Failed to search tags', err));
    },
    [activeTag],
  );

  const searchAuthorOptions = useCallback(
    (query: string) => {
      const params = new URLSearchParams();
      params.set('limit', query ? '50' : '30');
      if (query) params.set('q', query);
      axios
        .get<NamedOption[]>(`/api/authors/search?${params.toString()}`)
        .then((res) => {
          const items = Array.isArray(res.data) ? res.data : [];
          setAllAuthors(includeActiveOption(items, activeAuthor));
        })
        .catch((err) => console.error('Failed to search authors', err));
    },
    [activeAuthor],
  );

  return {
    allTags,
    allAuthors,
    filterOptionsLoading,
    loadFilterOptions,
    searchTagOptions,
    searchAuthorOptions,
  };
}
