/**
 * 系列详情页顶部的快捷动作条。除导出 ComicInfo 外每一项后端都要管理员，故只对管理员渲染。
 */

import { Download, Edit, FileDown, FolderHeart, FolderOpen, RefreshCw, Save } from 'lucide-react';
import { Fragment, type ReactElement } from 'react';
import { useAuth } from '../../auth/AuthProvider';
import { useI18n } from '../../i18n/LocaleProvider';
import type { ScrapeProvider } from '../../hooks/useScrapeProviders';

interface SeriesQuickActionsProps {
  onEdit: () => void;
  onAddToCollection: () => void;
  onExportComicInfo: () => void;
  onWriteComicInfo: () => void;
  onOpenDirectory: () => void;
  onRescan: () => void;
  onScrape: (provider: string) => void;
  providers: ScrapeProvider[];
  scrapeMenuOpen: boolean;
  onToggleScrapeMenu: () => void;
  onCloseScrapeMenu: () => void;
  isOpeningDirectory: boolean;
  isRescanning: boolean;
  isScraping: boolean;
}

export function SeriesQuickActions({
  onEdit,
  onAddToCollection,
  onExportComicInfo,
  onWriteComicInfo,
  onOpenDirectory,
  onRescan,
  onScrape,
  providers,
  scrapeMenuOpen,
  onToggleScrapeMenu,
  onCloseScrapeMenu,
  isOpeningDirectory,
  isRescanning,
  isScraping,
}: SeriesQuickActionsProps) {
  const { t } = useI18n();
  const { isAdmin } = useAuth();

  const btn = 'p-2 text-gray-200 transition-colors first:rounded-l-xl last:rounded-r-xl';

  // 动作条按「留下来的项」拼装：分隔线若写死在按钮之间，隐藏管理动作就会在两端留下悬空竖线，
  // 圆角也会落在一个已经不存在的按钮上。导出是 GET，普通用户可用，其余在后端都要管理员。
  const items: ReactElement[] = [];
  if (isAdmin) {
    items.push(
      <button key="edit" onClick={onEdit} className={`${btn} hover:text-white hover:bg-white/10`} title={t('series.header.editMetadata')}>
        <Edit className="w-4 h-4 m-0.5" />
      </button>,
      <button key="collection" onClick={onAddToCollection} className={`${btn} hover:text-white hover:bg-white/10`} title={t('series.header.addToCollection')}>
        <FolderHeart className="w-4 h-4 m-0.5" />
      </button>,
    );
  }
  items.push(
    <button
      key="export"
      onClick={onExportComicInfo}
      className={`${btn} hover:text-komgaPrimary hover:bg-komgaPrimary/10`}
      title={t('series.header.exportComicInfo')}
    >
      <FileDown className="w-4 h-4 m-0.5" />
    </button>,
  );
  if (isAdmin) {
    items.push(
      <button key="write" onClick={onWriteComicInfo} className={`${btn} hover:text-komgaPrimary hover:bg-komgaPrimary/10`} title={t('series.header.writeComicInfo')}>
        <Save className="w-4 h-4 m-0.5" />
      </button>,
      <button
        key="open-dir"
        onClick={onOpenDirectory}
        disabled={isOpeningDirectory}
        className={`${btn} hover:text-komgaPrimary hover:bg-komgaPrimary/10 disabled:opacity-50`}
        title={t('series.header.openDirectory')}
      >
        <FolderOpen className={`w-4 h-4 m-0.5 ${isOpeningDirectory ? 'animate-pulse text-komgaPrimary' : ''}`} />
      </button>,
      <button
        key="rescan"
        onClick={onRescan}
        disabled={isRescanning}
        className={`${btn} hover:text-komgaSecondary hover:bg-komgaSecondary/10 disabled:opacity-50`}
        title={t('series.header.rescan')}
      >
        <RefreshCw className={`w-4 h-4 m-0.5 ${isRescanning ? 'animate-spin text-komgaSecondary' : ''}`} />
      </button>,
      <div key="scrape" className="relative flex">
        <button
          onClick={onToggleScrapeMenu}
          disabled={isScraping}
          className="p-2 text-gray-200 hover:text-komgaPrimary hover:bg-komgaPrimary/10 transition-colors disabled:opacity-50 rounded-r-xl"
          title={t('series.header.scrape')}
        >
          {isScraping ? (
            <div className="w-4 h-4 m-0.5 animate-spin rounded-full border-2 border-komgaPrimary border-t-transparent" />
          ) : (
            <Download className="w-4 h-4 m-0.5" />
          )}
        </button>
        {scrapeMenuOpen && !isScraping && (
          <>
            <div className="fixed inset-0 z-40" onClick={onCloseScrapeMenu} />
            <div className="absolute right-0 top-full mt-2 w-52 bg-komgaSurface border border-white/10 rounded-xl shadow-2xl z-50 overflow-hidden animate-in fade-in zoom-in duration-200">
              <div className="px-3 py-2 text-xs font-semibold text-gray-400 border-b border-white/5 bg-komgaSurface/50">
                {t('series.header.pickSource')}
              </div>
              {(providers.length > 0
                ? providers
                : [
                    { id: 'bangumi', name: 'Bangumi', description: '' },
                    { id: 'llm', name: t('series.header.ollama'), description: '' },
                  ]
              ).map((p) => (
                <button
                  key={p.id}
                  onClick={() => onScrape(p.id)}
                  title={p.description}
                  className="w-full text-left px-4 py-3 text-sm font-medium text-gray-100 hover:bg-komgaPrimary hover:text-white transition-colors border-t border-white/5 first:border-t-0"
                >
                  {p.name}
                </button>
              ))}
            </div>
          </>
        )}
      </div>,
    );
  }

  return (
    <div className="flex items-center border border-white/10 rounded-xl shadow-xs bg-komgaSurface/80 backdrop-blur-md">
      {items.map((item, index) => (
        <Fragment key={item.key}>
          {index > 0 && <div className="w-px h-5 bg-white/10 mx-1" />}
          {item}
        </Fragment>
      ))}
    </div>
  );
}
