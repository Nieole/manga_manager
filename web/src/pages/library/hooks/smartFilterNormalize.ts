/**
 * 业务说明：本文件是资料库「智能筛选」远端记录的纯归一化逻辑，从 useSmartFilters 抽出。
 * 后端返回的智能筛选行可能缺省或使用不同类型（如数字 id），此处统一补齐默认值、强制 id 为字符串、
 * 兜底排序方向/每页数量/创建时间，保证前端消费的 SavedSmartFilter 形状稳定。
 * 抽成无网络依赖的纯函数后，这些兜底规则可被单元测试直接覆盖。
 */

import { DEFAULT_PAGE_SIZE, type SavedSmartFilter } from '../types';

export function normalizeRemoteSmartFilter(item: SavedSmartFilter): SavedSmartFilter {
  return {
    ...item,
    id: String(item.id),
    activeTag: item.activeTag ?? null,
    activeAuthor: item.activeAuthor ?? null,
    activeStatus: item.activeStatus ?? null,
    activeLetter: item.activeLetter ?? null,
    sortByField: item.sortByField || 'name',
    sortDir: item.sortDir || 'asc',
    pageSize: item.pageSize || DEFAULT_PAGE_SIZE,
    createdAt: item.createdAt || new Date().toISOString(),
  };
}
