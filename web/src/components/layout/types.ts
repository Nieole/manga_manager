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

export type { Library, BrowseDirEntry, BrowseDrive } from '../../features/libraries/types';
