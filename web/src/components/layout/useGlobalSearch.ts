import { useEffect, useState } from 'react';
import axios from 'axios';
import type { SearchHit } from './types';

export function useGlobalSearch() {
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<SearchHit[]>([]);
  const [isSearchModalOpen, setIsSearchModalOpen] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [searchTarget, setSearchTarget] = useState('all');

  useEffect(() => {
    if (!searchQuery.trim()) {
      setSearchResults([]);
      return;
    }

    const timer = setTimeout(() => {
      axios
        .get(`/api/search?q=${encodeURIComponent(searchQuery)}&target=${searchTarget}`)
        .then((res) => {
          if (res.data && res.data.hits) {
            setSearchResults(res.data.hits);
          } else {
            setSearchResults([]);
          }
        })
        .catch((err) => console.error('Search failed:', err));
    }, 300);

    return () => clearTimeout(timer);
  }, [searchQuery, searchTarget]);

  useEffect(() => {
    setSelectedIndex(0);
  }, [searchResults]);

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
    setSearchQuery,
    searchResults,
    isSearchModalOpen,
    setIsSearchModalOpen,
    selectedIndex,
    setSelectedIndex,
    searchTarget,
    setSearchTarget,
    resetSearch,
  };
}
