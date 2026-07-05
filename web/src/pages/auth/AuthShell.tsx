/**
 * 业务说明：本文件是登录 / 首次建管理员 / 强制改密等全屏鉴权页的共享外壳：居中卡片 + 品牌标题。
 * 这些页面在 AuthGate 未放行前渲染，独立于主 Layout，故自带背景与排版。
 * 维护要点：保持与全站暗色主题（komgaSurface / komgaPrimary）一致，卡片窄栏、表单可键盘操作。
 */

import type { ReactNode } from 'react';
import { BookMarked } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';

export const authInputClassName =
  'w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2.5 text-white placeholder:text-gray-500 focus:outline-hidden focus:ring-2 focus:ring-komgaPrimary/40 transition-all';

export const authLabelClassName = 'block text-sm font-medium text-gray-300 mb-1.5';

export function AuthShell({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle?: string;
  children: ReactNode;
}) {
  const { t } = useI18n();
  return (
    <div className="min-h-screen flex items-center justify-center bg-komgaBackground px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="flex items-center justify-center gap-2 mb-6 text-komgaPrimary">
          <BookMarked className="h-6 w-6" />
          <span className="text-lg font-semibold text-white">{t('app.name')}</span>
        </div>
        <div className="bg-komgaSurface border border-gray-800 rounded-2xl p-6 shadow-lg shadow-black/20">
          <h1 className="text-xl font-semibold text-white text-center text-balance">{title}</h1>
          {subtitle ? <p className="mt-1.5 text-sm text-gray-400 text-center">{subtitle}</p> : null}
          <div className="mt-6">{children}</div>
        </div>
      </div>
    </div>
  );
}
