/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

interface ReaderHelpPanelProps {
  t: Translate;
}

export function ReaderHelpPanel({ t }: ReaderHelpPanelProps) {
  return (
    <div className="self-end mt-3 bg-komgaSurface border border-gray-800 rounded-xl p-4 shadow-2xl w-[90vw] sm:w-80 max-w-sm text-sm text-gray-300 animate-in fade-in slide-in-from-top-4">
      <div className="space-y-3">
        <div>
          <p className="text-xs uppercase tracking-wider text-gray-500 mb-1">{t('reader.helpShortcuts')}</p>
          <p>{t('reader.helpArrowKeys')}</p>
          <p>{t('reader.helpPageKeys')}</p>
          <p>{t('reader.helpJumpKeys')}</p>
          <p>{t('reader.helpBookmarkKey')}</p>
          <p>{t('reader.helpToggleHelp')}</p>
        </div>
        <div>
          <p className="text-xs uppercase tracking-wider text-gray-500 mb-1">{t('reader.helpMobile')}</p>
          <p>{t('reader.helpMobileDescription')}</p>
        </div>
        <div>
          <p className="text-xs uppercase tracking-wider text-gray-500 mb-1">{t('reader.helpTroubleshooting')}</p>
          <p>{t('reader.helpTroubleshootingDescription')}</p>
        </div>
      </div>
    </div>
  );
}
