import { useCallback, useEffect, useState } from 'react';

export interface UseSeriesSelectionParams {
  totalCount: number;
  collectAllIds: () => number[];
  // resetKey 变化时清空选择态（传当前 seriesId）。系列详情路由无 key，经关系图/合集在系列间跳转不会重挂载，
  // 若不重置，选中项会残留到新系列——批量「标记已读」会写到上一个系列的书上（写错数据）。
  resetKey?: string | number;
}

export function useSeriesSelection({ totalCount, collectAllIds, resetKey }: UseSeriesSelectionParams) {
  const [isSelectionMode, setIsSelectionMode] = useState(false);
  const [selectedBooks, setSelectedBooks] = useState<number[]>([]);
  const [selectedVolumes, setSelectedVolumes] = useState<string[]>([]);

  useEffect(() => {
    setIsSelectionMode(false);
    setSelectedBooks([]);
    setSelectedVolumes([]);
  }, [resetKey]);

  const clear = useCallback(() => {
    setSelectedBooks([]);
    setSelectedVolumes([]);
  }, []);

  const toggleSelectionMode = useCallback(() => {
    setIsSelectionMode((prev) => {
      if (prev) {
        setSelectedBooks([]);
        setSelectedVolumes([]);
      }
      return !prev;
    });
  }, []);

  const toggleBook = useCallback((bookId: number) => {
    setSelectedBooks((prev) => (prev.includes(bookId) ? prev.filter((id) => id !== bookId) : [...prev, bookId]));
  }, []);

  const toggleVolume = useCallback((name: string) => {
    setSelectedVolumes((prev) => (prev.includes(name) ? prev.filter((n) => n !== name) : [...prev, name]));
  }, []);

  const selectAllOrNone = useCallback(() => {
    const all = collectAllIds();
    setSelectedBooks((prev) => (prev.length >= all.length ? [] : all));
    setSelectedVolumes([]);
  }, [collectAllIds]);

  const selectedCount = selectedBooks.length + selectedVolumes.length;
  const allSelected = selectedCount === totalCount && totalCount > 0;

  return {
    isSelectionMode,
    selectedBooks,
    selectedVolumes,
    selectedCount,
    allSelected,
    clear,
    toggleSelectionMode,
    toggleBook,
    toggleVolume,
    selectAllOrNone,
    setSelectedBooks,
    setSelectedVolumes,
    setIsSelectionMode,
  };
}
