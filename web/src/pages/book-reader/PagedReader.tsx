import type { CSSProperties, MouseEvent, RefObject } from 'react';
import { ChevronLeft, ChevronRight, Loader2 } from 'lucide-react';
import { getFilterStyle, getPagedImages, getScaleClasses } from './helpers';
import type { ImageFilter, Page, ReadDirection, ScaleMode } from './types';

interface PagedReaderProps {
  pages: Page[];
  currentPageIndex: number;
  doublePage: boolean;
  readDirection: ReadDirection;
  scaleMode: ScaleMode;
  imageFilter: ImageFilter;
  isDragging: boolean;
  containerRef: RefObject<HTMLDivElement | null>;
  cachedPageImageUrls: Record<number, string>;
  isPagedImageReady: (pageNum: number) => boolean;
  onPrev: () => void;
  onNext: () => void;
  onMouseDown: (event: MouseEvent<HTMLDivElement>) => void;
  onMouseLeave: () => void;
  onMouseUp: () => void;
  onMouseMove: (event: MouseEvent<HTMLDivElement>) => void;
}

export function PagedReader({
  pages,
  currentPageIndex,
  doublePage,
  readDirection,
  scaleMode,
  imageFilter,
  isDragging,
  containerRef,
  cachedPageImageUrls,
  isPagedImageReady,
  onPrev,
  onNext,
  onMouseDown,
  onMouseLeave,
  onMouseUp,
  onMouseMove,
}: PagedReaderProps) {
  return (
    <div className="flex items-center justify-center w-full h-full bg-komgaDark relative">
      <div
        className="absolute left-0 inset-y-0 w-[20vw] sm:w-1/3 z-10 flex items-center justify-start sm:px-8 cursor-pointer md:hover:bg-white/5 transition opacity-0 md:hover:opacity-100 group"
        onClick={() => readDirection === 'ltr' ? onPrev() : onNext()}
      >
        <ChevronLeft className="w-12 h-12 text-white/40 group-hover:text-white/80 drop-shadow-lg transition-colors hidden md:block" />
      </div>

      <div
        ref={containerRef}
        className={`flex flex-col sm:flex-row items-center justify-center h-full max-w-full overflow-auto ${isDragging ? 'cursor-grabbing' : 'cursor-grab'} ${(scaleMode === 'fit-width' || scaleMode === 'fit-screen') ? 'px-0 w-full' : 'px-8 sm:px-20'} select-none gap-0 ${doublePage ? 'drop-shadow-[0_20px_50px_rgba(0,0,0,0.9)]' : ''}`}
        onMouseDown={onMouseDown}
        onMouseLeave={onMouseLeave}
        onMouseUp={onMouseUp}
        onMouseMove={onMouseMove}
      >
        {getPagedImages(pages, currentPageIndex, doublePage, readDirection).map((page, index, spread) => {
          const isSpread = doublePage && spread.length > 1;
          const overlapStyle: CSSProperties = isSpread ? {
            marginLeft: index === 1 ? '-0.5px' : '0',
            marginRight: index === 0 ? '-0.5px' : '0',
            zIndex: index === 0 ? 1 : 0,
          } : {};

          return (
            <div
              key={page.number}
              className={`relative flex items-center justify-center ${doublePage ? 'max-w-none w-[50vw] sm:w-auto' : 'w-full flex-1 shrink-0'} ${scaleMode === 'fit-screen' || scaleMode === 'fit-height' ? 'h-full' : ''}`}
              style={overlapStyle}
            >
              {isPagedImageReady(page.number) ? (
                <img
                  src={cachedPageImageUrls[page.number]}
                  className={getScaleClasses(scaleMode, doublePage, !doublePage ? 'drop-shadow-2xl' : 'max-w-none')}
                  style={getFilterStyle(imageFilter)}
                  alt={`Page ${page.number}`}
                  draggable={false}
                />
              ) : (
                <div className="flex min-h-[40vh] min-w-[240px] items-center justify-center rounded-2xl border border-white/10 bg-black/30 px-10 py-16">
                  <Loader2 className="h-8 w-8 animate-spin text-komgaPrimary" />
                </div>
              )}
            </div>
          );
        })}
      </div>

      <div
        className="absolute right-0 inset-y-0 w-[20vw] sm:w-1/3 z-10 flex items-center justify-end sm:px-8 cursor-pointer md:hover:bg-white/5 transition opacity-0 md:hover:opacity-100 group"
        onClick={() => readDirection === 'ltr' ? onNext() : onPrev()}
      >
        <ChevronRight className="w-12 h-12 text-white/40 group-hover:text-white/80 drop-shadow-lg transition-colors hidden md:block" />
      </div>
    </div>
  );
}
