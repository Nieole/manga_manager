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


export interface TransferSummary { total: number; matched: number; missing: number; }
export interface SeriesTransferFacts { id: number; actual_book_count?: number; }
export interface ExternalTransferResponse { task_key?: string; missing_books?: number; existing_books?: number; series_count?: number; }
