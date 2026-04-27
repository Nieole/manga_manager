export interface NullString {
  String: string;
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
  rating?: { Float64: number; Valid: boolean };
  cover_path?: NullString;
  tags_string?: string | null;
  volume_count: number;
  actual_book_count: number;
  read_count: number;
  total_pages: { Float64: number; Valid: boolean };
  is_favorite: boolean;
  recent_book_id?: number;
  last_read_at?: { Time: string; Valid: boolean };
  last_read_page?: { Int64: number; Valid: boolean };
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
