export interface Library {
  id: string;
  name: string;
  path: string;
  auto_scan?: boolean;
  koreader_sync_enabled?: boolean;
  scan_interval?: number;
  scan_formats?: string;
}

export interface SearchHit {
  id: string;
  score?: number;
  fields?: {
    id?: string;
    title?: string;
    series_name?: string;
    type?: string;
    cover_path?: string;
  };
}

export interface BrowseDirEntry {
  name: string;
  path: string;
}

export interface BrowseDrive {
  name: string;
  path: string;
}
