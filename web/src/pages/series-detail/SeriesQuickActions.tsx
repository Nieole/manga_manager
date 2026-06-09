/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

import { Download, Edit, FileDown, FolderHeart, FolderOpen, RefreshCw } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';

interface SeriesQuickActionsProps {
  onEdit: () => void;
  onAddToCollection: () => void;
  onExportComicInfo: () => void;
  onOpenDirectory: () => void;
  onRescan: () => void;
  onScrape: (provider: string) => void;
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
  onOpenDirectory,
  onRescan,
  onScrape,
  scrapeMenuOpen,
  onToggleScrapeMenu,
  onCloseScrapeMenu,
  isOpeningDirectory,
  isRescanning,
  isScraping,
}: SeriesQuickActionsProps) {
  const { t } = useI18n();

  return (
    <div className="flex items-center border border-white/10 rounded-xl shadow-xs bg-komgaSurface/80 backdrop-blur-md">
      <button
        onClick={onEdit}
        className="p-2 text-gray-200 hover:text-white hover:bg-white/10 transition-colors rounded-l-xl"
        title={t('series.header.editMetadata')}
      >
        <Edit className="w-4 h-4 m-0.5" />
      </button>
      <div className="w-px h-5 bg-white/10 mx-1" />
      <button
        onClick={onAddToCollection}
        className="p-2 text-gray-200 hover:text-white hover:bg-white/10 transition-colors"
        title={t('series.header.addToCollection')}
      >
        <FolderHeart className="w-4 h-4 m-0.5" />
      </button>
      <div className="w-px h-5 bg-white/10 mx-1" />
      <button
        onClick={onExportComicInfo}
        className="p-2 text-gray-200 hover:text-komgaPrimary hover:bg-komgaPrimary/10 transition-colors"
        title={t('series.header.exportComicInfo')}
      >
        <FileDown className="w-4 h-4 m-0.5" />
      </button>
      <div className="w-px h-5 bg-white/10 mx-1" />
      <button
        onClick={onOpenDirectory}
        disabled={isOpeningDirectory}
        className="p-2 text-gray-200 hover:text-komgaPrimary hover:bg-komgaPrimary/10 transition-colors disabled:opacity-50"
        title={t('series.header.openDirectory')}
      >
        <FolderOpen className={`w-4 h-4 m-0.5 ${isOpeningDirectory ? 'animate-pulse text-komgaPrimary' : ''}`} />
      </button>
      <div className="w-px h-5 bg-white/10 mx-1" />
      <button
        onClick={onRescan}
        disabled={isRescanning}
        className="p-2 text-gray-200 hover:text-komgaSecondary hover:bg-komgaSecondary/10 transition-colors disabled:opacity-50"
        title={t('series.header.rescan')}
      >
        <RefreshCw className={`w-4 h-4 m-0.5 ${isRescanning ? 'animate-spin text-komgaSecondary' : ''}`} />
      </button>
      <div className="w-px h-5 bg-white/10 mx-1" />
      <div className="relative flex">
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
            <div className="absolute right-0 top-full mt-2 w-48 bg-komgaSurface border border-white/10 rounded-xl shadow-2xl z-50 overflow-hidden animate-in fade-in zoom-in duration-200">
              <div className="px-3 py-2 text-xs font-semibold text-gray-400 border-b border-white/5 bg-komgaSurface/50">
                {t('series.header.pickSource')}
              </div>
              <button
                onClick={() => onScrape('bangumi')}
                className="w-full text-left px-4 py-3 text-sm font-medium text-gray-100 hover:bg-komgaPrimary hover:text-white transition-colors"
              >
                {t('series.header.bangumiRecommended')}
              </button>
              <button
                onClick={() => onScrape('llm')}
                className="w-full text-left px-4 py-3 text-sm font-medium text-gray-100 hover:bg-komgaPrimary hover:text-white transition-colors border-t border-white/5"
              >
                {t('series.header.ollama')}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
