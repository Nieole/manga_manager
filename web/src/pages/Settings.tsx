import { useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { Outlet, UNSAFE_NavigationContext, useLocation, useNavigate } from 'react-router-dom';
import { AlertTriangle, FolderOpen, HardDrive, LayoutDashboard, Palette, Settings as SettingsIcon, Sparkles, TabletSmartphone, Wrench } from 'lucide-react';
import { ConfirmDialog } from '../components/ui/ConfirmDialog';
import { useI18n } from '../i18n/LocaleProvider';
import { SettingsProvider, useSettings } from './settings/SettingsContext';

type SettingsSectionKey = 'overview' | 'appearance' | 'library' | 'media' | 'ai' | 'koreader' | 'maintenance';

function getSectionKey(pathname: string): SettingsSectionKey {
  if (pathname.startsWith('/settings/appearance')) return 'appearance';
  if (pathname.startsWith('/settings/library')) return 'library';
  if (pathname.startsWith('/settings/media')) return 'media';
  if (pathname.startsWith('/settings/ai')) return 'ai';
  if (pathname.startsWith('/settings/koreader')) return 'koreader';
  if (pathname.startsWith('/settings/maintenance')) return 'maintenance';
  return 'overview';
}

function SettingsLayoutInner() {
  const { t } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const { loading, config, validation, toastMsg, setToastMsg, hasSectionChanges } = useSettings();
  const navigationContext = useContext(UNSAFE_NavigationContext);
  const navigator = navigationContext.navigator as { block?: (cb: (tx: { retry: () => void }) => void) => () => void };
  const [pendingTransition, setPendingTransition] = useState<null | { retry: () => void }>(null);

  const currentSection = getSectionKey(location.pathname);
  const currentHasUnsaved = hasSectionChanges(currentSection);

  useEffect(() => {
    if (!currentHasUnsaved || typeof navigator.block !== 'function') {
      return;
    }

    const unblock = navigator.block((tx) => {
      setPendingTransition({
        retry: () => {
          unblock();
          tx.retry();
        },
      });
    });

    return unblock;
  }, [currentHasUnsaved, navigator]);

  useEffect(() => {
    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!currentHasUnsaved) return;
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', handleBeforeUnload);
    return () => window.removeEventListener('beforeunload', handleBeforeUnload);
  }, [currentHasUnsaved]);

  const navigateSettingsSection = (path: string) => {
    if (path === location.pathname) return;
    navigate(path);
  };

  const navItems: Array<{ key: SettingsSectionKey; label: string; path: string; icon: ReactNode }> = [
    { key: 'overview', label: t('settings.nav.overview'), path: '/settings', icon: <LayoutDashboard className="h-4 w-4" /> },
    { key: 'appearance', label: t('settings.nav.appearance'), path: '/settings/appearance', icon: <Palette className="h-4 w-4" /> },
    { key: 'library', label: t('settings.nav.library'), path: '/settings/library', icon: <FolderOpen className="h-4 w-4" /> },
    { key: 'media', label: t('settings.nav.media'), path: '/settings/media', icon: <HardDrive className="h-4 w-4" /> },
    { key: 'ai', label: t('settings.nav.ai'), path: '/settings/ai', icon: <Sparkles className="h-4 w-4" /> },
    { key: 'koreader', label: 'KOReader', path: '/settings/koreader', icon: <TabletSmartphone className="h-4 w-4" /> },
    { key: 'maintenance', label: t('settings.nav.maintenance'), path: '/settings/maintenance', icon: <Wrench className="h-4 w-4" /> },
  ];

  const currentNavLabel = useMemo(() => navItems.find((item) => item.key === currentSection)?.label || t('settings.nav.title'), [currentSection, navItems, t]);

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="h-10 w-10 animate-spin rounded-full border-b-2 border-komgaPrimary" />
      </div>
    );
  }

  if (!config) {
    return <div className="p-8 text-center text-gray-500">{t('settings.loadFailed')}</div>;
  }

  return (
    <div className="mx-auto flex max-w-7xl gap-6 p-4 sm:p-8">
      <aside className="hidden w-72 shrink-0 lg:block">
        <div className="sticky top-8 space-y-4">
          <div className="rounded-2xl border border-gray-800 bg-komgaSurface p-5">
            <div className="flex items-center gap-3">
              <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/10 bg-white/[0.03]">
                <SettingsIcon className="h-5 w-5 text-komgaPrimary" />
              </div>
              <div>
                <h1 className="text-xl font-bold tracking-tight text-white">{t('settings.title')}</h1>
                <p className="mt-1 text-sm text-gray-400">{t('settings.subtitle')}</p>
              </div>
            </div>
            <div className={`mt-4 inline-flex items-center gap-2 rounded-full px-3 py-1.5 text-xs ${validation.valid ? 'border border-emerald-500/20 bg-emerald-500/10 text-emerald-300' : 'border border-amber-500/20 bg-amber-500/10 text-amber-300'}`}>
              {validation.valid ? t('settings.validationOk') : t('settings.validationIssues', { count: validation.issues.length })}
            </div>
          </div>

          <nav className="rounded-2xl border border-gray-800 bg-komgaSurface p-3">
            {navItems.map((item) => {
              const active = currentSection === item.key;
              return (
                <button
                  key={item.key}
                  type="button"
                  onClick={() => navigateSettingsSection(item.path)}
                  className={`mb-1 flex w-full items-center justify-between rounded-xl px-3 py-2.5 text-left text-sm transition-colors ${
                    active ? 'bg-komgaPrimary/10 text-komgaPrimary' : 'text-gray-300 hover:bg-gray-800 hover:text-white'
                  }`}
                >
                  <span className="flex items-center gap-3">
                    {item.icon}
                    {item.label}
                  </span>
                  {hasSectionChanges(item.key) ? <span className="h-2.5 w-2.5 rounded-full bg-amber-400" /> : null}
                </button>
              );
            })}
          </nav>
        </div>
      </aside>

      <div className="min-w-0 flex-1">
        <div className="mb-4 rounded-2xl border border-gray-800 bg-komgaSurface p-4 lg:hidden">
          <p className="text-sm text-gray-400">{t('settings.currentGroup')}</p>
          <div className="mt-2 flex flex-wrap gap-2">
            {navItems.map((item) => {
              const active = currentSection === item.key;
              return (
                <button
                  key={item.key}
                  type="button"
                  onClick={() => navigateSettingsSection(item.path)}
                  className={`rounded-full px-3 py-1.5 text-sm transition-colors ${active ? 'bg-komgaPrimary/10 text-komgaPrimary' : 'bg-gray-900 text-gray-300 hover:text-white'}`}
                >
                  {item.label}
                </button>
              );
            })}
          </div>
          <p className="mt-3 text-xs text-gray-500">{t('settings.currentGroupValue', { value: currentNavLabel })}</p>
        </div>

        <Outlet context={{ navigateSettingsSection }} />
      </div>

      <ConfirmDialog
        open={pendingTransition !== null}
        onClose={() => setPendingTransition(null)}
        onConfirm={() => {
          pendingTransition?.retry();
          setPendingTransition(null);
        }}
        title={t('settings.leaveTitle')}
        description={t('settings.leaveDescription')}
        confirmLabel={t('settings.leaveConfirm')}
        tone="warning"
      >
        <div className="rounded-xl border border-amber-500/20 bg-amber-500/10 p-4 text-sm text-amber-100">
          <div className="flex items-start gap-3">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <p>{t('settings.leaveHint')}</p>
          </div>
        </div>
      </ConfirmDialog>

      {toastMsg && (
        <div className={`fixed bottom-6 right-6 z-50 rounded-xl border px-4 py-3 text-sm shadow-xl ${toastMsg.type === 'success' ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-200' : 'border-red-500/30 bg-red-500/10 text-red-200'}`}>
          {toastMsg.text}
          <button onClick={() => setToastMsg(null)} className="ml-3 text-white/60 hover:text-white">
            ✕
          </button>
        </div>
      )}
    </div>
  );
}

export default function Settings() {
  return (
    <SettingsProvider>
      <SettingsLayoutInner />
    </SettingsProvider>
  );
}
