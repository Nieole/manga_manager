/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import { useRef, type CSSProperties, type MouseEvent, type RefObject } from 'react';
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
  onCenterTap?: () => void;
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
  onCenterTap,
  onMouseDown,
  onMouseLeave,
  onMouseUp,
  onMouseMove,
}: PagedReaderProps) {
  const tapStateRef = useRef<{ x: number; y: number; t: number } | null>(null);

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
    if (dx < 8 && dy < 8 && performance.now() - start.t < 400) {
      const screenWidth = window.innerWidth;
      const x = event.clientX;
      if (x < screenWidth * 0.3) {
        if (readDirection === 'ltr') onPrev(); else onNext();
      } else if (x > screenWidth * 0.7) {
        if (readDirection === 'ltr') onNext(); else onPrev();
      } else {
        onCenterTap();
      }
    }
  };

  return (
    <div className="flex items-center justify-center w-full h-full bg-komgaDark relative overflow-hidden group">
      {/* Desktop hover indicators for left/right */}
      <div className="absolute left-0 inset-y-0 w-[30%] z-10 flex items-center justify-start px-8 pointer-events-none opacity-0 group-hover:opacity-100 transition-opacity">
        <ChevronLeft className="w-12 h-12 text-white/40 drop-shadow-lg hidden md:block" />
      </div>
      <div className="absolute right-0 inset-y-0 w-[30%] z-10 flex items-center justify-end px-8 pointer-events-none opacity-0 group-hover:opacity-100 transition-opacity">
        <ChevronRight className="w-12 h-12 text-white/40 drop-shadow-lg hidden md:block" />
      </div>

      <div
        ref={containerRef}
        className={`flex flex-col sm:flex-row items-center justify-center h-full max-w-full overflow-auto touch-none ${isDragging ? 'cursor-grabbing' : 'cursor-grab'} ${(scaleMode === 'fit-width' || scaleMode === 'fit-screen') ? 'px-0 w-full' : 'px-8 sm:px-20'} select-none gap-0 ${doublePage ? 'drop-shadow-[0_20px_50px_rgba(0,0,0,0.9)]' : ''}`}
        onPointerDown={(e) => {
          handlePointerDown(e);
          onMouseDown(e);
        }}
        onPointerMove={onMouseMove}
        onPointerUp={(e) => {
          handlePointerUp(e);
          onMouseUp();
        }}
        onPointerLeave={() => {
          onMouseLeave();
          onMouseUp();
        }}
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
    </div>
  );
}
