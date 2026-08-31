/**
 * 本文件是资料库「智能视图」记录的纯逻辑层，从 useSmartFilters 抽出，负责 SavedSmartFilter 与它两侧
 * 形状之间的转换：归一化后端/本地缓存读来的行、拼写回后端的请求体、拼应用视图时落回筛选状态的快照。
 * 三处都要覆盖 SavedSmartFilter 的全部维度——漏掉一维就等于视图被静默改成了另一个视图，
 * 所以它们收在同一个文件里，新增维度时一处都不会被落下。
 * 抽成无网络/无 React 依赖的纯函数后，这些兜底与字段映射规则可被单元测试直接覆盖。
 */

import { DEFAULT_PAGE_SIZE, type SavedSmartFilter } from '../types';
import type { AdvancedFilters } from './libraryFilterParams';
import type { SavedLibrarySettings } from './useLibraryFilters';

export function normalizeRemoteSmartFilter(item: SavedSmartFilter): SavedSmartFilter {
  return {
    ...item,
    id: String(item.id),
    activeTag: item.activeTag ?? null,
    activeAuthor: item.activeAuthor ?? null,
    activeStatus: item.activeStatus ?? null,
    activeLetter: item.activeLetter ?? null,
    readState: item.readState ?? null,
    minRating: item.minRating ?? null,
    maxRating: item.maxRating ?? null,
    minProgress: item.minProgress ?? null,
    maxProgress: item.maxProgress ?? null,
    addedWithinDays: item.addedWithinDays ?? null,
    sortByField: item.sortByField || 'name',
    sortDir: item.sortDir || 'asc',
    pageSize: item.pageSize || DEFAULT_PAGE_SIZE,
    createdAt: item.createdAt || new Date().toISOString(),
  };
}

/** 写回后端 UpsertSmartFilterRequest 的请求体：除 id 与 createdAt 外的全部维度。 */
export function smartFilterRequestBody(filter: SavedSmartFilter) {
  return {
    name: filter.name,
    activeTag: filter.activeTag ?? null,
    activeAuthor: filter.activeAuthor ?? null,
    activeStatus: filter.activeStatus ?? null,
    activeLetter: filter.activeLetter ?? null,
    readState: filter.readState ?? null,
    minRating: filter.minRating ?? null,
    maxRating: filter.maxRating ?? null,
    minProgress: filter.minProgress ?? null,
    maxProgress: filter.maxProgress ?? null,
    addedWithinDays: filter.addedWithinDays ?? null,
    sortByField: filter.sortByField,
    sortDir: filter.sortDir,
    pageSize: filter.pageSize,
  };
}

/** 把视图平铺的六个高级筛选维度收回 AdvancedFilters 形状；缺省一律当作「该维度不筛选」。 */
export function advancedFromSmartFilter(filter: SavedSmartFilter): AdvancedFilters {
  return {
    readState: filter.readState ?? null,
    minRating: filter.minRating ?? null,
    maxRating: filter.maxRating ?? null,
    minProgress: filter.minProgress ?? null,
    maxProgress: filter.maxProgress ?? null,
    addedWithinDays: filter.addedWithinDays ?? null,
  };
}

/**
 * 应用视图时落回 useLibraryFilters 的快照。
 * 关键字不属于视图条件，一律清空，避免「视图条件 + 临时搜索词」叠加后让用户误以为视图失效。
 */
export function smartFilterToSnapshot(filter: SavedSmartFilter): Partial<SavedLibrarySettings> {
  return {
    activeTag: filter.activeTag,
    activeAuthor: filter.activeAuthor,
    activeStatus: filter.activeStatus,
    activeLetter: filter.activeLetter,
    keyword: '',
    advanced: advancedFromSmartFilter(filter),
    sortByField: filter.sortByField,
    sortDir: filter.sortDir,
    pageSize: filter.pageSize,
  };
}
