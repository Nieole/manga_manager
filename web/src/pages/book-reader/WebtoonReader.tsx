import type { ImageFilter, Page, ScaleMode } from './types';
import { getFilterStyle, getScaleClasses } from './helpers';

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

interface WebtoonReaderProps {
  t: Translate;
  bookId?: string;
  pages: Page[];
  cachedPageImageUrls: Record<number, string>;
  imageFilter: ImageFilter;
  scaleMode: ScaleMode;
  doublePage: boolean;
  nextBookId: number | null;
  getImageUrl: (bookId: string | undefined, pageNum: number) => string;
  onOpenNextBook: (bookId: number) => void;
}

export function WebtoonReader({
  t,
  bookId,
  pages,
  cachedPageImageUrls,
  imageFilter,
  scaleMode,
  doublePage,
  nextBookId,
  getImageUrl,
  onOpenNextBook,
}: WebtoonReaderProps) {
  return (
    <div className="flex flex-col items-center w-full bg-komgaDark relative h-full overflow-y-auto overflow-x-hidden">
      {pages.map((page) => (
        <img
          key={page.number}
          data-page-number={page.number}
          src={cachedPageImageUrls[page.number] || getImageUrl(bookId, page.number)}
          loading="lazy"
          decoding="async"
          style={getFilterStyle(imageFilter)}
          className={getScaleClasses(scaleMode, doublePage, 'bg-gray-900 min-h-[50vh] drop-shadow-lg max-w-[100vw]')}
          alt={`Page ${page.number}`}
        />
      ))}
      {nextBookId && (
        <button
          onClick={() => onOpenNextBook(nextBookId)}
          className="my-10 px-8 py-4 bg-komgaPrimary hover:bg-komgaPrimaryHover text-white font-bold rounded-xl shadow-2xl text-lg transition-all duration-300 hover:scale-105"
        >
          {t('reader.nextBook')}
        </button>
      )}
    </div>
  );
}
