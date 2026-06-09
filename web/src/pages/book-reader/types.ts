/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

export interface Page {
  number: number;
  width: number;
  height: number;
}

export type ReadMode = 'webtoon' | 'paged';
export type ReadDirection = 'ltr' | 'rtl';
export type ScaleMode = 'original' | 'fit-height' | 'fit-width' | 'fit-screen';
export type ReaderTheme = 'base' | 'comimi';
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
