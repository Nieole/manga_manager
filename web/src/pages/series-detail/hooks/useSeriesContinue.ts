/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

import type { Book, SeriesContinue } from '../types';

export interface ContinueCta {
  bookId: number;
  page: number;
  totalPages: number;
  volumeLabel?: string;
  bookLabel: string;
}

export function buildContinueCta(continueInfo: SeriesContinue | null, books: Book[]): ContinueCta | null {
  if (!continueInfo) return null;
  const bookId = continueInfo.next_unread_book_id || continueInfo.last_read_book_id;
  if (!bookId) return null;
  const book = books.find((b) => b.id === bookId);
  const page = continueInfo.last_read_page && continueInfo.last_read_book_id === bookId ? continueInfo.last_read_page : 0;
  const totalPages = book?.page_count ?? 0;
  return {
    bookId,
    page,
    totalPages,
    volumeLabel: book?.volume?.trim() || undefined,
    bookLabel: book?.title?.Valid && book.title.String ? book.title.String : book?.name || '',
  };
}

export function isFullyRead(continueInfo: SeriesContinue | null): boolean {
  if (!continueInfo) return false;
  return continueInfo.total_books > 0 && continueInfo.read_books >= continueInfo.total_books;
}
