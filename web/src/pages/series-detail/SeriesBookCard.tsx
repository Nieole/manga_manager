import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { BookImage, CheckCircle2, MoreHorizontal } from 'lucide-react';
import type { Book } from './types';
import { useI18n } from '../../i18n/LocaleProvider';

interface SeriesBookCardProps {
  book: Book;
  isSelectionMode: boolean;
  isSelected: boolean;
  onCardClick: (book: Book) => void;
  onQuickToggleRead: (book: Book, makeRead: boolean) => void;
  onExportComicInfo: (book: Book) => void;
  onCopyPath: (book: Book) => void;
}

export function SeriesBookCard({
  book,
  isSelectionMode,
  isSelected,
  onCardClick,
  onQuickToggleRead,
  onExportComicInfo,
  onCopyPath,
}: SeriesBookCardProps) {
  const { t } = useI18n();
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!menuOpen) return;
    function handle(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false);
    }
    document.addEventListener('mousedown', handle);
    return () => document.removeEventListener('mousedown', handle);
  }, [menuOpen]);

  const isFinished = book.last_read_page?.Valid && book.last_read_page.Int64 >= book.page_count && book.page_count > 0;
  const readPage = book.last_read_page?.Valid ? book.last_read_page.Int64 : 0;
  const progressPct = book.page_count > 0 ? Math.min(100, (readPage / book.page_count) * 100) : 0;
  const showProgress = book.page_count > 0 && readPage > 0;
  const showResumeBadge = readPage > 0 && !isFinished && book.page_count > 0;

  const coverSrc = book.cover_path?.Valid
    ? `/api/covers/${book.id}${book.updated_at ? `?v=${new Date(book.updated_at).getTime()}` : ''}`
    : null;

  return (
    <Link
      to={`/reader/${book.id}`}
      onClick={(e) => {
        if (isSelectionMode) {
          e.preventDefault();
          onCardClick(book);
        }
      }}
      className={`group flex flex-col rounded-2xl overflow-hidden bg-gray-950/40 backdrop-blur-md border ${
        isSelected
          ? 'border-komgaPrimary ring-2 ring-komgaPrimary shadow-[0_0_20px_rgba(var(--rgb-komga-primary),0.3)]'
          : 'border-white/5 hover:border-white/20 hover:-translate-y-1.5 hover:shadow-[0_20px_40px_-15px_rgba(0,0,0,0.7)]'
      } transition-all duration-500 cursor-pointer`}
    >
      <div className="aspect-3/4 w-full bg-gray-900/50 border-b border-white/5 flex items-center justify-center relative overflow-hidden">
        {isSelectionMode && (
          <div className="absolute top-2 left-2 z-30">
            <div
              className={`w-5 h-5 rounded-full border-2 flex items-center justify-center transition-colors ${
                isSelected ? 'bg-komgaPrimary border-komgaPrimary' : 'bg-black/50 border-gray-400'
              }`}
            >
              {isSelected && <span className="text-white text-xs font-bold leading-none select-none">&#10003;</span>}
            </div>
          </div>
        )}
        {coverSrc ? (
          <>
            <img
              src={coverSrc}
              className="absolute inset-0 w-full h-full object-cover transition-transform duration-700 group-hover:scale-110"
              alt={t('common.cover')}
              loading="lazy"
            />
            <div className="absolute inset-0 ring-1 ring-inset ring-white/10 pointer-events-none transition-opacity group-hover:opacity-50" />
            <div className="absolute inset-0 bg-linear-to-t from-gray-950/80 via-transparent to-gray-950/20 opacity-0 group-hover:opacity-100 transition-opacity duration-500 pointer-events-none" />
          </>
        ) : (
          <BookImage className="w-12 h-12 text-gray-700 opacity-50 group-hover:text-komgaPrimary transition-colors relative z-10" />
        )}

        {!isSelectionMode && (
          <div className="absolute top-2 right-2 z-30 flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
            <button
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                onQuickToggleRead(book, !isFinished);
              }}
              className="p-1.5 rounded-full bg-black/60 border border-white/10 text-white/40 hover:text-green-400 hover:bg-green-400/20 hover:border-green-400/40 transition-colors backdrop-blur-sm"
              title={isFinished ? t('series.book.markUnread') : t('series.book.quickMarkRead')}
            >
              <CheckCircle2 className={`w-4 h-4 ${isFinished ? 'text-green-400 fill-green-400/20' : ''}`} />
            </button>
            <div className="relative" ref={menuRef}>
              <button
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  setMenuOpen((v) => !v);
                }}
                className="p-1.5 rounded-full bg-black/60 border border-white/10 text-white/40 hover:text-komgaPrimary hover:bg-komgaPrimary/20 hover:border-komgaPrimary/40 transition-colors backdrop-blur-sm"
                title={t('series.book.moreActions')}
              >
                <MoreHorizontal className="w-4 h-4" />
              </button>
              {menuOpen && (
                <div className="absolute right-0 top-full mt-1 w-44 rounded-xl border border-white/10 bg-komgaSurface shadow-2xl z-40 overflow-hidden">
                  <button
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      setMenuOpen(false);
                      onExportComicInfo(book);
                    }}
                    className="block w-full text-left px-3 py-2 text-xs text-gray-200 hover:bg-komgaPrimary/15 hover:text-white"
                  >
                    {t('series.book.exportComicInfo')}
                  </button>
                  <button
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      setMenuOpen(false);
                      onCopyPath(book);
                    }}
                    className="block w-full text-left px-3 py-2 text-xs text-gray-200 hover:bg-komgaPrimary/15 hover:text-white border-t border-white/5"
                  >
                    {t('series.book.copyPath')}
                  </button>
                </div>
              )}
            </div>
          </div>
        )}

        {showResumeBadge && (
          <div className="absolute right-2 bottom-2 z-20 px-2 py-0.5 rounded-md bg-gray-950/70 border border-white/10 text-[11px] font-semibold text-amber-200">
            {readPage}/{book.page_count}
          </div>
        )}

        <div className="absolute inset-0 bg-linear-to-t from-gray-950/90 via-gray-950/10 to-transparent flex items-end p-3 z-10 pointer-events-none opacity-80 group-hover:opacity-100 transition-opacity">
          <span className="text-[10px] font-bold uppercase tracking-widest text-white px-2 py-1 bg-gray-950/40 border border-white/10 rounded-md backdrop-blur-md drop-shadow-md">
            {book.page_count} Pages
          </span>
        </div>

        {showProgress && (
          <div className="absolute inset-x-0 bottom-0 h-1.5 bg-gray-950/60 z-20 shadow-inner">
            <div
              className={`h-full transition-all duration-700 ease-out ${isFinished ? 'bg-linear-to-r from-green-500 to-green-400' : 'bg-linear-to-r from-komgaPrimary to-komgaPrimaryHover'}`}
              style={{ width: `${progressPct}%` }}
            />
          </div>
        )}
      </div>
      <div className="p-4 flex-1 bg-linear-to-b from-transparent to-gray-950/20">
        <h4 className="text-sm font-bold text-gray-200 line-clamp-2 leading-snug group-hover:text-white transition-colors drop-shadow-xs">
          {book.title?.Valid ? book.title.String : book.name}
        </h4>
      </div>
    </Link>
  );
}
