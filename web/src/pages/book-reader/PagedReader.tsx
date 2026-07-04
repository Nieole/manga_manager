/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import { useCallback, type CSSProperties, type MouseEvent, type RefObject } from 'react';
import { ChevronLeft, ChevronRight, Loader2 } from 'lucide-react';
import { getFilterStyle, getPagedImages, getScaleClasses } from './helpers';
import { useReaderZoom } from './useReaderZoom';
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
  // 单击（经缩放 hook 的单/双击消歧后）落到屏幕左/右三分之一翻页、中央切换 UI；方向随 LTR/RTL 反转。
  const handleSingleTap = useCallback(
    (clientX: number) => {
      const screenWidth = window.innerWidth;
      if (clientX < screenWidth * 0.3) {
        if (readDirection === 'ltr') onPrev(); else onNext();
      } else if (clientX > screenWidth * 0.7) {
        if (readDirection === 'ltr') onNext(); else onPrev();
      } else {
        onCenterTap?.();
      }
    },
    [readDirection, onPrev, onNext, onCenterTap],
  );

  // 自由缩放（捏合 / 双击 / Ctrl+滚轮 / 拖拽）；resetKey 随翻页/双页切换复位到 1x；单击交还 handleSingleTap。
  const zoom = useReaderZoom(containerRef, `${currentPageIndex}:${doublePage}`, handleSingleTap);

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
        className={`flex items-center justify-center h-full w-full max-w-full ${zoom.isZoomed ? 'overflow-hidden' : 'overflow-auto'} touch-none ${isDragging || zoom.isZoomed ? 'cursor-grabbing' : 'cursor-grab'} ${(scaleMode === 'fit-width' || scaleMode === 'fit-screen') ? 'px-0' : 'px-8 sm:px-20'} select-none`}
        onPointerDown={(e) => {
          if (!zoom.onPointerDown(e)) onMouseDown(e);
        }}
        onPointerMove={(e) => {
          if (!zoom.onPointerMove(e)) onMouseMove(e);
        }}
        onPointerUp={(e) => {
          zoom.onPointerUp(e);
          onMouseUp();
        }}
        onPointerLeave={() => {
          onMouseLeave();
          onMouseUp();
        }}
      >
        <div
          className={`flex flex-col sm:flex-row items-center justify-center h-full w-full gap-0 ${doublePage ? 'drop-shadow-[0_20px_50px_rgba(0,0,0,0.9)]' : ''}`}
          style={zoom.wrapperStyle}
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
    </div>
  );
}
