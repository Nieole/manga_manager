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
