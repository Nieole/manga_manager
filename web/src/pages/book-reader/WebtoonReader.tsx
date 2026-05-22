import { forwardRef, useImperativeHandle, useRef } from 'react';
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso';
import type { ImageFilter, Page, ScaleMode } from './types';
import { getFilterStyle, getScaleClasses } from './helpers';

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

interface WebtoonReaderProps {
  t: Translate;
  bookId?: string;
  pages: Page[];
  currentPageIndex: number;
  cachedPageImageUrls: Record<number, string>;
  imageFilter: ImageFilter;
  scaleMode: ScaleMode;
  doublePage: boolean;
  nextBookId: number | null;
  getImageUrl: (bookId: string | undefined, pageNum: number) => string;
  onVisiblePageChange: (pageIndex: number) => void;
  onRenderRangeChange: (startIndex: number, endIndex: number) => void;
  onRenderedImageCountChange: (count: number) => void;
  onOpenNextBook: (bookId: number) => void;
}

export interface WebtoonReaderHandle {
  scrollToPage: (pageNumber: number) => void;
}

export const WebtoonReader = forwardRef<WebtoonReaderHandle, WebtoonReaderProps>(function WebtoonReader({
  t,
  bookId,
  pages,
  currentPageIndex,
  cachedPageImageUrls,
  imageFilter,
  scaleMode,
  doublePage,
  nextBookId,
  getImageUrl,
  onVisiblePageChange,
  onRenderRangeChange,
  onRenderedImageCountChange,
  onOpenNextBook,
}, ref) {
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const itemCount = pages.length + (nextBookId ? 1 : 0);

  useImperativeHandle(ref, () => ({
    scrollToPage(pageNumber: number) {
      const index = Math.max(0, Math.min(pages.length - 1, pageNumber - 1));
      virtuosoRef.current?.scrollToIndex({ index, align: 'start', behavior: 'auto' });
    },
  }), [pages.length]);

  return (
    <Virtuoso
      ref={virtuosoRef}
      className="h-full w-full bg-komgaDark"
      scrollerRef={(element) => {
        rootRef.current = element as HTMLDivElement | null;
      }}
      totalCount={itemCount}
      initialTopMostItemIndex={Math.max(0, Math.min(currentPageIndex, pages.length - 1))}
      increaseViewportBy={{ top: 900, bottom: 1400 }}
      overscan={6}
      rangeChanged={({ startIndex, endIndex }) => {
        if (startIndex < pages.length) {
          onVisiblePageChange(startIndex);
        }
        onRenderRangeChange(startIndex, Math.min(endIndex, pages.length - 1));
        window.requestAnimationFrame(() => {
          onRenderedImageCountChange(rootRef.current?.querySelectorAll('img[data-page-number]').length ?? 0);
        });
      }}
      itemContent={(index) => {
        const page = pages[index];
        if (!page) {
          if (!nextBookId) return null;
          return (
            <div className="flex justify-center py-10">
              <button
                onClick={() => onOpenNextBook(nextBookId)}
                className="rounded-lg bg-komgaPrimary px-8 py-4 text-lg font-bold text-white shadow-2xl transition-all duration-300 hover:bg-komgaPrimaryHover hover:scale-105"
              >
                {t('reader.nextBook')}
              </button>
            </div>
          );
        }

        return (
          <div className="flex w-full justify-center">
            <img
              data-page-number={page.number}
              src={cachedPageImageUrls[page.number] || getImageUrl(bookId, page.number)}
              loading="lazy"
              decoding="async"
              style={getFilterStyle(imageFilter)}
              className={getScaleClasses(scaleMode, doublePage, 'bg-gray-900 min-h-[50vh] drop-shadow-lg max-w-[100vw]')}
              alt={`Page ${page.number}`}
            />
          </div>
        );
      }}
    />
  );
});
