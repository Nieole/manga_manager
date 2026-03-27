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
}

export interface NamedOption {
  name: string;
}
