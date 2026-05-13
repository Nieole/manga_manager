export interface Page {
  number: number;
  width: number;
  height: number;
}

export type ReadMode = 'webtoon' | 'paged';
export type ReadDirection = 'ltr' | 'rtl';
export type ScaleMode = 'original' | 'fit-height' | 'fit-width' | 'fit-screen';
export type ReaderImageFormat = 'original' | 'webp' | 'jpeg';
export type ImageFilter =
  | 'none'
  | 'nearest'
  | 'average'
  | 'bilinear'
  | 'bicubic'
  | 'lanczos3'
  | 'waifu2x'
  | 'realcugan'
  | 'mitchell'
  | 'lanczos2'
  | 'bspline'
  | 'catmullrom';

export interface NullableText {
  Valid?: boolean;
  String?: string;
}

export interface NullableInt {
  Valid?: boolean;
  Int64?: number;
}

export interface ReaderBookInfo {
  id?: number;
  name: string;
  title?: NullableText;
  volume?: string;
  series_id?: number;
  last_read_page?: NullableInt;
}

export interface ReadingBookmark {
  id: number;
  book_id: number;
  page: number;
  note: string;
  created_at: string;
  updated_at: string;
}
