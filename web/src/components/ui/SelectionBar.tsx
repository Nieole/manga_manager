/**
 * 业务说明：本文件是业务实现，属于前端共享组件层，负责沉淀按钮、面板、列表、封面、进度和反馈等可复用 UI 片段。
 * 它让资料库、阅读器、设置和系列详情在视觉和交互上保持一致。
 * 维护时应关注组件职责边界、可访问性、主题变量、加载态和不同页面的复用语义。
 */

import type { ReactNode } from 'react';

export interface SelectionBarAction {
  key: string;
  label: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  variant?: 'default' | 'primary' | 'success' | 'warning' | 'danger' | 'info';
  icon?: ReactNode;
}

interface SelectionBarProps {
  visible: boolean;
  count: number;
  countLabel: ReactNode;
  actions: SelectionBarAction[];
}

const VARIANT_CLASS: Record<NonNullable<SelectionBarAction['variant']>, string> = {
  default: 'bg-gray-800 hover:bg-gray-700 text-gray-300 border border-gray-700',
  primary: 'bg-komgaPrimary/10 hover:bg-komgaPrimary/20 text-komgaPrimary border border-komgaPrimary/30',
  success: 'bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-300 border border-emerald-500/30',
  warning: 'bg-amber-500/10 hover:bg-amber-500/20 text-amber-300 border border-amber-500/30',
  danger: 'bg-red-500/10 hover:bg-red-500/20 text-red-500 border border-red-500/30',
  info: 'bg-blue-500/10 hover:bg-blue-500/20 text-blue-300 border border-blue-500/30',
};

/**
 * SelectionBar：底部居中悬浮的批量操作栏。在多选场景与活跃选区数 > 0 时显示。
 * 提取自 Home.tsx，资源库 / 系列详情 / 阅读列表均可复用。
 */
export function SelectionBar({ visible, count, countLabel, actions }: SelectionBarProps) {
  if (!visible || count === 0) return null;
  return (
    <div className="fixed bottom-8 left-1/2 -translate-x-1/2 w-max max-w-[95vw] bg-gray-900 border border-gray-700 shadow-[0_20px_50px_-12px_rgba(0,0,0,0.8)] rounded-2xl px-4 sm:px-6 py-4 flex flex-wrap justify-center items-center gap-4 sm:gap-6 z-50 animate-in slide-in-from-bottom-5">
      <span className="text-white font-medium text-sm whitespace-nowrap shrink-0">{countLabel}</span>
      <div className="flex flex-wrap items-center justify-center gap-2 sm:gap-3">
        {actions.map((action) => (
          <button
            key={action.key}
            onClick={action.onClick}
            disabled={action.disabled}
            className={`px-4 py-2 rounded-lg text-sm font-medium transition-colors flex items-center gap-2 whitespace-nowrap disabled:opacity-40 disabled:cursor-not-allowed ${VARIANT_CLASS[action.variant ?? 'default']}`}
          >
            {action.icon}
            <span>{action.label}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
