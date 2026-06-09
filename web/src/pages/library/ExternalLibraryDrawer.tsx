/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { CheckCircle2, HardDrive, Loader2, X } from 'lucide-react';
import { createPortal } from 'react-dom';
import { DirectoryPicker } from '../../components/layout/DirectoryPicker';
import type { BrowseDirEntry, BrowseDrive } from '../../components/layout/types';
import { useI18n } from '../../i18n/LocaleProvider';
import type { ExternalSession } from './types';

interface ExternalLibraryDrawerProps {
  open: boolean;
  onClose: () => void;
  externalPath: string;
  externalIgnoreExtension: boolean;
  externalSession: ExternalSession | null;
  startingExternalScan: boolean;
  externalBrowsing: boolean;
  externalBrowseCurrent: string;
  externalBrowseParent: string;
  externalBrowseDirs: BrowseDirEntry[];
  externalBrowseDrives: BrowseDrive[];
  recentExternalPaths: string[];
  externalVisibilitySummary: { complete: number; partial: number; missing: number };
  onChangePath: (value: string) => void;
  onToggleIgnoreExtension: (value: boolean) => void;
  onOpenBrowse: () => void;
  onCloseBrowse: () => void;
  onChooseCurrentBrowse: () => void;
  onNavigateBrowse: (path: string) => void;
  onStartScan: () => void;
  onClearSession: () => void;
}

export function ExternalLibraryDrawer({
  open,
  onClose,
  externalPath,
  externalIgnoreExtension,
  externalSession,
  startingExternalScan,
  externalBrowsing,
  externalBrowseCurrent,
  externalBrowseParent,
  externalBrowseDirs,
  externalBrowseDrives,
  recentExternalPaths,
  externalVisibilitySummary,
  onChangePath,
  onToggleIgnoreExtension,
  onOpenBrowse,
  onCloseBrowse,
  onChooseCurrentBrowse,
  onNavigateBrowse,
  onStartScan,
  onClearSession,
}: ExternalLibraryDrawerProps) {
  const { t } = useI18n();

  if (typeof document === 'undefined') return null;

  return createPortal(
    <div className={`fixed inset-0 z-80 ${open ? '' : 'pointer-events-none'}`} aria-hidden={!open}>
      <div
        role="presentation"
        onClick={onClose}
        className={`absolute inset-0 backdrop-blur-xs transition-opacity duration-300 ${open ? 'opacity-100' : 'opacity-0'}`}
        style={{
          background:
            'radial-gradient(circle at top, rgb(var(--theme-glow) / 0.16), transparent 35%), linear-gradient(to bottom, rgb(var(--theme-overlay-top) / 0.78), rgb(var(--theme-overlay-bottom) / 0.88))',
        }}
      />
      <aside
        className={`absolute top-0 right-0 h-full w-full max-w-md bg-komgaSurface border-l border-gray-800 shadow-2xl transition-transform duration-300 ${
          open ? 'translate-x-0' : 'translate-x-full'
        }`}
      >
        <div className="flex h-full flex-col">
          <header className="flex items-center justify-between border-b border-gray-800 px-5 py-4">
            <div className="flex items-center gap-2 text-white">
              <HardDrive className="h-5 w-5 text-komgaPrimary" />
              <h3 className="text-lg font-semibold">{t('home.external.title')}</h3>
              {externalSession?.status === 'scanning' && <Loader2 className="ml-1 h-4 w-4 animate-spin text-blue-400" />}
              {externalSession?.status === 'ready' && <CheckCircle2 className="ml-1 h-4 w-4 text-emerald-400" />}
            </div>
            <button
              onClick={onClose}
              className="rounded-md p-1 text-gray-400 transition-colors hover:bg-gray-800 hover:text-white"
              aria-label={t('common.close')}
            >
              <X className="h-5 w-5" />
            </button>
          </header>
          <div className="flex-1 overflow-y-auto px-5 py-5 space-y-5">
            <p className="text-sm text-gray-400">{t('home.external.description')}</p>
            <DirectoryPicker
              value={externalPath}
              onChange={onChangePath}
              browsing={externalBrowsing}
              browseCurrent={externalBrowseCurrent}
              browseParent={externalBrowseParent}
              browseDirs={externalBrowseDirs}
              browseDrives={externalBrowseDrives}
              recentPaths={recentExternalPaths}
              onOpen={onOpenBrowse}
              onClose={onCloseBrowse}
              onChooseCurrent={onChooseCurrentBrowse}
              onNavigate={onNavigateBrowse}
            />

            <label className="inline-flex items-center gap-3 rounded-lg border border-gray-800 bg-gray-900/40 px-3 py-2 text-sm text-gray-300">
              <input
                type="checkbox"
                checked={externalIgnoreExtension}
                onChange={(event) => onToggleIgnoreExtension(event.target.checked)}
                className="h-4 w-4 rounded-sm border-gray-600 bg-gray-900 text-komgaPrimary focus:ring-komgaPrimary"
              />
              <span>{t('home.external.ignoreExtension')}</span>
            </label>

            <div className="flex flex-wrap gap-3">
              <button
                onClick={onStartScan}
                disabled={startingExternalScan || !externalPath.trim()}
                className="rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 px-4 py-2 text-sm font-medium text-komgaPrimary hover:bg-komgaPrimary/20 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {startingExternalScan ? t('home.external.startingScan') : t('home.external.scanAction')}
              </button>
              {externalSession && (
                <button
                  onClick={onClearSession}
                  className="rounded-lg border border-gray-700 bg-gray-900 px-4 py-2 text-sm font-medium text-gray-300 hover:border-gray-600 hover:text-white"
                >
                  {t('home.external.clearSession')}
                </button>
              )}
            </div>

            {externalSession && (
              <div className="rounded-xl border border-gray-800 bg-gray-900/40 px-4 py-3 text-sm text-gray-300">
                <div className="flex items-center gap-2">
                  {externalSession.status === 'scanning' ? (
                    <Loader2 className="h-4 w-4 animate-spin text-blue-400" />
                  ) : externalSession.status === 'ready' ? (
                    <CheckCircle2 className="h-4 w-4 text-emerald-400" />
                  ) : (
                    <HardDrive className="h-4 w-4 text-red-400" />
                  )}
                  <span className="font-medium text-white">
                    {externalSession.status === 'scanning'
                      ? t('home.external.statusScanning')
                      : externalSession.status === 'ready'
                        ? t('home.external.statusReady')
                        : t('home.external.statusFailed')}
                  </span>
                </div>
                <p className="mt-2 text-xs text-gray-400 break-all">{externalSession.external_path}</p>
                <p className="mt-2 text-xs text-gray-500">
                  {t('home.external.matchRule', {
                    rule: externalSession.ignore_extension
                      ? t('home.external.ignoreExtensionShort')
                      : t('home.external.keepExtensionShort'),
                  })}
                </p>
                <div className="mt-3 grid grid-cols-3 gap-3 text-xs">
                  <Cell label={t('home.external.scanned')} value={externalSession.scanned_files} />
                  <Cell label={t('home.external.matched')} value={`${externalSession.matched_books}/${externalSession.total_books}`} />
                  <Cell label={t('home.external.unmatched')} value={externalSession.unmatched_files} />
                </div>
                {externalSession.error && <p className="mt-3 text-xs text-red-300">{externalSession.error}</p>}
              </div>
            )}

            {externalSession && (
              <div className="rounded-xl border border-gray-800 bg-gray-900/60 px-4 py-3">
                <h4 className="text-sm font-semibold text-white">{t('home.external.currentPageTitle')}</h4>
                <p className="mt-1 text-xs text-gray-400">{t('home.external.currentPageDescription')}</p>
                <div className="mt-3 grid grid-cols-3 gap-3 text-xs">
                  <Pill tone="emerald" label={t('home.external.complete')} value={externalVisibilitySummary.complete} />
                  <Pill tone="amber" label={t('home.external.partial')} value={externalVisibilitySummary.partial} />
                  <Pill tone="gray" label={t('home.external.missing')} value={externalVisibilitySummary.missing} />
                </div>
              </div>
            )}
          </div>
        </div>
      </aside>
    </div>,
    document.body,
  );
}

function Cell({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <p className="text-gray-500">{label}</p>
      <p className="mt-1 font-semibold text-white">{value}</p>
    </div>
  );
}

function Pill({ label, value, tone }: { label: string; value: number; tone: 'emerald' | 'amber' | 'gray' }) {
  const palette = {
    emerald: 'border-emerald-500/20 bg-emerald-500/10 text-emerald-300',
    amber: 'border-amber-500/20 bg-amber-500/10 text-amber-300',
    gray: 'border-gray-700 bg-gray-900 text-gray-300',
  }[tone];
  return (
    <div className={`rounded-xl border px-3 py-2 ${palette}`}>
      <p>{label}</p>
      <p className="mt-1 text-xl font-semibold text-white">{value}</p>
    </div>
  );
}
