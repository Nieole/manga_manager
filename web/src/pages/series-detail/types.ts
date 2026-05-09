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

export interface SeriesRelation {
  id: number;
  target_series_id: number;
  target_series_name: string;
  relation_type: string;
}

export interface SeriesRelationCandidate {
  id: number;
  name: string;
  title?: NullString;
  cover_path?: NullString;
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
  SourceURL?: string;
  Provider?: string;
  Confidence?: number;
  ReleaseDate: string;
  VolumeCount: number;
}

export interface MetadataReviewField {
  name: string;
  label: string;
  current: string;
  proposed: string;
  confidence: number;
  locked: boolean;
  source: string;
  source_url: string;
  status: string;
}

export interface MetadataReview {
  id: number;
  series_id: number;
  provider: string;
  source_url: string;
  source_id: number;
  source_query: string;
  summary: string;
  confidence: number;
  status: string;
  raw_payload: string;
  created_at: string;
  updated_at: string;
  applied_at?: string;
  rejected_at?: string;
  fields: MetadataReviewField[];
}

export interface MetadataReviewInboxItem extends MetadataReview {
  library_id: number;
  library_name: string;
  series_name: string;
  series_title: string;
  cover_book_id: number;
  field_count: number;
  locked_field_count: number;
}

export interface MetadataReviewInboxResponse {
  items: MetadataReviewInboxItem[];
  total: number;
  limit: number;
  offset: number;
}

export interface MetadataProvenance {
  field_name: string;
  label: string;
  value: string;
  source: string;
  source_url: string;
  confidence: number;
  updated_at: string;
}

export interface MetadataReviewResponse {
  reviews: MetadataReview[];
  provenance: MetadataProvenance[];
}
