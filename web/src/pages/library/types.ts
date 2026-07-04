/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

// Null* 契约原语统一收敛到 api/contracts.ts（单一来源），此处再导出以保持既有 import 路径不变。
export type { NullString, NullInt64, NullTime, NullFloat64 } from '../../api/contracts';
import type { NullString, NullInt64, NullTime, NullFloat64 } from '../../api/contracts';

export interface AIRecommendation {
  series_id: number;
  reason: string;
  title: string;
  cover_path: string;
}

/**
 * 资料库列表/卡片视图的系列形状：来自系列分页/搜索接口（基于 series_stats 聚合），
 * 携带封面、卷数、已读数、收藏、外部同步等展示与统计字段。
 * 注意：这与 series-detail/types.ts 的 `Series`（单系列详情行，含 library_id/path/
 * publisher/status/language/locked_fields）是**不同接口的不同 DTO**，只是恰好同名，
 * 不可互换使用。
 */
export interface Series {
  id: number;
  name: string;
  title?: NullString;
  summary?: NullString;
  rating?: NullFloat64;
  cover_path?: NullString;
  tags_string?: string | null;
  volume_count: number;
  actual_book_count: number;
  read_count: number;
  total_pages: NullFloat64;
  is_favorite: boolean;
  recent_book_id?: number;
  last_read_at?: NullTime;
  last_read_page?: NullInt64;
  last_read_book_id?: NullInt64;
  updated_at?: string;
  external_match_count?: number;
  external_total_count?: number;
  external_sync_status?: 'missing' | 'partial' | 'complete';
}

export interface NamedOption {
  name: string;
}

export interface SavedSmartFilter {
  id: string;
  name: string;
  activeTag: string | null;
  activeAuthor: string | null;
  activeStatus: string | null;
  activeLetter: string | null;
  sortByField: string;
  sortDir: string;
  pageSize: number;
  createdAt: string;
}

export interface SeriesSearchResponse {
  items?: Series[];
  total?: number;
  page?: number;
  limit?: number;
  next_cursor?: string;
  has_more?: boolean;
}

export interface ExternalSession {
  session_id: string;
  library_id: number;
  external_path: string;
  ignore_extension: boolean;
  status: 'scanning' | 'ready' | 'failed';
  error?: string;
  scanned_files: number;
  matched_books: number;
  unmatched_files: number;
  total_books: number;
  created_at: string;
  updated_at: string;
}

export interface ExternalSeriesStatus {
  series_id: number;
  series_name?: string;
  external_match_count: number;
  external_total_count: number;
  external_sync_status: 'missing' | 'partial' | 'complete';
}

export interface ExternalSessionCreateResponse {
  session: ExternalSession;
  task_key: string;
}

export const DEFAULT_PAGE_SIZE = 30;
export const PAGINATION_MODE_KEY_PREFIX = 'lib_pagination_mode_';
export type PaginationMode = 'paged' | 'infinite';
