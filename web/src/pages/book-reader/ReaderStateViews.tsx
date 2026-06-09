/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import { ArrowLeft, Loader2, RefreshCw } from 'lucide-react';

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

interface ReaderErrorStateProps {
  t: Translate;
  message: string;
  onBackToSeries: () => void;
  onRetry: () => void;
}

export function ReaderEyeProtectionOverlay() {
  return (
    <div
      className="absolute inset-0 z-30 pointer-events-none"
      style={{
        background: 'rgba(255, 180, 50, 0.12)',
        mixBlendMode: 'multiply',
      }}
    />
  );
}

export function ReaderLoadingState() {
  return (
    <div className="flex items-center justify-center h-full">
      <Loader2 className="w-10 h-10 animate-spin text-komgaPrimary" />
    </div>
  );
}

export function ReaderErrorState({
  t,
  message,
  onBackToSeries,
  onRetry,
}: ReaderErrorStateProps) {
  return (
    <div className="flex h-full items-center justify-center px-6">
      <div className="max-w-xl rounded-2xl border border-red-500/20 bg-red-500/10 p-6 text-center">
        <p className="text-lg font-semibold text-white">{t('reader.error.title')}</p>
        <p className="mt-3 text-sm leading-7 text-red-100/90">{message}</p>
        <div className="mt-6 flex flex-col sm:flex-row items-center justify-center gap-3">
          <button
            onClick={onRetry}
            className="inline-flex items-center gap-2 rounded-xl bg-white/10 px-4 py-2 text-sm text-white hover:bg-white/15"
          >
            <RefreshCw className="w-4 h-4" />
            {t('reader.retry')}
          </button>
          <button
            onClick={onBackToSeries}
            className="inline-flex items-center gap-2 rounded-xl bg-komgaPrimary px-4 py-2 text-sm text-white hover:bg-komgaPrimaryHover"
          >
            <ArrowLeft className="w-4 h-4" />
            {t('reader.backToSeries')}
          </button>
        </div>
      </div>
    </div>
  );
}
