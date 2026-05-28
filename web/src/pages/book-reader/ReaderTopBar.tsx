import { useState } from 'react';
import { ArrowLeft, Bookmark, CircleHelp, ListOrdered, Loader2, Settings } from 'lucide-react';
import type { ProgressSyncStatus } from './useReaderProgressIndicator';
import type { VolumeBookEntry } from './useReaderSiblings';

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

interface ReaderTopBarProps {
  t: Translate;
  bookTitle: string;
  bookVolume?: string;
  isBookmarked: boolean;
  savingBookmark: boolean;
  loading: boolean;
  showHelp: boolean;
  showSettings: boolean;
  progressStatus?: ProgressSyncStatus;
  onBack: () => void;
  onSaveBookmark: () => void;
  onToggleHelp: () => void;
  onToggleSettings: () => void;
  allInVolume: VolumeBookEntry[];
  currentBookId: number | null;
  onOpenBook: (bookId: number) => void;
}

const STATUS_DOT_STYLE: Record<ProgressSyncStatus, string> = {
  idle: 'bg-gray-500/40',
  syncing: 'bg-amber-400 animate-pulse',
  synced: 'bg-emerald-400',
  'offline-queued': 'bg-rose-400',
};

function statusTooltipKey(status: ProgressSyncStatus) {
  switch (status) {
    case 'syncing':
      return 'reader.progress.syncing';
    case 'synced':
      return 'reader.progress.synced';
    case 'offline-queued':
      return 'reader.progress.offlineQueued';
    default:
      return 'reader.progress.idle';
  }
}

export function ReaderTopBar({
  t,
  bookTitle,
  bookVolume,
  isBookmarked,
  savingBookmark,
  loading,
  showHelp,
  showSettings,
  progressStatus,
  onBack,
  onSaveBookmark,
  onToggleHelp,
  onToggleSettings,
  allInVolume,
  currentBookId,
  onOpenBook,
}: ReaderTopBarProps) {
  const [popoverOpen, setPopoverOpen] = useState(false);

  return (
    <div className="flex items-center justify-between w-full relative">
      <button
        onClick={onBack}
        className="text-white hover:text-komgaPrimary transition flex items-center bg-komgaDark/70 rounded-full px-4 py-2 backdrop-blur border border-white/10 shadow-lg shrink-0 z-10"
      >
        <ArrowLeft className="w-5 h-5 mr-2" />
        {t('reader.back')}
      </button>

      <div className="absolute inset-0 flex items-center justify-center pointer-events-none px-32">
        <div className="flex flex-col items-center max-w-full">
          <div className="flex items-center gap-2 max-w-full">
            {progressStatus && (
              <span
                className={`w-2 h-2 rounded-full shrink-0 shadow-[0_0_6px] ${STATUS_DOT_STYLE[progressStatus]}`}
                title={t(statusTooltipKey(progressStatus))}
                aria-label={t(statusTooltipKey(progressStatus))}
              />
            )}
            <span className="text-white font-medium truncate drop-shadow-md text-center">{bookTitle}</span>
          </div>
          {bookVolume && (
            <span className="text-[11px] text-gray-300/80 truncate drop-shadow-md mt-0.5 max-w-full">
              {bookVolume}
            </span>
          )}
        </div>
      </div>

      <div className="flex items-center gap-2 shrink-0 z-10">
        {allInVolume.length > 1 && (
          <div className="relative">
            <button
              onClick={() => setPopoverOpen((v) => !v)}
              className={`text-white hover:text-komgaPrimary transition flex items-center bg-komgaDark/70 rounded-full p-2.5 backdrop-blur border shadow-lg ${popoverOpen ? 'text-komgaPrimary border-komgaPrimary/50' : 'border-white/10'}`}
              title={t('reader.siblings.volumeChapters')}
              aria-expanded={popoverOpen}
            >
              <ListOrdered className="w-5 h-5" />
            </button>
            {popoverOpen && (
              <div className="absolute right-0 top-full mt-2 w-72 max-h-80 overflow-y-auto rounded-2xl border border-white/15 bg-komgaDark/95 backdrop-blur p-2 shadow-2xl z-50">
                <ul className="space-y-1">
                  {allInVolume.map((book) => {
                    const active = book.id === currentBookId;
                    return (
                      <li key={book.id}>
                        <button
                          type="button"
                          onClick={() => {
                            setPopoverOpen(false);
                            onOpenBook(book.id);
                          }}
                          className={`w-full text-left px-3 py-2 rounded-lg text-xs transition-colors flex items-center gap-2 ${
                            active
                              ? 'bg-komgaPrimary/20 text-komgaPrimary border border-komgaPrimary/40'
                              : 'hover:bg-white/5 text-gray-200 border border-transparent'
                          }`}
                        >
                          <span className="truncate">{book.title}</span>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              </div>
            )}
          </div>
        )}
        <button
          onClick={onSaveBookmark}
          className={`text-white hover:text-komgaPrimary transition flex items-center bg-komgaDark/70 rounded-full p-2.5 backdrop-blur border border-white/10 shadow-lg ${isBookmarked ? 'text-komgaPrimary border-komgaPrimary/50' : ''}`}
          title={isBookmarked ? t('reader.bookmark.update') : t('reader.bookmark.add')}
          disabled={savingBookmark || loading}
        >
          {savingBookmark ? <Loader2 className="w-5 h-5 animate-spin" /> : <Bookmark className="w-5 h-5" />}
        </button>
        <button
          onClick={onToggleHelp}
          className={`text-white hover:text-komgaPrimary transition flex items-center bg-komgaDark/70 rounded-full p-2.5 backdrop-blur border border-white/10 shadow-lg ${showHelp ? 'text-komgaPrimary border-komgaPrimary/50' : ''}`}
          title={t('reader.help')}
        >
          <CircleHelp className="w-5 h-5" />
        </button>
        <button
          onClick={onToggleSettings}
          className={`text-white hover:text-komgaPrimary transition flex items-center bg-komgaDark/70 rounded-full p-2.5 backdrop-blur border border-white/10 shadow-lg ${showSettings ? 'text-komgaPrimary border-komgaPrimary/50' : ''}`}
          title={t('reader.settings')}
        >
          <Settings className="w-5 h-5" />
        </button>
      </div>
    </div>
  );
}
