/**
 * 业务说明：本文件是业务实现，属于前端共享组件层，负责沉淀按钮、面板、列表、封面、进度和反馈等可复用 UI 片段。
 * 它让资料库、阅读器、设置和系列详情在视觉和交互上保持一致。
 * 维护时应关注组件职责边界、可访问性、主题变量、加载态和不同页面的复用语义。
 */

import { useEffect, useState } from 'react';
import { apiClient } from '../../api/client';
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
      apiClient
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
