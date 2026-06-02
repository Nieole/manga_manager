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
