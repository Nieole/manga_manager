export interface NullString {
  String: string;
  Valid: boolean;
}

export interface NullFloat64 {
  Float64: number;
  Valid: boolean;
}

export interface Series {
  id: number;
  name: string;
  library_id: number;
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

export interface Book {
  id: number;
  name: string;
  library_id: number;
  volume: string;
  title?: NullString;
  summary?: NullString;
  page_count: number;
  last_read_page?: { Valid: boolean; Int64: number };
  cover_path?: NullString;
  updated_at?: string;
}

export interface SearchResult {
  Title: string;
  OriginalTitle: string;
  Summary: string;
  Publisher: string;
  Status?: string;
  CoverURL: string;
  Rating: number;
  Tags: string[];
  SourceID: number;
  ReleaseDate: string;
  VolumeCount: number;
}
