import type { NullString, NullFloat64 } from '../../api/contracts';

/**
 * 单系列详情视图的系列形状：由 GET /api/series/{id}（getSeriesInfo）直接返回后端
 * `database.Series` 行，字段与该表列一致（library_id/path/publisher/status/language/
 * locked_fields/book_count 等）。
 * 注意：这与 library/types.ts 的 `Series`（列表/卡片聚合视图，含 volume_count/
 * read_count/is_favorite/external_* 等统计字段）是**不同接口的不同 DTO**，只是恰好同名，
 * 不可互换使用。
 */
export interface Series {
  id: number;
  name: string;
  library_id: number;
  path: string;
  title?: NullString;
  summary?: NullString;
  publisher?: NullString;
  status?: NullString;
  rating?: NullFloat64;
  language?: NullString;
  book_count: number;
  locked_fields: NullString;
  updated_at?: string;
}

export interface MetaTag {
  id: number;
  name: string;
}

export interface Author {
  id: number;
  name: string;
  role: string;
}

export interface SeriesLink {
  id: number;
  name: string;
  url: string;
}
