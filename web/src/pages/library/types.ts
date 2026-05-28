export interface NullString {
  String: string;
  Valid: boolean;
}

export interface NullInt64 {
  Int64: number;
  Valid: boolean;
}

export interface NullTime {
  Time: string;
  Valid: boolean;
}

export interface NullFloat64 {
  Float64: number;
  Valid: boolean;
}

export interface AIRecommendation {
  series_id: number;
  reason: string;
  title: string;
  cover_path: string;
}

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
