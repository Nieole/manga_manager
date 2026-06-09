/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

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
  nextBookLabel?: string | null;
  getImageUrl: (bookId: string | undefined, pageNum: number) => string;
  onVisiblePageChange: (pageIndex: number) => void;
  onRenderRangeChange: (startIndex: number, endIndex: number) => void;
  onRenderedImageCountChange: (count: number) => void;
  onOpenNextBook: (bookId: number) => void;
  onCenterTap?: () => void;
}

export interface WebtoonReaderHandle {
  scrollToPage: (pageNumber: number) => void;
}

const TAP_MAX_MOVE = 8;
const TAP_MAX_DURATION = 400;

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
  nextBookLabel,
  getImageUrl,
  onVisiblePageChange,
  onRenderRangeChange,
  onRenderedImageCountChange,
  onOpenNextBook,
  onCenterTap,
}, ref) {
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const tapStateRef = useRef<{ x: number; y: number; t: number } | null>(null);
  const itemCount = pages.length + (nextBookId ? 1 : 0);

  useImperativeHandle(ref, () => ({
    scrollToPage(pageNumber: number) {
      const index = Math.max(0, Math.min(pages.length - 1, pageNumber - 1));
      virtuosoRef.current?.scrollToIndex({ index, align: 'start', behavior: 'auto' });
    },
  }), [pages.length]);

  const handlePointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!onCenterTap) return;
    tapStateRef.current = { x: event.clientX, y: event.clientY, t: performance.now() };
  };
  const handlePointerUp = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!onCenterTap || !tapStateRef.current) return;
    const start = tapStateRef.current;
    tapStateRef.current = null;
    const dx = Math.abs(event.clientX - start.x);
    const dy = Math.abs(event.clientY - start.y);
    if (dx < TAP_MAX_MOVE && dy < TAP_MAX_MOVE && performance.now() - start.t < TAP_MAX_DURATION) {
      const target = event.target as HTMLElement | null;
      if (target?.closest('button, a, input, textarea, select')) return;
      onCenterTap();
    }
  };

  return (
    <div
      className="relative w-full h-full"
      onPointerDown={handlePointerDown}
      onPointerUp={handlePointerUp}
    >
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
                  {nextBookLabel
                    ? t('reader.nextBookNamed', { name: nextBookLabel })
                    : t('reader.nextBook')}
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
    </div>
  );
});
