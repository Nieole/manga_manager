/**
 * 业务说明：本文件是业务实现，属于前端共享组件层，负责沉淀按钮、面板、列表、封面、进度和反馈等可复用 UI 片段。
 * 它让资料库、阅读器、设置和系列详情在视觉和交互上保持一致。
 * 维护时应关注组件职责边界、可访问性、主题变量、加载态和不同页面的复用语义。
 */

import type { ReactNode } from 'react';

interface PageShellProps {
  children: ReactNode;
  maxWidth?: '6xl' | '7xl' | 'full';
}

export function PageShell({ children, maxWidth = '7xl' }: PageShellProps) {
  const maxWidthClass = {
    '6xl': 'max-w-6xl',
    '7xl': 'max-w-7xl',
    full: 'max-w-[1600px]',
  }[maxWidth];

  return (
    <div className={`mx-auto ${maxWidthClass} space-y-6 p-4 sm:p-6 lg:p-8`}>
      {children}
    </div>
  );
}

interface PageHeaderProps {
  badge?: { icon: ReactNode; label: string };
  title: string;
  description?: string;
  actions?: ReactNode;
}

export function PageHeader({ badge, title, description, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
      <div>
        {badge && (
          <div className="inline-flex items-center gap-2 rounded-full border border-komgaPrimary/20 bg-komgaPrimary/10 px-3 py-1 text-xs font-medium text-komgaPrimary">
            {badge.icon}
            {badge.label}
          </div>
        )}
        <h1 className={`text-2xl font-bold tracking-tight text-white ${badge ? 'mt-3' : ''}`}>
          {title}
        </h1>
        {description && (
          <p className="mt-2 max-w-3xl text-sm leading-6 text-gray-400">{description}</p>
        )}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}
