import { Bookmark, Loader2, Trash2 } from 'lucide-react';
import type { ReadingBookmark } from './types';

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

interface BookmarkPanelProps {
  t: Translate;
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

export function BookmarkPanel({
  t,
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
}: BookmarkPanelProps) {
  return (
    <div className="rounded-lg border border-gray-800 bg-gray-900/50 p-3">
      <div className="mb-2 flex items-center justify-between">
        <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider">{t('reader.bookmarks')}</span>
        <span className="text-[10px] text-gray-400">{t('reader.bookmark.count', { count: bookmarks.length })}</span>
      </div>
      <textarea
        value={bookmarkNote}
        onChange={(e) => onBookmarkNoteChange(e.target.value)}
        placeholder={t('reader.bookmark.notePlaceholder')}
        className="mb-2 h-16 w-full resize-none rounded border border-gray-700 bg-gray-950 p-2 text-xs text-gray-300 outline-none focus:border-komgaPrimary"
      />
      <button
        onClick={onSaveBookmark}
        disabled={savingBookmark || loading}
        className="mb-3 flex w-full items-center justify-center gap-2 rounded bg-komgaPrimary px-3 py-2 text-xs font-semibold text-white hover:bg-komgaPrimaryHover disabled:cursor-not-allowed disabled:opacity-60"
      >
        {savingBookmark ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Bookmark className="h-3.5 w-3.5" />}
        {currentBookmark ? t('reader.bookmark.update') : t('reader.bookmark.addCurrent', { page: currentPageNumber })}
      </button>
      <div className="max-h-36 space-y-2 overflow-y-auto pr-1">
        {bookmarks.length === 0 ? (
          <p className="text-xs text-gray-500">{t('reader.bookmark.empty')}</p>
        ) : bookmarks.map((bookmark) => (
          <div key={bookmark.id} className="flex items-start gap-2 rounded border border-gray-800 bg-gray-950/70 p-2">
            <button
              onClick={() => onJumpToPage(bookmark.page)}
              className="shrink-0 rounded bg-gray-800 px-2 py-1 text-[11px] font-semibold text-komgaPrimary hover:bg-gray-700"
            >
              {t('reader.bookmark.page', { page: bookmark.page })}
            </button>
            <button
              onClick={() => onJumpToPage(bookmark.page)}
              className="min-w-0 flex-1 text-left text-[11px] leading-5 text-gray-300 hover:text-white"
            >
              {bookmark.note || t('reader.bookmark.noNote')}
            </button>
            <button
              onClick={() => onDeleteBookmark(bookmark)}
              className="shrink-0 rounded p-1 text-gray-500 hover:bg-red-500/10 hover:text-red-300"
              title={t('reader.bookmark.delete')}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
