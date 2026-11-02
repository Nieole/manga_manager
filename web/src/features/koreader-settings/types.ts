export interface KOReaderStatus {
  enabled: boolean;
  base_path: string;
  allow_registration: boolean;
  match_mode: string;
  path_ignore_extension: boolean;
  path_match_depth: number;
  account_count: number;
  enabled_account_count: number;
  latest_error?: string;
  stats: {
    configured: boolean;
    has_password: boolean;
    has_valid_sync_key: boolean;
    username: string;
    total_books: number;
    hashed_books: number;
    unmatched_progress_count: number;
    matched_progress_count: number;
    latest_sync_at?: { Time: string; Valid: boolean } | null;
  };
}

export interface KOReaderForm {
  enabled: boolean;
  base_path: string;
  allow_registration: boolean;
  match_mode: string;
  path_ignore_extension: boolean;
}

export interface KOReaderAccount {
  id: number;
  username: string;
  sync_key: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
  last_used_at?: string | null;
  latest_error?: string;
}

export interface KOReaderAccountForm {
  username: string;
}

export interface KOReaderUnmatchedItem {
  id: number;
  document: string;
  normalized_key: string;
  device: string;
  device_id: string;
  percentage: number;
  updated_at: string;
  suggestion: string;
}

export interface KOReaderDeviceDiagnostics {
  summary: {
    device_count: number;
    healthy_devices: number;
    attention_devices: number;
    total_records: number;
    matched_records: number;
    unmatched_records: number;
    conflict_count: number;
    error_conflict_count: number;
  };
  devices: KOReaderDeviceItem[];
  conflicts: KOReaderDeviceConflictItem[];
}

export interface KOReaderDeviceItem {
  key: string;
  username: string;
  device: string;
  device_id: string;
  health: 'ready' | 'needs_reconcile' | 'error' | string;
  total_records: number;
  matched_records: number;
  unmatched_records: number;
  latest_sync_at?: string;
  latest_document: string;
  latest_matched_by: string;
  latest_error?: string;
  match_methods: Array<{ method: string; count: number }>;
  suggestion: string;
}

export interface KOReaderDeviceConflictItem {
  // "<来源表>:<主键>" 形式的复合标识，只用于列表 key。这个列表是两张表的 UNION，
  // 两边的自增主键会重号，所以它**不是**任何接口的入参。
  id: string;
  source_table: string;
  // 只在这一行确实对应一条进度记录时才有；「重置进度」只能用它。
  progress_id?: number;
  type: string;
  severity: 'warning' | 'error' | string;
  username: string;
  device: string;
  device_id: string;
  document: string;
  normalized_key: string;
  book_id?: number;
  matched_by: string;
  status: string;
  message: string;
  percentage: number;
  updated_at: string;
  suggestion: string;
}
