import { useCallback, useState } from 'react';

export interface UseSeriesSelectionParams {
  totalCount: number;
  collectAllIds: () => number[];
}

export function useSeriesSelection({ totalCount, collectAllIds }: UseSeriesSelectionParams) {
  const [isSelectionMode, setIsSelectionMode] = useState(false);
  const [selectedBooks, setSelectedBooks] = useState<number[]>([]);
  const [selectedVolumes, setSelectedVolumes] = useState<string[]>([]);

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
