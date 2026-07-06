/**
 * 业务说明：本文件是资料库筛选参数的纯逻辑层，从 useLibraryFilters 抽出，负责 URL query 与
 * 高级筛选模型（阅读状态/评分/进度/加入天数）之间的解析与序列化，以及排序方向、游标分页字段的判定。
 * 抽成无 React/router 依赖的叶子模块后，这些边界解析规则可被单元测试直接覆盖，防止后续重构静默改坏
 * URL 兼容性与筛选语义。
 */

import { DEFAULT_PAGE_SIZE } from '../types';

// AdvancedFilters 对齐智能合集的筛选维度：阅读状态 / 评分区间 / 进度区间 / 加入天数。
// null 表示该维度不筛选。收成单一对象以减少 hook 的 state 数量与穿透面。
export interface AdvancedFilters {
  readState: string | null; // 'unread' | 'reading' | 'completed' | null
  minRating: number | null; // 0–10
  maxRating: number | null;
  minProgress: number | null; // 0–100
  maxProgress: number | null;
  addedWithinDays: number | null;
}

export const EMPTY_ADVANCED_FILTERS: AdvancedFilters = {
  readState: null,
  minRating: null,
  maxRating: null,
  minProgress: null,
  maxProgress: null,
  addedWithinDays: null,
};

export function hasAdvancedFilters(a: AdvancedFilters): boolean {
  return (
    a.readState !== null ||
    a.minRating !== null ||
    a.maxRating !== null ||
    a.minProgress !== null ||
    a.maxProgress !== null ||
    a.addedWithinDays !== null
  );
}

export const VALID_SORT_DIRS = new Set(['asc', 'desc']);
// 需与后端 seriesSearchSort.supportsCursor() 保持一致。books/volumes/pages 为 NOT NULL 整数列，
// 方向匹配的 *_desc 索引 + (name,id) tie-break 使 keyset 前滚稳定；rating 可空、read 为每用户派生值，故不纳入。
const SUPPORTS_CURSOR_FIELDS = new Set(['name', 'updated', 'created', 'favorite', 'books', 'volumes', 'pages']);

export function supportsCursorPagination(field: string) {
  return SUPPORTS_CURSOR_FIELDS.has(field);
}

export function setOrDelete(params: URLSearchParams, key: string, value: string | null) {
  if (value && value !== '') {
    params.set(key, value);
  } else {
    params.delete(key);
  }
}

export function parseFiltersFromSearch(params: URLSearchParams): {
  activeTag: string | null;
  activeAuthor: string | null;
  activeStatus: string | null;
  activeLetter: string | null;
  sortByField: string;
  sortDir: string;
  keyword: string;
  advanced: AdvancedFilters;
  page: number;
  pageSize: number;
} | null {
  // 至少有一个 query 参数才认为 URL 携带了完整意图（避免覆盖服务端 settings）
  const hasAny = ['tag', 'author', 'status', 'letter', 'q', 'sort', 'dir', 'size', 'page', 'read', 'rmin', 'rmax', 'pmin', 'pmax', 'days'].some((k) =>
    params.has(k),
  );
  if (!hasAny) return null;
  const sizeRaw = parseInt(params.get('size') || '', 10);
  const pageRaw = parseInt(params.get('page') || '', 10);
  return {
    activeTag: params.get('tag') || null,
    activeAuthor: params.get('author') || null,
    activeStatus: params.get('status') || null,
    activeLetter: params.get('letter') || null,
    keyword: params.get('q') || '',
    advanced: {
      readState: parseReadStateParam(params.get('read')),
      minRating: parseNumberParam(params.get('rmin')),
      maxRating: parseNumberParam(params.get('rmax')),
      minProgress: parseNumberParam(params.get('pmin')),
      maxProgress: parseNumberParam(params.get('pmax')),
      addedWithinDays: parseNumberParam(params.get('days')),
    },
    sortByField: params.get('sort') || 'name',
    sortDir: VALID_SORT_DIRS.has(params.get('dir') || '') ? (params.get('dir') as string) : 'asc',
    pageSize: Number.isFinite(sizeRaw) && sizeRaw > 0 ? sizeRaw : DEFAULT_PAGE_SIZE,
    page: Number.isFinite(pageRaw) && pageRaw > 0 ? pageRaw : 1,
  };
}

export function parseReadStateParam(v: string | null): string | null {
  return v === 'unread' || v === 'reading' || v === 'completed' ? v : null;
}

export function parseNumberParam(v: string | null): number | null {
  if (v === null || v.trim() === '') return null;
  const n = Number(v);
  return Number.isFinite(n) ? n : null;
}
