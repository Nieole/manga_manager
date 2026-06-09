/**
 * 业务说明：本文件是业务实现，属于前端共享组件层，负责沉淀按钮、面板、列表、封面、进度和反馈等可复用 UI 片段。
 * 它让资料库、阅读器、设置和系列详情在视觉和交互上保持一致。
 * 维护时应关注组件职责边界、可访问性、主题变量、加载态和不同页面的复用语义。
 */

export const modalPrimaryButtonClass =
  'inline-flex items-center justify-center gap-2 rounded-xl bg-komgaPrimary px-4 py-2.5 text-sm font-semibold text-white shadow-lg shadow-komgaPrimary/20 transition-all hover:bg-komgaPrimaryHover disabled:cursor-not-allowed disabled:opacity-50';

export const modalSecondaryButtonClass =
  'inline-flex items-center justify-center gap-2 rounded-xl border border-gray-700 bg-gray-900/70 px-4 py-2.5 text-sm font-medium text-gray-200 transition-all hover:border-gray-600 hover:bg-gray-800';

export const modalGhostButtonClass =
  'inline-flex items-center justify-center gap-2 rounded-xl px-4 py-2.5 text-sm font-medium text-gray-300 transition-all hover:bg-gray-800/80 hover:text-white';

export const modalInputClass =
  'w-full rounded-xl border border-gray-700 bg-gray-950/80 px-4 py-3 text-sm text-white placeholder-gray-500 shadow-inner shadow-black/20 outline-hidden transition-all focus:border-komgaPrimary/50 focus:ring-2 focus:ring-komgaPrimary/20';

export const modalTextareaClass = `${modalInputClass} min-h-[110px] resize-none`;

export const modalSelectClass = `${modalInputClass} cursor-pointer`;

export const modalSectionClass =
  'rounded-2xl border border-gray-800/90 bg-gray-950/50 p-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.03)]';

export const modalTagClass =
  'inline-flex items-center gap-1 rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 px-2.5 py-1 text-xs font-medium text-komgaPrimary';

export const modalSubtleTagClass =
  'inline-flex items-center gap-1 rounded-lg border border-gray-700 bg-gray-800/80 px-2.5 py-1 text-xs text-gray-300';
