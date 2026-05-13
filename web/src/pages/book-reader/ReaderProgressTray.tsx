type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

interface ReaderProgressTrayProps {
  t: Translate;
  showSettings: boolean;
  currentPageIndex: number;
  pageCount: number;
  sliderValue: number;
  hoverPage: number | null;
  hoverX: number;
  onSliderChange: (value: number) => void;
  onHoverPageChange: (page: number | null) => void;
  onHoverXChange: (x: number) => void;
  onCommitPage: (page: number) => void;
}

export function ReaderProgressTray({
  t,
  showSettings,
  currentPageIndex,
  pageCount,
  sliderValue,
  hoverPage,
  hoverX,
  onSliderChange,
  onHoverPageChange,
  onHoverXChange,
  onCommitPage,
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

  return (
    <div className={`absolute bottom-0 inset-x-0 bg-gradient-to-t from-komgaDark/90 via-komgaDark/45 to-transparent pb-8 pt-16 px-6 sm:px-12 flex flex-col items-center transition-all duration-300 z-20 ${showSettings ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-4 hover:translate-y-0 hover:opacity-100'}`}>
      <div className="w-full max-w-2xl flex items-center gap-4 bg-komgaDark/70 px-6 py-3 rounded-2xl backdrop-blur border border-white/10 shadow-2xl">
        <span className="text-white font-medium text-sm whitespace-nowrap w-8 text-right drop-shadow-md">{currentPageIndex + 1}</span>
        <div className="flex-1 relative h-6 flex items-center group/slider">
          {hoverPage !== null && (
            <div
              className="absolute bottom-full mb-3 bg-komgaPrimary text-white text-[10px] font-bold py-1 px-2 rounded-md shadow-[0_0_15px_rgba(168,85,247,0.4)] pointer-events-none transform -translate-x-1/2 whitespace-nowrap z-30 animate-in fade-in zoom-in-95 duration-150"
              style={{ left: `${hoverX}px` }}
            >
              {t('reader.pagePreview', { page: hoverPage })}
              <div className="absolute top-full left-1/2 -translate-x-1/2 border-x-[4px] border-x-transparent border-t-[4px] border-t-komgaPrimary"></div>
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
        <span className="text-gray-400 font-medium text-sm whitespace-nowrap w-8 drop-shadow-md">{pageCount}</span>
      </div>
    </div>
  );
}
