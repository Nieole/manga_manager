/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { Infinity as InfinityIcon, ListOrdered } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import type { PaginationMode } from './types';

interface LibraryPaginationProps {
  paginationMode: PaginationMode;
  totalSeries: number;
  page: number;
  pageSize: number;
  pageCursorMap: Record<number, string>;
  supportsCursor: boolean;
  lastLoadedPage: number;
  onChangePageSize: (size: number) => void;
  onChangePage: (page: number) => void;
  onTogglePaginationMode: () => void;
  onResetCursor: () => void;
}

function getVisiblePageNumbers(page: number, totalPages: number) {
  const visibleCount = Math.min(5, totalPages);
  const currentPage = Math.max(1, Math.min(page, totalPages));
  let startPage = currentPage - Math.floor(visibleCount / 2);
  startPage = Math.max(1, Math.min(startPage, totalPages - visibleCount + 1));

  return Array.from({ length: visibleCount }, (_, index) => startPage + index);
}

export function LibraryPagination({
  paginationMode,
  totalSeries,
  page,
  pageSize,
  pageCursorMap,
  supportsCursor,
  lastLoadedPage,
  onChangePageSize,
  onChangePage,
  onTogglePaginationMode,
  onResetCursor,
}: LibraryPaginationProps) {
  const { t } = useI18n();
  const totalPages = Math.max(1, Math.ceil(totalSeries / pageSize));
  const visiblePageNumbers = getVisiblePageNumbers(page, totalPages);

  return (
    <div className="mt-12 mb-8 flex flex-col xl:flex-row items-center justify-between gap-6 border-t border-gray-800 pt-8">
      <div className="flex flex-wrap justify-center items-center gap-4 text-sm">
        <span className="text-gray-500">{t('home.pagination.totalSeries', { count: totalSeries })}</span>
        <div className="h-4 w-px bg-gray-800" />
        <div className="flex items-center gap-2 text-gray-400">
          {t('home.pagination.pageSize')}
          <select
            value={pageSize}
            onChange={(e) => onChangePageSize(Number(e.target.value))}
            className="bg-transparent border border-gray-700 text-white rounded-sm focus:ring-komgaPrimary focus:border-komgaPrimary px-1 py-0.5 outline-hidden transition-colors"
          >
            <option value={30}>30</option>
            <option value={50}>50</option>
            <option value={100}>100</option>
          </select>
        </div>
        <div className="h-4 w-px bg-gray-800" />
        <button
          onClick={onTogglePaginationMode}
          className="inline-flex items-center gap-1.5 rounded-md border border-gray-700 px-2 py-1 text-xs text-gray-300 hover:border-komgaPrimary hover:text-komgaPrimary transition-colors"
          title={paginationMode === 'paged' ? t('library.pagination.switchToInfinite') : t('library.pagination.switchToPaged')}
        >
          {paginationMode === 'paged' ? <InfinityIcon className="w-3.5 h-3.5" /> : <ListOrdered className="w-3.5 h-3.5" />}
          {paginationMode === 'paged' ? t('library.pagination.infinite') : t('library.pagination.paged')}
        </button>
        {paginationMode === 'paged' && (
          <>
            <div className="h-4 w-px bg-gray-800" />
            <span className="text-gray-500">{t('home.pagination.currentPage', { page, total: totalPages })}</span>
          </>
        )}
      </div>

      {paginationMode === 'paged' && (
        <div className="flex flex-wrap justify-center items-center gap-2">
          <button
            onClick={() => onChangePage(1)}
            disabled={page === 1}
            className="px-3 py-1.5 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
          >
            {t('home.pagination.first')}
          </button>
          <button
            onClick={() => onChangePage(Math.max(1, page - 1))}
            disabled={page === 1}
            className="px-3 py-1.5 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
          >
            {t('home.pagination.prev')}
          </button>
          <div className="flex items-center gap-1 mx-1 sm:mx-2 overflow-x-auto">
            {visiblePageNumbers.map((pNum) => {
              return (
                <button
                  key={`page-${pNum}`}
                  onClick={() => onChangePage(pNum)}
                  className={`w-8 h-8 sm:w-9 sm:h-9 shrink-0 flex items-center justify-center rounded-lg text-sm font-bold transition-all ${
                    page === pNum ? 'bg-komgaPrimary text-white shadow-md' : 'bg-transparent text-gray-400 hover:bg-white/5 hover:text-white'
                  }`}
                >
                  {pNum}
                </button>
              );
            })}
          </div>
          <button
            onClick={() => onChangePage(Math.min(totalPages, page + 1))}
            disabled={page >= totalPages || (supportsCursor && page + 1 > lastLoadedPage + 1 && !pageCursorMap[page + 1])}
            className="px-3 py-1.5 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
          >
            {t('home.pagination.next')}
          </button>
          <button
            onClick={() => {
              onResetCursor();
              onChangePage(totalPages);
            }}
            disabled={page >= totalPages}
            className="px-3 py-1.5 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
          >
            {t('home.pagination.last')}
          </button>
          <div className="hidden sm:flex items-center gap-2 ml-2 pl-4 border-l border-gray-800 text-sm text-gray-500">
            {t('home.pagination.jumpTo')}
            <input
              type="number"
              min={1}
              max={totalPages}
              className="w-14 select-text bg-gray-900 border border-gray-800 rounded-lg text-white text-center py-1 focus:border-komgaPrimary outline-hidden placeholder:text-gray-700"
              placeholder={page.toString()}
              onMouseDown={(e) => e.stopPropagation()}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  const val = parseInt(e.currentTarget.value, 10);
                  if (val > 0 && val <= totalPages) {
                    onResetCursor();
                    onChangePage(val);
                  }
                  e.currentTarget.value = '';
                }
              }}
            />
            {t('home.pagination.page')}
          </div>
        </div>
      )}
    </div>
  );
}
