import type { Book } from './types';
import { SeriesBookCard } from './SeriesBookCard';

interface SeriesBookGridProps {
  books: Book[];
  isSelectionMode: boolean;
  selectedBooks: number[];
  onCardClick: (book: Book) => void;
  onQuickToggleRead: (book: Book, makeRead: boolean) => void;
  onExportComicInfo: (book: Book) => void;
  onCopyPath: (book: Book) => void;
}

export function SeriesBookGrid({
  books,
  isSelectionMode,
  selectedBooks,
  onCardClick,
  onQuickToggleRead,
  onExportComicInfo,
  onCopyPath,
}: SeriesBookGridProps) {
  if (books.length === 0) return null;
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4 sm:gap-6">
      {books.map((book) => (
        <SeriesBookCard
          key={book.id}
          book={book}
          isSelectionMode={isSelectionMode}
          isSelected={selectedBooks.includes(book.id)}
          onCardClick={onCardClick}
          onQuickToggleRead={onQuickToggleRead}
          onExportComicInfo={onExportComicInfo}
          onCopyPath={onCopyPath}
        />
      ))}
    </div>
  );
}
