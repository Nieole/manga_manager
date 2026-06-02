import { SkipBack, SkipForward } from 'lucide-react';
import type { SiblingBook } from './useReaderSiblings';

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

interface ReaderProgressTrayProps {
  t: Translate;
  readDirection: string;
  currentPageIndex: number;
  pageCount: number;
  sliderValue: number;
  hoverPage: number | null;
  hoverX: number;
  onSliderChange: (value: number) => void;
  onHoverPageChange: (page: number | null) => void;
  onHoverXChange: (x: number) => void;
  onCommitPage: (page: number) => void;
  prev: SiblingBook | null;
  next: SiblingBook | null;
  onOpenBook: (bookId: number) => void;
}

export function ReaderProgressTray({
  t,
  readDirection,
  currentPageIndex,
  pageCount,
  sliderValue,
  hoverPage,
  hoverX,
  onSliderChange,
  onHoverPageChange,
  onHoverXChange,
  onCommitPage,
  prev,
  next,
  onOpenBook,
}: ReaderProgressTrayProps) {
  if (pageCount <= 0) return null;

  const handlePointerPreview = (target: HTMLInputElement, clientX: number) => {
    const rect = target.getBoundingClientRect();
    const x = clientX - rect.left;
    const percent = x / rect.width;
    const page = Math.round(percent * (pageCount - 1)) + 1;
    onHoverPageChange(Math.max(1, Math.min(pageCount, page)));
    onHoverXChange(x);
  };

  const commitRangeValue = (target: HTMLInputElement) => {
    onCommitPage(parseInt(target.value, 10));
  };

  const isRtl = readDirection === 'rtl';
  const leftSibl = isRtl ? next : prev;
  const leftLabel = isRtl ? t('reader.siblings.next') : t('reader.siblings.prev');
  const rightSibl = isRtl ? prev : next;
  const rightLabel = isRtl ? t('reader.siblings.prev') : t('reader.siblings.next');

  return (
    <div className="bg-linear-to-t from-komgaDark/90 via-komgaDark/45 to-transparent pb-8 pt-16 px-4 sm:px-8 flex flex-col items-center pointer-events-none">
      <div className="w-full max-w-4xl flex items-center justify-center gap-2 sm:gap-4 pointer-events-auto">
        
        {/* Left Book Button */}
        <div className="shrink-0 w-12 sm:w-48 flex justify-end">
          {leftSibl ? (
            <button
              onClick={() => onOpenBook(leftSibl.id)}
              title={`${leftLabel}: ${leftSibl.title}`}
              className="group flex items-center gap-2 bg-komgaDark/70 hover:bg-komgaPrimary/20 hover:border-komgaPrimary/40 transition-all border border-white/10 rounded-full sm:rounded-2xl px-0 sm:px-4 py-3 sm:py-2.5 backdrop-blur-sm shadow-xl text-white w-12 sm:w-auto h-12 sm:h-auto justify-center"
            >
              <SkipBack className="w-5 h-5 shrink-0 text-gray-300 group-hover:text-komgaPrimary transition-colors" />
              <span className="hidden sm:block text-xs font-medium truncate text-gray-200 group-hover:text-white max-w-[120px]">
                {leftSibl.title}
              </span>
            </button>
          ) : (
            <div className="w-12 h-12 sm:w-auto sm:h-auto px-0 sm:px-4 py-3 sm:py-2.5 rounded-full sm:rounded-2xl border border-transparent flex items-center justify-center">
              <SkipBack className="w-5 h-5 text-gray-600/50" />
            </div>
          )}
        </div>

        {/* Progress Slider */}
        <div className="flex-1 flex items-center gap-3 sm:gap-4 bg-komgaDark/70 px-4 sm:px-6 py-3 rounded-2xl backdrop-blur-sm border border-white/10 shadow-2xl min-w-[200px]">
          <span className="text-white font-medium text-xs sm:text-sm whitespace-nowrap w-6 sm:w-8 text-right drop-shadow-md">{currentPageIndex + 1}</span>
          <div className="flex-1 relative h-6 flex items-center group/slider">
            {hoverPage !== null && (
              <div
                className="absolute bottom-full mb-3 bg-komgaPrimary text-white text-[10px] font-bold py-1 px-2 rounded-md shadow-[0_0_15px_rgba(168,85,247,0.4)] pointer-events-none transform -translate-x-1/2 whitespace-nowrap z-30 animate-in fade-in zoom-in-95 duration-150"
                style={{ left: `${hoverX}px` }}
              >
                {t('reader.pagePreview', { page: hoverPage })}
                <div className="absolute top-full left-1/2 -translate-x-1/2 border-x-4 border-x-transparent border-t-4 border-t-komgaPrimary"></div>
              </div>
            )}
            <input
              type="range"
              min={1}
              max={pageCount}
              value={sliderValue}
              onChange={(e) => onSliderChange(parseInt(e.target.value, 10))}
              onMouseMove={(e) => handlePointerPreview(e.currentTarget, e.clientX)}
              onMouseLeave={() => onHoverPageChange(null)}
              onMouseUp={(e) => commitRangeValue(e.currentTarget)}
              onTouchEnd={(e) => commitRangeValue(e.currentTarget)}
              className="w-full accent-komgaPrimary h-1.5 bg-gray-700/50 rounded-lg appearance-none cursor-pointer"
            />
          </div>
          <span className="text-gray-400 font-medium text-xs sm:text-sm whitespace-nowrap w-6 sm:w-8 drop-shadow-md">{pageCount}</span>
        </div>

        {/* Right Book Button */}
        <div className="shrink-0 w-12 sm:w-48 flex justify-start">
          {rightSibl ? (
            <button
              onClick={() => onOpenBook(rightSibl.id)}
              title={`${rightLabel}: ${rightSibl.title}`}
              className="group flex items-center gap-2 bg-komgaDark/70 hover:bg-komgaPrimary/20 hover:border-komgaPrimary/40 transition-all border border-white/10 rounded-full sm:rounded-2xl px-0 sm:px-4 py-3 sm:py-2.5 backdrop-blur-sm shadow-xl text-white w-12 sm:w-auto h-12 sm:h-auto justify-center"
            >
              <span className="hidden sm:block text-xs font-medium truncate text-gray-200 group-hover:text-white max-w-[120px]">
                {rightSibl.title}
              </span>
              <SkipForward className="w-5 h-5 shrink-0 text-gray-300 group-hover:text-komgaPrimary transition-colors" />
            </button>
          ) : (
            <div className="w-12 h-12 sm:w-auto sm:h-auto px-0 sm:px-4 py-3 sm:py-2.5 rounded-full sm:rounded-2xl border border-transparent flex items-center justify-center">
              <SkipForward className="w-5 h-5 text-gray-600/50" />
            </div>
          )}
        </div>

      </div>
    </div>
  );
}
