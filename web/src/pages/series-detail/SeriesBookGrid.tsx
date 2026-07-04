/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

import type { Book } from './types';
import { SeriesBookCard } from './SeriesBookCard';

interface SeriesBookGridProps {
  books: Book[];
  isSelectionMode: boolean;
  selectedBooks: number[];
  onCardClick: (book: Book) => void;
  onQuickToggleRead: (book: Book, makeRead: boolean) => void;
  onExportComicInfo: (book: Book) => void;
  onWriteComicInfo: (book: Book) => void;
  onCopyPath: (book: Book) => void;
}

export function SeriesBookGrid({
  books,
  isSelectionMode,
  selectedBooks,
  onCardClick,
  onQuickToggleRead,
  onExportComicInfo,
  onWriteComicInfo,
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
          onWriteComicInfo={onWriteComicInfo}
          onCopyPath={onCopyPath}
        />
      ))}
    </div>
  );
}
