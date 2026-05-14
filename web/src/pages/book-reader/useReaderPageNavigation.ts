import { useCallback, type Dispatch, type MutableRefObject, type SetStateAction } from 'react';
import type { Page, ReadMode } from './types';

interface UseReaderPageNavigationOptions {
  activePages: Page[];
  doublePage: boolean;
  readModeRef: MutableRefObject<ReadMode>;
  nextBookIdRef: MutableRefObject<number | null>;
  setCurrentPageIndex: Dispatch<SetStateAction<number>>;
  setSliderValue: Dispatch<SetStateAction<number>>;
  onOpenBook: (bookId: number) => void;
}

export function useReaderPageNavigation({
  activePages,
  doublePage,
  readModeRef,
  nextBookIdRef,
  setCurrentPageIndex,
  setSliderValue,
  onOpenBook,
}: UseReaderPageNavigationOptions) {
  const jumpToPage = useCallback((pageNumber: number) => {
    const targetIndex = Math.max(0, Math.min(activePages.length - 1, pageNumber - 1));
    setSliderValue(targetIndex + 1);
    if (readModeRef.current === 'paged') {
      setCurrentPageIndex(targetIndex);
      return;
    }

    const targetImg = document.querySelector(`img[data-page-number="${targetIndex + 1}"]`);
    if (targetImg) {
      targetImg.scrollIntoView({ behavior: 'auto', block: 'center' });
    }
  }, [activePages.length, readModeRef, setCurrentPageIndex, setSliderValue]);

  const handleNext = useCallback(() => {
    const step = doublePage ? 2 : 1;
    setCurrentPageIndex((prev) => {
      if (prev + step >= activePages.length) {
        if (nextBookIdRef.current) {
          setTimeout(() => onOpenBook(nextBookIdRef.current as number), 0);
        }
        return prev;
      }
      return Math.min(prev + step, activePages.length - 1);
    });
  }, [activePages.length, doublePage, nextBookIdRef, onOpenBook, setCurrentPageIndex]);

  const handlePrev = useCallback(() => {
    const step = doublePage ? 2 : 1;
    setCurrentPageIndex((prev) => Math.max(prev - step, 0));
  }, [doublePage, setCurrentPageIndex]);

  const firstPage = useCallback(() => {
    setCurrentPageIndex(0);
  }, [setCurrentPageIndex]);

  const lastPage = useCallback(() => {
    setCurrentPageIndex(Math.max(0, activePages.length - 1));
  }, [activePages.length, setCurrentPageIndex]);

  return {
    jumpToPage,
    handleNext,
    handlePrev,
    firstPage,
    lastPage,
  };
}
