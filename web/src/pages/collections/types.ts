/**
 * 本文件是前端合集页面的共享类型定义，描述手工合集与智能合集视图、其系列成员、
 * 智能合集快照预览等前后端契约形状，供合集页各子组件复用。
 * 维护时应保持字段与后端 /api/collection-views、/api/collections、/api/smart-filters 响应一致。
 */

export interface Collection {
  view_id: string;
  kind: 'collection' | 'smart';
  id: number;
  numeric_id: number;
  name: string;
  description: string;
  series_count: number;
  source_type: string;
  source_review_id?: number;
  created_at: string;
  library_id?: number;
  activeTag?: string | null;
  activeAuthor?: string | null;
  activeStatus?: string | null;
  activeLetter?: string | null;
  readState?: string | null;
  minRating?: number | null;
  maxRating?: number | null;
  minProgress?: number | null;
  maxProgress?: number | null;
  addedWithinDays?: number | null;
  sortByField?: string;
  sortDir?: string;
  pageSize?: number;
}

export interface CollectionSeriesItem {
  series_id: number;
  series_name: string;
  cover_path: { String: string; Valid: boolean };
  book_count: number;
}

export interface SmartCollectionSeriesItem {
  id: number;
  name: string;
  title?: { String: string; Valid: boolean };
  cover_path?: { String: string; Valid: boolean };
  actual_book_count?: number;
  book_count?: number;
}

export interface SmartCollectionSeriesResponse {
  items: SmartCollectionSeriesItem[];
  // total 是命中总数，items 只是其中一页；limit/offset 是这一页的实际口径，回显供继续往后取。
  total: number;
  limit: number;
  offset: number;
  filter: Collection;
}

export interface SmartCollectionSnapshotPreview {
  items: SmartCollectionSeriesItem[];
  total: number;
  preview_limit: number;
  snapshot_limit: number;
  snapshot_count: number;
  truncated: boolean;
  name_conflict: boolean;
}

// 合集页各子组件共用的翻译函数签名（LocaleProvider 的 t 兼容此形状）。
export type TFunc = (key: string, vars?: Record<string, unknown>) => string;

// 作品群合集在后端 collections.source_type 里的取值。
export const FRANCHISE_SOURCE_TYPE = 'system_franchise';

// 作品群由系列关系推导而来，只能看不能改：写入端点一律回 403，界面上就不该给出编辑入口。
// 它仍然列在合集页里——「这个系列的相关作品」是有价值的浏览视角，隐藏掉等于丢功能。
export function isReadOnlyCollection(c: Pick<Collection, 'source_type'> | null | undefined): boolean {
  return c?.source_type === FRANCHISE_SOURCE_TYPE;
}
