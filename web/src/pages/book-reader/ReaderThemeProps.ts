import type { RefObject, Dispatch, SetStateAction } from 'react';
import type { Page, ReadMode, ReadDirection, ScaleMode, ImageFilter, ReaderImageFormat, ReadingBookmark, ReaderTheme } from './types';
import type { WebtoonReaderHandle } from './WebtoonReader';
import type { OfflineBookStatus } from './offlineReader';

export interface ReaderThemeProps {
  t: any;
  bookId: string | undefined;
  pages: Page[];
  currentPageIndex: number;
  
  cachedPageImageUrls: Record<number, string>;
  isPagedImageReady: (index: number) => boolean;
  getImageUrl: (bookId: string | undefined, pageNumber: number) => string;
  ensurePageImageLoaded: (targetBookId: string, pageNum: number) => Promise<string>;
  
  readMode: ReadMode;
  setReadMode: Dispatch<SetStateAction<ReadMode>>;
  readerTheme: ReaderTheme;
  setReaderTheme: Dispatch<SetStateAction<ReaderTheme>>;
  readDirection: ReadDirection;
  setReadDirection: Dispatch<SetStateAction<ReadDirection>>;
  doublePage: boolean;
  setDoublePage: Dispatch<SetStateAction<boolean>>;
  scaleMode: ScaleMode;
  setScaleMode: Dispatch<SetStateAction<ScaleMode>>;
  imageFilter: ImageFilter;
  setImageFilter: Dispatch<SetStateAction<ImageFilter>>;
  autoCrop: boolean;
  setAutoCrop: Dispatch<SetStateAction<boolean>>;
  preloadCount: number;
  setPreloadCount: Dispatch<SetStateAction<number>>;
  readerImageFormat: ReaderImageFormat;
  setReaderImageFormat: Dispatch<SetStateAction<ReaderImageFormat>>;
  readerImageQuality: number;
  setReaderImageQuality: Dispatch<SetStateAction<number>>;
  eyeProtection: boolean;
  setEyeProtection: Dispatch<SetStateAction<boolean>>;
  w2xScale: number;
  setW2xScale: Dispatch<SetStateAction<number>>;
  w2xNoise: number;
  setW2xNoise: Dispatch<SetStateAction<number>>;
  w2xFormat: string;
  setW2xFormat: Dispatch<SetStateAction<string>>;
  
  showSettings: boolean;
  setShowSettings: Dispatch<SetStateAction<boolean>>;
  showHelp: boolean;
  setShowHelp: Dispatch<SetStateAction<boolean>>;
  sliderValue: number;
  setSliderValue: (val: number) => void;
  hoverPage: number | null;
  setHoverPage: (val: number | null) => void;
  hoverX: number;
  setHoverX: (val: number) => void;

  loading: boolean;
  loadError: string | null;
  readerLoading: boolean;
  bookTitle: string;
  bookVolume: string;
  nextBookId: number | null;
  
  handleBackToSeries: () => void;
  handleOpenBook: (targetBookId: number) => void;
  toggleHelp: () => void;
  setCurrentPageIndex: (index: number) => void;
  
  handleNext: () => void;
  handlePrev: () => void;
  jumpToPage: (pageNumber: number) => void;

  containerRef: RefObject<HTMLDivElement | null>;
  isDragging: boolean;
  handleMouseDown: (e: any) => void;
  handleMouseLeave: () => void;
  handleMouseUp: () => void;
  handleMouseMove: (e: any) => void;

  immersive: {
    visible: boolean;
    toggle: () => void;
    show: () => void;
  };

  siblings: {
    prev: any;
    next: any;
    allInVolume: any[];
  };

  webtoonReaderRef: RefObject<WebtoonReaderHandle | null>;
  handleWebtoonRenderRangeChange: (start: number, end: number) => void;
  handleWebtoonRenderedImageCountChange: (count: number) => void;

  progressIndicator: { status: any };

  bookmarks: ReadingBookmark[];
  bookmarkNote: string;
  setBookmarkNote: (note: string) => void;
  savingBookmark: boolean;
  currentBookmark: ReadingBookmark | null;
  currentPageNumber: number;
  handleSaveBookmark: () => void;
  handleDeleteBookmark: (bookmark: ReadingBookmark) => void;

  isOnline: boolean;
  offlineSupported: boolean;
  offlineStatus: OfflineBookStatus | null;
  offlineCaching: boolean;
  offlineDeleting: boolean;
  offlineCachedPages: number;
  offlineQueuedPage: number | null;
  offlineCacheError: string | null;
  handleCacheBookOffline: () => void;
  handleDeleteOfflineBook: () => void;
}
