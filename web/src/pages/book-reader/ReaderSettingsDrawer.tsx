import { useEffect, useMemo, useState, type Dispatch, type SetStateAction } from 'react';
import { BookmarkPanel } from './BookmarkPanel';
import { OfflineReadingPanel } from './OfflineReadingPanel';
import type { OfflineBookStatus } from './offlineReader';
import type { ImageFilter, ReaderImageFormat, ReadDirection, ReadingBookmark, ReadMode, ScaleMode } from './types';

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;
type ReaderSettingsTab = 'reading' | 'image' | 'cache' | 'bookmarks';
type ReaderSettingsMode = 'global' | 'book';

const SETTINGS_MODE_STORAGE_KEY = 'manga-reader:settings-mode';
const GLOBAL_TABS: ReaderSettingsTab[] = ['reading', 'image'];
const BOOK_TABS: ReaderSettingsTab[] = ['cache', 'bookmarks'];

interface ReaderSettingsDrawerProps {
  t: Translate;
  readMode: ReadMode;
  setReadMode: Dispatch<SetStateAction<ReadMode>>;
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
  isOnline: boolean;
  offlineSupported: boolean;
  offlineStatus: OfflineBookStatus | null;
  offlineCaching: boolean;
  offlineDeleting: boolean;
  offlineCachedPages: number;
  activePageCount: number;
  offlineQueuedPage: number | null;
  offlineCacheError: string | null;
  readerLoading: boolean;
  onCacheBook: () => void;
  onDeleteOfflineBook: () => void;
  bookmarks: ReadingBookmark[];
  bookmarkNote: string;
  savingBookmark: boolean;
  loading: boolean;
  currentBookmark: ReadingBookmark | null;
  currentPageNumber: number;
  onBookmarkNoteChange: (note: string) => void;
  onSaveBookmark: () => void;
  onDeleteBookmark: (bookmark: ReadingBookmark) => void;
  onJumpToPage: (page: number) => void;
}

export function ReaderSettingsDrawer({
  t,
  readMode,
  setReadMode,
  readDirection,
  setReadDirection,
  doublePage,
  setDoublePage,
  scaleMode,
  setScaleMode,
  imageFilter,
  setImageFilter,
  autoCrop,
  setAutoCrop,
  preloadCount,
  setPreloadCount,
  readerImageFormat,
  setReaderImageFormat,
  readerImageQuality,
  setReaderImageQuality,
  eyeProtection,
  setEyeProtection,
  w2xScale,
  setW2xScale,
  w2xNoise,
  setW2xNoise,
  w2xFormat,
  setW2xFormat,
  isOnline,
  offlineSupported,
  offlineStatus,
  offlineCaching,
  offlineDeleting,
  offlineCachedPages,
  activePageCount,
  offlineQueuedPage,
  offlineCacheError,
  readerLoading,
  onCacheBook,
  onDeleteOfflineBook,
  bookmarks,
  bookmarkNote,
  savingBookmark,
  loading,
  currentBookmark,
  currentPageNumber,
  onBookmarkNoteChange,
  onSaveBookmark,
  onDeleteBookmark,
  onJumpToPage,
}: ReaderSettingsDrawerProps) {
  const [mode, setMode] = useState<ReaderSettingsMode>(() => {
    if (typeof window === 'undefined') return 'global';
    const stored = window.localStorage.getItem(SETTINGS_MODE_STORAGE_KEY);
    return stored === 'book' ? 'book' : 'global';
  });
  const [activeTab, setActiveTab] = useState<ReaderSettingsTab>(() => (mode === 'book' ? 'cache' : 'reading'));

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(SETTINGS_MODE_STORAGE_KEY, mode);
    if (mode === 'global' && !GLOBAL_TABS.includes(activeTab)) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setActiveTab('reading');
    } else if (mode === 'book' && !BOOK_TABS.includes(activeTab)) {
      setActiveTab('cache');
    }
  }, [mode, activeTab]);

  const tabs = useMemo<Array<{ id: ReaderSettingsTab; label: string }>>(() => {
    if (mode === 'global') {
      return [
        { id: 'reading', label: t('reader.settingsTab.reading') },
        { id: 'image', label: t('reader.settingsTab.image') },
      ];
    }
    return [
      { id: 'cache', label: t('reader.settingsTab.cache') },
      { id: 'bookmarks', label: t('reader.settingsTab.bookmarks') },
    ];
  }, [mode, t]);

  return (
    <div className="self-center sm:self-end mt-4 bg-komgaSurface border border-gray-800 rounded-xl p-3 sm:p-4 shadow-2xl w-[92vw] sm:w-[360px] max-w-sm text-sm text-gray-300 flex flex-col gap-3 animate-in fade-in slide-in-from-top-4 origin-top sm:origin-top-right">
      <div className="grid grid-cols-2 gap-1 rounded-lg bg-gray-950/80 p-1 border border-gray-800">
        <button
          type="button"
          onClick={() => setMode('global')}
          className={`min-w-0 rounded-md px-2 py-1.5 text-xs font-semibold transition ${mode === 'global' ? 'bg-komgaPrimary text-white shadow' : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200'}`}
          aria-pressed={mode === 'global'}
        >
          <span className="block truncate">{t('reader.settingsMode.global')}</span>
        </button>
        <button
          type="button"
          onClick={() => setMode('book')}
          className={`min-w-0 rounded-md px-2 py-1.5 text-xs font-semibold transition ${mode === 'book' ? 'bg-komgaPrimary text-white shadow' : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200'}`}
          aria-pressed={mode === 'book'}
        >
          <span className="block truncate">{t('reader.settingsMode.book')}</span>
        </button>
      </div>
      <div className={`grid gap-1 rounded-lg bg-gray-950/80 p-1 ${tabs.length === 2 ? 'grid-cols-2' : 'grid-cols-4'}`}>
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => setActiveTab(tab.id)}
            className={`min-w-0 rounded-md px-1.5 py-1.5 text-[11px] font-medium transition ${activeTab === tab.id ? 'bg-komgaPrimary text-white shadow' : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200'}`}
          >
            <span className="block truncate">{tab.label}</span>
          </button>
        ))}
      </div>

      <div className="max-h-[70vh] overflow-y-auto pr-1">
        {activeTab === 'reading' && (
          <div className="space-y-4">
            <div>
              <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 block border-b border-gray-800 pb-1">{t('reader.layoutSection')}</span>
              <div className="flex bg-gray-900 rounded p-1 mb-3">
                <button className={`flex-1 py-1.5 rounded transition ${readMode === 'webtoon' ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadMode('webtoon')}>{t('reader.modeWebtoon')}</button>
                <button className={`flex-1 py-1.5 rounded transition ${readMode === 'paged' ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadMode('paged')}>{t('reader.modePaged')}</button>
              </div>

              {readMode === 'paged' && (
                <div className="space-y-3">
                  <div>
                    <span className="text-[10px] text-gray-500 mb-1 block">{t('reader.doublePageTitle')}</span>
                    <div className="flex bg-gray-900 rounded p-0.5">
                      <button className={`flex-1 py-1 rounded text-xs transition ${!doublePage ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setDoublePage(false)}>{t('reader.singlePage')}</button>
                      <button className={`flex-1 py-1 rounded text-xs transition ${doublePage ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setDoublePage(true)}>{t('reader.doublePage')}</button>
                    </div>
                  </div>
                  <div>
                    <span className="text-[10px] text-gray-500 mb-1 block">{t('reader.readDirection')}</span>
                    <div className="flex bg-gray-900 rounded p-0.5">
                      <button className={`flex-1 py-1 rounded text-xs transition ${readDirection === 'ltr' ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadDirection('ltr')}>{t('reader.ltr')}</button>
                      <button className={`flex-1 py-1 rounded text-xs transition ${readDirection === 'rtl' ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadDirection('rtl')}>{t('reader.rtl')}</button>
                    </div>
                  </div>
                </div>
              )}
            </div>

            <div>
              <div className="flex items-center justify-between mb-1">
                <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider">{t('reader.preloadPages')}</span>
                <span className="text-[10px] text-gray-400">{t('reader.pageCountShort', { count: preloadCount })}</span>
              </div>
              <input
                type="range"
                min={0}
                max={10}
                step={1}
                value={preloadCount}
                onChange={(e) => setPreloadCount(parseInt(e.target.value, 10))}
                className="w-full accent-komgaPrimary h-1"
              />
            </div>

            <div>
              <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 block">{t('reader.eyeProtection')}</span>
              <button
                onClick={() => setEyeProtection(!eyeProtection)}
                className={`w-full flex items-center justify-between px-3 py-2.5 rounded-lg transition-all ${eyeProtection ? 'bg-amber-900/40 border border-amber-600/40 text-amber-500' : 'bg-gray-900 border border-gray-800 text-gray-400 hover:bg-gray-800'}`}
              >
                <span className="text-xs flex items-center gap-2">
                  <span className="text-base">{eyeProtection ? '\u{1F319}' : '\u{2600}\u{FE0F}'}</span>
                  {t('reader.eyeProtectionWarm')}
                </span>
                <span className={`text-[10px] font-medium ${eyeProtection ? 'text-amber-400' : 'text-gray-600'}`}>{eyeProtection ? t('reader.on') : t('settings.koreader.off')}</span>
              </button>
            </div>
          </div>
        )}

        {activeTab === 'image' && (
          <div className="space-y-4">
            <div>
              <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 block">{t('reader.imageSection')}</span>
              <div className="flex bg-gray-900 rounded p-1 mb-3">
                {['original', 'fit-height', 'fit-width', 'fit-screen'].map((sm) => (
                  <button
                    key={sm}
                    className={`flex-1 py-1 rounded transition text-[10px] ${scaleMode === sm ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`}
                    onClick={() => setScaleMode(sm as ScaleMode)}
                    title={sm === 'original' ? t('reader.scaleOriginal') : sm === 'fit-height' ? t('reader.scaleFitHeight') : sm === 'fit-width' ? t('reader.scaleFitWidth') : t('reader.scaleFitScreen')}
                  >
                    {sm === 'original' ? t('reader.scaleOriginalShort') : sm === 'fit-height' ? t('reader.scaleFitHeightShort') : sm === 'fit-width' ? t('reader.scaleFitWidthShort') : t('reader.scaleFitScreenShort')}
                  </button>
                ))}
              </div>

              <select
                value={imageFilter}
                onChange={(e) => setImageFilter(e.target.value as ImageFilter)}
                className="w-full bg-gray-900 border border-gray-700 text-gray-300 text-xs rounded p-2 outline-none cursor-pointer mb-2"
              >
                <option value="none">{t('reader.filter.raw')}</option>
                <option value="nearest">{t('reader.filter.nearest')}</option>
                <option value="average">{t('reader.filter.average')}</option>
                <option value="bilinear">{t('reader.filter.bilinear')}</option>
                <option value="bicubic">{t('reader.filter.bicubic')}</option>
                <option value="lanczos2">{t('reader.filter.lanczos2')}</option>
                <option value="lanczos3">{t('reader.filter.lanczos3')}</option>
                <option value="mitchell">{t('reader.filter.mitchell')}</option>
                <option value="bspline">{t('reader.filter.bspline')}</option>
                <option value="catmullrom">{t('reader.filter.catmullrom')}</option>
                <option value="waifu2x">{t('reader.filter.waifu2x')}</option>
                <option value="realcugan">{t('reader.filter.realcugan')}</option>
              </select>

              <button
                className={`w-full py-2 rounded text-xs transition font-medium border ${autoCrop ? 'bg-komgaPrimary/20 border-komgaPrimary text-komgaPrimary shadow-[0_0_15px_rgba(168,85,247,0.2)]' : 'bg-gray-900 border-gray-700 text-gray-400 hover:border-gray-500'}`}
                onClick={() => setAutoCrop(!autoCrop)}
              >
                {autoCrop ? t('reader.autoCropOn') : t('reader.autoCropOff')}
              </button>
            </div>

            <div className="rounded-lg border border-gray-800 bg-gray-900/60 p-3">
              <div className="mb-2 flex items-center justify-between">
                <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider">{t('reader.networkSection')}</span>
                <span className="text-[10px] text-gray-400">{readerImageFormat === 'original' ? t('reader.networkOriginal') : `${readerImageFormat.toUpperCase()} ${readerImageQuality}`}</span>
              </div>
              <select
                value={readerImageFormat}
                onChange={(e) => setReaderImageFormat(e.target.value as ReaderImageFormat)}
                className="mb-2 w-full rounded border border-gray-700 bg-gray-950 p-2 text-xs text-gray-300 outline-none"
              >
                <option value="original">{t('reader.networkOriginal')}</option>
                <option value="webp">{t('reader.networkWebp')}</option>
                <option value="jpeg">{t('reader.networkJpeg')}</option>
              </select>
              {readerImageFormat !== 'original' && (
                <input
                  type="range"
                  min={45}
                  max={95}
                  step={5}
                  value={readerImageQuality}
                  onChange={(e) => setReaderImageQuality(parseInt(e.target.value, 10))}
                  className="w-full accent-komgaPrimary h-1"
                  aria-label={t('reader.networkQuality')}
                />
              )}
              <p className="mt-2 text-[11px] leading-5 text-gray-500">{t('reader.networkHint')}</p>
            </div>

            {(imageFilter === 'waifu2x' || imageFilter === 'realcugan') && (
              <div className="bg-gray-900/50 p-3 rounded border border-komgaPrimary/30 animate-in fade-in slide-in-from-top-2">
                <div className="mb-3">
                  <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider mb-2 flex justify-between">
                    <span>{t('reader.engineScale')}</span>
                    <span className="text-komgaPrimary">{w2xScale}x</span>
                  </span>
                  <div className="flex bg-gray-900 rounded p-1 border border-gray-800">
                    {[1, 2, 4, 8].map((scale) => (
                      <button key={scale} className={`flex-1 py-1 rounded transition text-xs font-semibold ${w2xScale === scale ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`} onClick={() => setW2xScale(scale)}>{scale}x</button>
                    ))}
                  </div>
                </div>
                <div className="mb-3">
                  <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider mb-2 flex justify-between">
                    <span>{t('reader.noiseLevel')}</span>
                    <span className="text-komgaPrimary">{w2xNoise === -1 ? t('settings.koreader.off') : w2xNoise}</span>
                  </span>
                  <div className="flex bg-gray-900 rounded p-1 border border-gray-800">
                    {[-1, 0, 1, 2, 3].map((noise) => (
                      <button key={noise} className={`flex-1 py-1 rounded transition text-xs font-semibold ${w2xNoise === noise ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`} onClick={() => setW2xNoise(noise)}>{noise === -1 ? t('settings.koreader.off') : noise}</button>
                    ))}
                  </div>
                </div>
                <div>
                  <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider mb-2 flex justify-between">
                    <span>{t('reader.outputFormat')}</span>
                    <span className="text-komgaPrimary uppercase text-[10px]">{w2xFormat}</span>
                  </span>
                  <div className="flex bg-gray-900 rounded p-1 border border-gray-800">
                    {['webp', 'png', 'jpg'].map((format) => (
                      <button key={format} className={`flex-1 py-1 rounded transition text-xs font-semibold uppercase ${w2xFormat === format ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`} onClick={() => setW2xFormat(format)}>{format}</button>
                    ))}
                  </div>
                </div>
              </div>
            )}
          </div>
        )}

        {activeTab === 'cache' && (
          <OfflineReadingPanel
            t={t}
            isOnline={isOnline}
            offlineSupported={offlineSupported}
            offlineStatus={offlineStatus}
            offlineCaching={offlineCaching}
            offlineDeleting={offlineDeleting}
            offlineCachedPages={offlineCachedPages}
            activePageCount={activePageCount}
            offlineQueuedPage={offlineQueuedPage}
            offlineCacheError={offlineCacheError}
            readerLoading={readerLoading}
            onCacheBook={onCacheBook}
            onDeleteOfflineBook={onDeleteOfflineBook}
          />
        )}

        {activeTab === 'bookmarks' && (
          <BookmarkPanel
            t={t}
            bookmarks={bookmarks}
            bookmarkNote={bookmarkNote}
            savingBookmark={savingBookmark}
            loading={loading}
            currentBookmark={currentBookmark}
            currentPageNumber={currentPageNumber}
            onBookmarkNoteChange={onBookmarkNoteChange}
            onSaveBookmark={onSaveBookmark}
            onDeleteBookmark={onDeleteBookmark}
            onJumpToPage={onJumpToPage}
          />
        )}
      </div>
    </div>
  );
}
