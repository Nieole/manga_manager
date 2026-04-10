import { Loader2, X } from 'lucide-react';
import { DirectoryPicker } from './DirectoryPicker';
import { DEFAULT_SCAN_INTERVAL } from './constants';
import type { BrowseDirEntry, BrowseDrive } from './types';

interface LibraryFormModalProps {
  title: string;
  submitLabel: string;
  submittingLabel: string;
  open: boolean;
  name: string;
  path: string;
  autoScan: boolean;
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
  onScanIntervalChange,
  onScanFormatsChange,
  onOpenDirectoryBrowser,
  onCloseDirectoryBrowser,
  onChooseCurrentDirectory,
  onNavigateDirectory,
}: LibraryFormModalProps) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-komgaSurface rounded-xl shadow-2xl border border-gray-800 w-full max-w-md overflow-hidden animate-in fade-in zoom-in duration-200">
        <div className="flex justify-between items-center p-6 border-b border-gray-800">
          <h3 className="text-xl font-semibold text-white">{title}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-white transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>
        <form onSubmit={onSubmit} className="p-6">
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-400 mb-1">名称</label>
              <input
                type="text"
                required
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
                placeholder="例如: 日漫收藏"
                className="w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary focus:border-transparent transition-all"
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

          <div className="mt-4 p-4 bg-gray-900 rounded-lg border border-gray-800 space-y-4">
            <label className="flex items-center space-x-3 cursor-pointer">
              <input
                type="checkbox"
                checked={autoScan}
                onChange={(e) => onAutoScanChange(e.target.checked)}
                className="form-checkbox h-4 w-4 text-komgaPrimary bg-gray-800 border-gray-700 rounded focus:ring-komgaPrimary focus:ring-2"
              />
              <span className="text-sm font-medium text-gray-300">开启后台自动轮次扫描监控</span>
            </label>
            {autoScan && (
              <>
                <div>
                  <label className="block text-sm font-medium text-gray-400 mb-1">循环触发扫描任务的间隔 (默认60分钟)</label>
                  <input
                    type="number"
                    min="1"
                    value={scanInterval}
                    onChange={(e) => onScanIntervalChange(parseInt(e.target.value) || DEFAULT_SCAN_INTERVAL)}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-komgaPrimary"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-400 mb-1">目标提取匹配类型 (英文逗号分隔)</label>
                  <input
                    type="text"
                    value={scanFormats}
                    onChange={(e) => onScanFormatsChange(e.target.value)}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-komgaPrimary"
                  />
                  <p className="mt-1 text-xs text-gray-500">当前受支持的格式：{supportedScanFormats}</p>
                </div>
              </>
            )}
          </div>

          <div className="mt-8 flex justify-end space-x-3">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-gray-400 hover:text-white transition-colors"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="px-6 py-2 bg-komgaPrimary hover:bg-purple-600 text-white text-sm font-medium rounded-lg shadow-lg flex items-center transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {submitting ? (
                <>
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                  {submittingLabel}
                </>
              ) : (
                submitLabel
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
