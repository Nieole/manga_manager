import { useEffect } from 'react';
import type { ReadDirection, ReadMode } from './types';

function isReaderShortcutInput(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false;
  const tagName = target.tagName.toLowerCase();
  return tagName === 'input' || tagName === 'textarea' || tagName === 'select' || target.isContentEditable;
}

interface UseReaderKeyboardShortcutsOptions {
  readMode: ReadMode;
  readDirection: ReadDirection;
  activePageCount: number;
  onNext: () => void;
  onPrev: () => void;
  onFirstPage: () => void;
  onLastPage: () => void;
  onToggleHelp: () => void;
  onSaveBookmark: () => void;
}

export function useReaderKeyboardShortcuts({
  readMode,
  readDirection,
  activePageCount,
  onNext,
  onPrev,
  onFirstPage,
  onLastPage,
  onToggleHelp,
  onSaveBookmark,
}: UseReaderKeyboardShortcutsOptions) {
  useEffect(() => {
    if (readMode !== 'paged') return undefined;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (isReaderShortcutInput(event.target)) return;
      if (event.key === 'ArrowRight' || event.key === 'PageDown' || event.key === ' ') {
        event.preventDefault();
        if (readDirection === 'ltr') {
          onNext();
        } else {
          onPrev();
        }
      } else if (event.key === 'ArrowLeft' || event.key === 'PageUp') {
        event.preventDefault();
        if (readDirection === 'ltr') {
          onPrev();
        } else {
          onNext();
        }
      } else if (event.key === 'Home') {
        event.preventDefault();
        onFirstPage();
      } else if (event.key === 'End') {
        event.preventDefault();
        onLastPage();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [activePageCount, onFirstPage, onLastPage, onNext, onPrev, readDirection, readMode]);

  useEffect(() => {
    const handleGlobalHelp = (event: KeyboardEvent) => {
      if (isReaderShortcutInput(event.target)) return;
      if (event.key.toLowerCase() === 'h' || event.key === '?') {
        event.preventDefault();
        onToggleHelp();
      } else if (event.key.toLowerCase() === 'b') {
        event.preventDefault();
        onSaveBookmark();
      }
    };

    window.addEventListener('keydown', handleGlobalHelp);
    return () => window.removeEventListener('keydown', handleGlobalHelp);
  }, [onSaveBookmark, onToggleHelp]);
}
