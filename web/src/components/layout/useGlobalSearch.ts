import { useEffect, useState } from 'react';
import axios from 'axios';
import type { SearchHit } from './types';

export function useGlobalSearch() {
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<SearchHit[]>([]);
  const [isSearchModalOpen, setIsSearchModalOpen] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [searchTarget, setSearchTarget] = useState('all');
  const visibleSearchResults = searchQuery.trim() ? searchResults : [];

  useEffect(() => {
    const query = searchQuery.trim();
    if (!query) {
      return;
    }

    const controller = new AbortController();
    const timer = setTimeout(() => {
      axios
        .get(`/api/search?q=${encodeURIComponent(query)}&target=${searchTarget}`, {
          signal: controller.signal,
        })
        .then((res) => {
          if (res.data && res.data.hits) {
            setSearchResults(res.data.hits);
          } else {
            setSearchResults([]);
          }
          setSelectedIndex(0);
        })
        .catch((err) => {
          if (!axios.isCancel(err)) {
            console.error('Search failed:', err);
          }
        });
    }, 300);

    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [searchQuery, searchTarget]);

  const updateSearchQuery = (value: string) => {
    setSearchQuery(value);
    setSelectedIndex(0);
  };

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setIsSearchModalOpen(true);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  const resetSearch = () => {
    setSearchQuery('');
    setSearchResults([]);
    setSelectedIndex(0);
  };

  return {
    searchQuery,
    setSearchQuery: updateSearchQuery,
    searchResults: visibleSearchResults,
    isSearchModalOpen,
    setIsSearchModalOpen,
    selectedIndex,
    setSelectedIndex,
    searchTarget,
    setSearchTarget,
    resetSearch,
  };
}
