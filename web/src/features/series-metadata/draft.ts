/** Undefined fields are absent; empty strings, zero and empty arrays explicitly clear values. */
export interface SeriesEditForm {
  id?: number;
  title?: string;
  summary?: string;
  publisher?: string;
  status?: string;
  rating?: number;
  language?: string;
  tagsInput?: string[];
  authorsInput?: { name: string; role: string }[];
  linksInput?: { name: string; url: string }[];
}

export type SeriesFormField = 'title' | 'summary' | 'publisher' | 'status' | 'rating' | 'language' | 'tagsInput' | 'authorsInput' | 'linksInput';
export type SeriesFormValue = string | number | string[] | { name: string; role: string }[] | { name: string; url: string }[];
