import { ArrowLeft, Bookmark, CircleHelp, Loader2, Settings } from 'lucide-react';

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

interface ReaderTopBarProps {
  t: Translate;
  bookTitle: string;
  isBookmarked: boolean;
  savingBookmark: boolean;
  loading: boolean;
  showHelp: boolean;
  showSettings: boolean;
  onBack: () => void;
  onSaveBookmark: () => void;
  onToggleHelp: () => void;
  onToggleSettings: () => void;
}

export function ReaderTopBar({
  t,
  bookTitle,
  isBookmarked,
  savingBookmark,
  loading,
  showHelp,
  showSettings,
  onBack,
  onSaveBookmark,
  onToggleHelp,
  onToggleSettings,
}: ReaderTopBarProps) {
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
        <span className="text-white font-medium truncate drop-shadow-md text-center">{bookTitle}</span>
      </div>

      <div className="flex items-center gap-2 shrink-0 z-10">
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
