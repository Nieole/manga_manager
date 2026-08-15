import { useCallback, type Dispatch, type MutableRefObject, type SetStateAction } from 'react';
import type { Page, ReadMode } from './types';
import { lastPageIndex, nextPageIndex, pageNumberToIndex, prevPageIndex } from './readerPageNavigation';

interface UseReaderPageNavigationOptions {
  activePages: Page[];
  doublePage: boolean;
  readModeRef: MutableRefObject<ReadMode>;
  nextBookIdRef: MutableRefObject<number | null>;
  setCurrentPageIndex: Dispatch<SetStateAction<number>>;
  setSliderValue: Dispatch<SetStateAction<number>>;
  onScrollToWebtoonPage?: (pageNumber: number) => void;
  onOpenBook: (bookId: number) => void;
}

export function useReaderPageNavigation({
  activePages,
  doublePage,
  readModeRef,
  nextBookIdRef,
  setCurrentPageIndex,
  setSliderValue,
  onScrollToWebtoonPage,
  onOpenBook,
}: UseReaderPageNavigationOptions) {
  const jumpToPage = useCallback((pageNumber: number) => {
    const targetIndex = pageNumberToIndex(pageNumber, activePages.length);
    setSliderValue(targetIndex + 1);
    if (readModeRef.current === 'paged') {
      setCurrentPageIndex(targetIndex);
      return;
    }

    setCurrentPageIndex(targetIndex);
    onScrollToWebtoonPage?.(targetIndex + 1);
  }, [activePages.length, onScrollToWebtoonPage, readModeRef, setCurrentPageIndex, setSliderValue]);

  const handleNext = useCallback(() => {
    setCurrentPageIndex((prev) => {
      const { index, atEnd } = nextPageIndex(prev, activePages.length, doublePage);
      if (atEnd && nextBookIdRef.current) {
        setTimeout(() => onOpenBook(nextBookIdRef.current as number), 0);
      }
      return index;
    });
  }, [activePages.length, doublePage, nextBookIdRef, onOpenBook, setCurrentPageIndex]);

  const handlePrev = useCallback(() => {
    setCurrentPageIndex((prev) => prevPageIndex(prev, doublePage));
  }, [doublePage, setCurrentPageIndex]);

  const firstPage = useCallback(() => {
    setCurrentPageIndex(0);
  }, [setCurrentPageIndex]);

  const lastPage = useCallback(() => {
    setCurrentPageIndex(lastPageIndex(activePages.length));
  }, [activePages.length, setCurrentPageIndex]);

  return {
    jumpToPage,
    handleNext,
    handlePrev,
    firstPage,
    lastPage,
  };
}
