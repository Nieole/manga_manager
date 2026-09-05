import { useCallback, useEffect, useState } from 'react';
import { apiClient } from '../../../api/client';
import { useLatestRequest } from '../../../hooks/useLatestRequest';

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
  // 标签与作者的联想各记一个世代号：只有 250 毫秒防抖挡不住在途请求，慢网下先发的响应后到，
  // 下拉里列的就是上一次输入的候选。两条链路互不作废。
  const tagRequest = useLatestRequest();
  const authorRequest = useLatestRequest();

  const loadFilterOptions = useCallback(() => {
    if (filterOptionsLoaded || filterOptionsLoading) return;
    setFilterOptionsLoading(true);
    Promise.all([
      apiClient.get<NamedOption[]>('/api/tags/search?limit=30').catch(() => ({ data: [] as NamedOption[] })),
      apiClient.get<NamedOption[]>('/api/authors/search?limit=30').catch(() => ({ data: [] as NamedOption[] })),
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
      void tagRequest.run(
        () => apiClient.get<NamedOption[]>(`/api/tags/search?${params.toString()}`),
        {
          onSuccess: (res) => {
            const items = Array.isArray(res.data) ? res.data : [];
            setAllTags(includeActiveOption(items, activeTag));
          },
          onError: (err) => console.error('Failed to search tags', err),
        },
      );
    },
    [activeTag, tagRequest],
  );

  const searchAuthorOptions = useCallback(
    (query: string) => {
      const params = new URLSearchParams();
      params.set('limit', query ? '50' : '30');
      if (query) params.set('q', query);
      void authorRequest.run(
        () => apiClient.get<NamedOption[]>(`/api/authors/search?${params.toString()}`),
        {
          onSuccess: (res) => {
            const items = Array.isArray(res.data) ? res.data : [];
            setAllAuthors(includeActiveOption(items, activeAuthor));
          },
          onError: (err) => console.error('Failed to search authors', err),
        },
      );
    },
    [activeAuthor, authorRequest],
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
