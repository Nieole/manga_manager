import { Loader2 } from 'lucide-react';
import { DirectoryPicker } from './DirectoryPicker';
import { DEFAULT_SCAN_INTERVAL } from './constants';
import type { BrowseDirEntry, BrowseDrive } from './types';
import { ModalShell } from '../ui/ModalShell';
import { modalGhostButtonClass, modalInputClass, modalPrimaryButtonClass, modalSectionClass } from '../ui/modalStyles';
import { useI18n } from '../../i18n/LocaleProvider';

interface LibraryFormModalProps {
  title: string;
  submitLabel: string;
  submittingLabel: string;
  open: boolean;
  name: string;
  path: string;
  autoScan: boolean;
  koreaderSyncEnabled: boolean;
  scanInterval: number;
  scanFormats: string;
  submitting: boolean;
  browsing: boolean;
  browseCurrent: string;
  browseParent: string;
  browseDirs: BrowseDirEntry[];
  browseDrives: BrowseDrive[];
  recentPaths: string[];
  supportedScanFormats: string;
  onClose: () => void;
  onSubmit: (e: React.FormEvent) => void;
  onNameChange: (value: string) => void;
  onPathChange: (value: string) => void;
  onAutoScanChange: (value: boolean) => void;
  onKOReaderSyncEnabledChange: (value: boolean) => void;
  onScanIntervalChange: (value: number) => void;
  onScanFormatsChange: (value: string) => void;
  onOpenDirectoryBrowser: () => void;
  onCloseDirectoryBrowser: () => void;
  onChooseCurrentDirectory: () => void;
  onNavigateDirectory: (path: string) => void;
}

export function LibraryFormModal({
  title,
  submitLabel,
  submittingLabel,
  open,
  name,
  path,
  autoScan,
  koreaderSyncEnabled,
  scanInterval,
  scanFormats,
  submitting,
  browsing,
  browseCurrent,
  browseParent,
  browseDirs,
  browseDrives,
  recentPaths,
  supportedScanFormats,
  onClose,
  onSubmit,
  onNameChange,
  onPathChange,
  onAutoScanChange,
  onKOReaderSyncEnabledChange,
  onScanIntervalChange,
  onScanFormatsChange,
  onOpenDirectoryBrowser,
  onCloseDirectoryBrowser,
  onChooseCurrentDirectory,
  onNavigateDirectory,
}: LibraryFormModalProps) {
  const { t } = useI18n();
  if (!open) return null;

  return (
    <ModalShell
      open={open}
      onClose={onClose}
      title={title}
      description={t('libraryForm.description')}
      size="compact"
      bodyClassName="pt-5"
      footer={
        <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
          <button type="button" onClick={onClose} className={modalGhostButtonClass}>
            {t('modal.cancel')}
          </button>
          <button type="submit" form="library-form-modal" disabled={submitting} className={modalPrimaryButtonClass}>
            {submitting ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                {submittingLabel}
              </>
            ) : (
              submitLabel
            )}
          </button>
        </div>
      }
    >
      <form id="library-form-modal" onSubmit={onSubmit} className="space-y-5">
          <div className={modalSectionClass}>
            <div className="space-y-4">
            <div>
              <label className="mb-1.5 block text-sm font-medium text-gray-300">{t('libraryForm.name')}</label>
              <input
                type="text"
                required
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
                placeholder={t('libraryForm.namePlaceholder')}
                className={modalInputClass}
              />
            </div>
            <DirectoryPicker
              value={path}
              recentPaths={recentPaths}
              onChange={onPathChange}
              browsing={browsing}
              browseCurrent={browseCurrent}
              browseParent={browseParent}
              browseDirs={browseDirs}
              browseDrives={browseDrives}
              onOpen={onOpenDirectoryBrowser}
              onClose={onCloseDirectoryBrowser}
              onChooseCurrent={onChooseCurrentDirectory}
              onNavigate={onNavigateDirectory}
            />
            </div>
          </div>

          <div className={`${modalSectionClass} space-y-4`}>
            <div>
              <p className="text-sm font-medium text-gray-200">{t('libraryForm.strategyTitle')}</p>
              <p className="mt-1 text-xs leading-5 text-gray-500">{t('libraryForm.strategyDescription')}</p>
            </div>

            <label className="flex cursor-pointer items-start gap-3 rounded-2xl border border-gray-800 bg-black/20 px-4 py-3">
              <input
                type="checkbox"
                checked={koreaderSyncEnabled}
                onChange={(e) => onKOReaderSyncEnabledChange(e.target.checked)}
                className="mt-0.5 h-4 w-4 rounded border-gray-700 bg-gray-800 text-komgaPrimary focus:ring-2 focus:ring-komgaPrimary"
              />
              <div>
                <p className="text-sm font-medium text-gray-200">{t('libraryForm.koreaderSyncTitle')}</p>
                <p className="mt-1 text-xs text-gray-500">{t('libraryForm.koreaderSyncDescription')}</p>
              </div>
            </label>

            <label className="flex cursor-pointer items-start gap-3 rounded-2xl border border-gray-800 bg-black/20 px-4 py-3">
              <input
                type="checkbox"
                checked={autoScan}
                onChange={(e) => onAutoScanChange(e.target.checked)}
                className="mt-0.5 h-4 w-4 rounded border-gray-700 bg-gray-800 text-komgaPrimary focus:ring-2 focus:ring-komgaPrimary"
              />
              <div>
                <p className="text-sm font-medium text-gray-200">{t('libraryForm.autoScanTitle')}</p>
                <p className="mt-1 text-xs text-gray-500">{t('libraryForm.autoScanDescription')}</p>
              </div>
            </label>
            {autoScan && (
              <div className="space-y-4 rounded-2xl border border-gray-800 bg-black/20 p-4">
                <div>
                  <label className="mb-1.5 block text-sm font-medium text-gray-300">{t('libraryForm.scanInterval')}</label>
                  <input
                    type="number"
                    min="1"
                    value={scanInterval}
                    onChange={(e) => onScanIntervalChange(parseInt(e.target.value) || DEFAULT_SCAN_INTERVAL)}
                    className={modalInputClass}
                  />
                </div>
                <div>
                  <label className="mb-1.5 block text-sm font-medium text-gray-300">{t('libraryForm.scanFormats')}</label>
                  <input
                    type="text"
                    value={scanFormats}
                    onChange={(e) => onScanFormatsChange(e.target.value)}
                    className={modalInputClass}
                  />
                  <p className="mt-2 text-xs text-gray-500">{t('libraryForm.supportedFormats', { formats: supportedScanFormats })}</p>
                </div>
              </div>
            )}
          </div>
      </form>
    </ModalShell>
  );
}
