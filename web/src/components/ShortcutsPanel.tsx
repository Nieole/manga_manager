import { useEffect } from 'react';
import { createPortal } from 'react-dom';
import { Keyboard, X } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';

interface ShortcutEntry {
  keys: string[];
  labelKey: string;
}

interface ShortcutGroup {
  titleKey: string;
  entries: ShortcutEntry[];
}

const GROUPS: ShortcutGroup[] = [
  {
    titleKey: 'shortcuts.group.global',
    entries: [
      { keys: ['?'], labelKey: 'shortcuts.global.openPanel' },
      { keys: ['/'], labelKey: 'shortcuts.global.search' },
      { keys: ['Esc'], labelKey: 'shortcuts.global.close' },
      { keys: ['g', 'h'], labelKey: 'shortcuts.global.goHome' },
      { keys: ['g', 'r'], labelKey: 'shortcuts.global.goReviews' },
      { keys: ['g', 'o'], labelKey: 'shortcuts.global.goOps' },
      { keys: ['g', 'c'], labelKey: 'shortcuts.global.goCollections' },
      { keys: ['g', 'l'], labelKey: 'shortcuts.global.goReadingLists' },
      { keys: ['g', 's'], labelKey: 'shortcuts.global.goSettings' },
      { keys: ['['], labelKey: 'shortcuts.global.toggleSidebar' },
    ],
  },
  {
    titleKey: 'shortcuts.group.library',
    entries: [
      { keys: ['/'], labelKey: 'shortcuts.library.search' },
      { keys: ['g'], labelKey: 'shortcuts.library.jumpFirst' },
      { keys: ['Shift', 'G'], labelKey: 'shortcuts.library.jumpLast' },
      { keys: ['e'], labelKey: 'shortcuts.library.toggleSelection' },
      { keys: ['Esc'], labelKey: 'shortcuts.library.exitSelection' },
    ],
  },
  {
    titleKey: 'shortcuts.group.reviews',
    entries: [
      { keys: ['j'], labelKey: 'shortcuts.reviews.next' },
      { keys: ['k'], labelKey: 'shortcuts.reviews.prev' },
      { keys: ['a'], labelKey: 'shortcuts.reviews.approve' },
      { keys: ['r'], labelKey: 'shortcuts.reviews.reject' },
      { keys: ['Space'], labelKey: 'shortcuts.reviews.toggleSelect' },
    ],
  },
  {
    titleKey: 'shortcuts.group.organize',
    entries: [
      { keys: ['j'], labelKey: 'shortcuts.organize.next' },
      { keys: ['k'], labelKey: 'shortcuts.organize.prev' },
      { keys: ['Enter'], labelKey: 'shortcuts.organize.open' },
    ],
  },
  {
    titleKey: 'shortcuts.group.tasks',
    entries: [
      { keys: ['p'], labelKey: 'shortcuts.tasks.pause' },
      { keys: ['c'], labelKey: 'shortcuts.tasks.cancel' },
      { keys: ['y'], labelKey: 'shortcuts.tasks.retry' },
    ],
  },
];

interface ShortcutsPanelProps {
  open: boolean;
  onClose: () => void;
}

export function ShortcutsPanel({ open, onClose }: ShortcutsPanelProps) {
  const { t } = useI18n();
  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [open, onClose]);

  if (!open) return null;
  const node = (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-3xl max-h-[90vh] flex flex-col rounded-2xl border border-gray-800 bg-gray-950 shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="flex items-center justify-between border-b border-gray-800 px-5 py-4">
          <div className="flex items-center gap-3">
            <span className="rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 p-2 text-komgaPrimary">
              <Keyboard className="h-4 w-4" />
            </span>
            <div>
              <h2 className="text-sm font-semibold text-white">{t('shortcuts.title')}</h2>
              <p className="text-xs text-gray-500">{t('shortcuts.description')}</p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="rounded-md p-1.5 text-gray-500 hover:bg-gray-800 hover:text-white transition"
            aria-label={t('common.close')}
          >
            <X className="h-4 w-4" />
          </button>
        </header>
        <div className="grid gap-4 p-5 sm:grid-cols-2 overflow-y-auto min-h-0">
          {GROUPS.map((group) => (
            <section key={group.titleKey} className="rounded-xl border border-gray-800 bg-gray-900/40 p-3">
              <h3 className="text-[11px] font-semibold uppercase tracking-wider text-gray-500">
                {t(group.titleKey)}
              </h3>
              <ul className="mt-2 space-y-1.5">
                {group.entries.map((entry) => (
                  <li key={entry.labelKey} className="flex items-center justify-between gap-3 text-sm">
                    <span className="text-gray-300">{t(entry.labelKey)}</span>
                    <span className="flex items-center gap-1">
                      {entry.keys.map((key, idx) => (
                        <span key={idx} className="flex items-center gap-1">
                          {idx > 0 && <span className="text-[10px] text-gray-600">{t('shortcuts.then')}</span>}
                          <kbd className="min-w-[1.75rem] rounded border border-gray-700 bg-gray-900 px-1.5 py-0.5 text-center font-mono text-[10px] text-gray-300">
                            {key}
                          </kbd>
                        </span>
                      ))}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
        <footer className="border-t border-gray-800 px-5 py-3 text-[11px] text-gray-500">
          {t('shortcuts.footerHint')}
        </footer>
      </div>
    </div>
  );
  return createPortal(node, document.body);
}
