import { FolderOpen } from 'lucide-react';
import type { BrowseDirEntry, BrowseDrive } from './types';

interface DirectoryPickerProps {
  value: string;
  recentPaths: string[];
  onChange: (value: string) => void;
  browsing: boolean;
  browseCurrent: string;
  browseParent: string;
  browseDirs: BrowseDirEntry[];
  browseDrives: BrowseDrive[];
  onOpen: () => void;
  onClose: () => void;
  onChooseCurrent: () => void;
  onNavigate: (path: string) => void;
}

export function DirectoryPicker({
  value,
  onChange,
  browsing,
  browseCurrent,
  browseParent,
  browseDirs,
  browseDrives,
  recentPaths,
  onOpen,
  onClose,
  onChooseCurrent,
  onNavigate,
}: DirectoryPickerProps) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-400 mb-1">路径</label>
      <div className="flex gap-2">
        <input
          type="text"
          required
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="点击「浏览」选择文件夹"
          className="flex-1 bg-gray-900 border border-gray-800 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary focus:border-transparent transition-all"
        />
        <button
          type="button"
          onClick={onOpen}
          className="px-4 py-2.5 bg-gray-800 hover:bg-gray-700 text-white text-sm rounded-lg border border-gray-700 transition-colors whitespace-nowrap"
        >
          <FolderOpen className="w-4 h-4 inline mr-1" />
          浏览
        </button>
      </div>
      {recentPaths.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-2">
          {recentPaths.map((path) => (
            <button
              key={path}
              type="button"
              onClick={() => onChange(path)}
              className="rounded-full border border-gray-700 bg-gray-900 px-3 py-1 text-xs text-gray-400 hover:border-komgaPrimary/40 hover:text-white"
              title={path}
            >
              最近使用: {path}
            </button>
          ))}
        </div>
      )}
      {browsing && (
        <div className="mt-3 bg-gray-900 rounded-lg border border-gray-700 overflow-hidden">
          <div className="px-3 py-2 bg-gray-800 flex items-center justify-between text-xs">
            <span className="text-gray-400 truncate flex-1 mr-2" title={browseCurrent}>
              {browseCurrent}
            </span>
            <div className="flex gap-1">
              <button
                type="button"
                onClick={onChooseCurrent}
                className="px-2 py-1 bg-komgaPrimary hover:bg-komgaPrimaryHover text-white rounded text-xs transition-colors"
              >
                选择此目录
              </button>
              <button
                type="button"
                onClick={onClose}
                className="px-2 py-1 text-gray-400 hover:text-white transition-colors"
              >
                关闭
              </button>
            </div>
          </div>
          <div className="max-h-48 overflow-y-auto">
            {browseDrives.length > 0 && (
              <div className="px-3 py-2 flex flex-wrap gap-1 border-b border-gray-700">
                {browseDrives.map((drv) => (
                  <button
                    key={drv.path}
                    type="button"
                    onClick={() => onNavigate(drv.path)}
                    className={`px-2 py-1 text-xs rounded transition-colors ${
                      browseCurrent.startsWith(drv.path) || browseCurrent.startsWith(drv.name)
                        ? 'bg-komgaPrimary text-white'
                        : 'bg-gray-800 text-gray-400 hover:bg-gray-700 hover:text-white'
                    }`}
                  >
                    {drv.name}
                  </button>
                ))}
              </div>
            )}
            {browseCurrent !== browseParent && (
              <button
                type="button"
                onClick={() => onNavigate(browseParent)}
                className="w-full text-left px-3 py-2 text-sm text-yellow-400 hover:bg-gray-800 transition-colors flex items-center"
              >
                ↑ ..
              </button>
            )}
            {browseDirs.length === 0 ? (
              <div className="px-3 py-3 text-xs text-gray-500 text-center">此目录下无子文件夹</div>
            ) : (
              browseDirs.map((dir) => (
                <button
                  key={dir.path}
                  type="button"
                  onClick={() => onNavigate(dir.path)}
                  className="w-full text-left px-3 py-2 text-sm text-gray-300 hover:bg-gray-800 hover:text-komgaPrimary transition-colors flex items-center"
                >
                  <FolderOpen className="w-4 h-4 mr-2 text-komgaPrimary/60" />
                  {dir.name}
                </button>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}
