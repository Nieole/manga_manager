import { useEffect, useId, useRef, useState } from 'react';
import { ArrowLeft, Bookmark, CircleHelp, Download, Ellipsis, ImagePlus, ListOrdered, Loader2, Settings } from 'lucide-react';
import type { ProgressSyncStatus } from './useReaderProgressIndicator';
import type { VolumeBookEntry } from './useReaderSiblings';
import { downloadBookFile } from '../../utils/download';
import { setBookCoverFromPage } from '../../utils/cover';
import { getApiErrorMessage } from '../../api/client';
import { useAuth } from '../../auth/AuthProvider';
import { useToast } from '../../components/ToastProvider';

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
  currentPageNumber: number;
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
  currentPageNumber,
  onOpenBook,
}: ReaderTopBarProps) {
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [settingCover, setSettingCover] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const barRef = useRef<HTMLDivElement>(null);
  const moreButtonRef = useRef<HTMLButtonElement>(null);
  const chaptersButtonRef = useRef<HTMLButtonElement>(null);
  const moreId = useId();
  const { showToast } = useToast();
  // 把当前页设为封面写的是 POST /api/books/{id}/cover，后端只放行管理员；书签、进度与下载不受此限。
  const { isAdmin } = useAuth();

  useEffect(() => {
    if (!moreOpen && !popoverOpen) return;
    const onPointerDown = (event: PointerEvent) => {
      if (event.target instanceof Node && !barRef.current?.contains(event.target)) {
        setMoreOpen(false);
        setPopoverOpen(false);
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      event.stopPropagation();
      setMoreOpen(false);
      setPopoverOpen(false);
      (moreOpen ? moreButtonRef : chaptersButtonRef).current?.focus();
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown, true);
    };
  }, [moreOpen, popoverOpen]);

  const handleSetCover = async () => {
    if (currentBookId === null) return;
    setSettingCover(true);
    try {
      await setBookCoverFromPage(currentBookId, currentPageNumber);
      showToast(t('reader.coverSet'), 'success');
    } catch (err) {
      showToast(getApiErrorMessage(err, t('reader.coverSetFailed')), 'error');
    } finally {
      setSettingCover(false);
    }
  };

  const secondaryActions = [
    ...(currentBookId !== null && isAdmin ? [{
      key: 'cover', label: t('reader.setCover'), onClick: handleSetCover, disabled: settingCover, active: false,
      icon: settingCover ? <Loader2 className="w-5 h-5 animate-spin" /> : <ImagePlus className="w-5 h-5" />,
    }] : []),
    ...(currentBookId !== null ? [{
      key: 'download', label: t('reader.download'), onClick: () => downloadBookFile(currentBookId), disabled: false, active: false,
      icon: <Download className="w-5 h-5" />,
    }] : []),
    {
      key: 'bookmark', label: isBookmarked ? t('reader.bookmark.update') : t('reader.bookmark.add'),
      onClick: onSaveBookmark, disabled: savingBookmark || loading, active: isBookmarked,
      icon: savingBookmark ? <Loader2 className="w-5 h-5 animate-spin" /> : <Bookmark className="w-5 h-5" />,
    },
    { key: 'help', label: t('reader.help'), onClick: onToggleHelp, disabled: false, active: showHelp, icon: <CircleHelp className="w-5 h-5" /> },
  ];

  return (
    <div ref={barRef} className="flex items-center justify-between gap-2 sm:gap-0 w-full relative">
      <button
        onClick={onBack}
        aria-label={t('reader.back')}
        className="text-white hover:text-komgaPrimary transition flex items-center justify-center bg-komgaDark/70 rounded-full p-2.5 min-w-11 min-h-11 sm:min-w-0 sm:min-h-0 sm:px-4 sm:py-2 backdrop-blur-sm border border-white/10 shadow-lg shrink-0 z-10"
      >
        <ArrowLeft className="w-5 h-5 sm:mr-2" />
        <span className="hidden sm:inline">{t('reader.back')}</span>
      </button>

      <div className="flex min-w-0 flex-1 items-center justify-center pointer-events-none sm:absolute sm:inset-0 sm:px-32">
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
          <>
            <button
              ref={chaptersButtonRef}
              onClick={() => {
                setMoreOpen(false);
                setPopoverOpen((v) => !v);
              }}
              className={`text-white hover:text-komgaPrimary transition flex items-center justify-center min-w-11 min-h-11 sm:min-w-0 sm:min-h-0 bg-komgaDark/70 rounded-full p-2.5 backdrop-blur-sm border shadow-lg ${popoverOpen ? 'text-komgaPrimary border-komgaPrimary/50' : 'border-white/10'}`}
              title={t('reader.siblings.volumeChapters')}
              aria-expanded={popoverOpen}
            >
              <ListOrdered className="w-5 h-5" />
            </button>
            {popoverOpen && (
              <div className="absolute right-0 top-full mt-2 w-[85vw] sm:w-72 max-h-[50vh] sm:max-h-[70vh] overflow-y-auto rounded-2xl border border-white/15 bg-komgaDark/95 backdrop-blur-sm p-2 shadow-2xl z-50">
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
          </>
        )}
        <div className="hidden sm:flex items-center gap-2">
          {secondaryActions.map(action => (
            <button
              key={action.key}
              onClick={action.onClick}
              disabled={action.disabled}
              title={action.label}
              className={`text-white hover:text-komgaPrimary transition flex items-center bg-komgaDark/70 rounded-full p-2.5 backdrop-blur-sm border border-white/10 shadow-lg disabled:opacity-60 ${action.active ? 'text-komgaPrimary border-komgaPrimary/50' : ''}`}
            >
              {action.icon}
            </button>
          ))}
        </div>
        <div className="sm:hidden">
          <button
            ref={moreButtonRef}
            onClick={() => {
              setPopoverOpen(false);
              setMoreOpen(v => !v);
            }}
            title={t('reader.moreActions')}
            aria-label={t('reader.moreActions')}
            aria-expanded={moreOpen}
            aria-controls={moreId}
            className={`text-white hover:text-komgaPrimary transition flex items-center justify-center min-w-11 min-h-11 bg-komgaDark/70 rounded-full p-2.5 backdrop-blur-sm border shadow-lg ${moreOpen ? 'text-komgaPrimary border-komgaPrimary/50' : 'border-white/10'}`}
          >
            <Ellipsis className="w-5 h-5" />
          </button>
          {moreOpen && (
            <div id={moreId} role="group" aria-label={t('reader.moreActions')} className="absolute right-0 top-full mt-2 w-64 max-w-[calc(100vw-3rem)] max-h-[50vh] overflow-y-auto rounded-2xl border border-white/15 bg-komgaDark/95 backdrop-blur-sm p-2 shadow-2xl z-50">
              {[...secondaryActions].sort((a, b) => Number(b.key === 'bookmark') - Number(a.key === 'bookmark')).map(action => (
                <button
                  key={action.key}
                  onClick={() => {
                    setMoreOpen(false);
                    action.onClick();
                  }}
                  disabled={action.disabled}
                  className={`w-full min-h-11 flex items-center gap-3 px-3 py-2 text-sm text-left rounded-lg hover:bg-white/5 transition-colors disabled:opacity-60 ${action.active ? 'text-komgaPrimary' : 'text-gray-200'}`}
                >
                  {action.icon}
                  <span>{action.label}</span>
                </button>
              ))}
            </div>
          )}
        </div>
        <button
          onClick={() => {
            setMoreOpen(false);
            setPopoverOpen(false);
            onToggleSettings();
          }}
          className={`text-white hover:text-komgaPrimary transition flex items-center justify-center min-w-11 min-h-11 sm:min-w-0 sm:min-h-0 bg-komgaDark/70 rounded-full p-2.5 backdrop-blur-sm border border-white/10 shadow-lg ${showSettings ? 'text-komgaPrimary border-komgaPrimary/50' : ''}`}
          title={t('reader.settings')}
        >
          <Settings className="w-5 h-5" />
        </button>
      </div>
    </div>
  );
}
