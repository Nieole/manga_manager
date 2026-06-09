/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useEffect } from 'react';

interface UseLibraryKeyboardParams {
  enabled: boolean;
  onFocusSearch: () => void;
  onJumpFirst: () => void;
  onJumpLast: () => void;
  onToggleSelection: () => void;
  onEscape: () => void;
}

/**
 * useLibraryKeyboard：资源库页面全局快捷键。仅在文档焦点不在输入控件上时生效。
 *   /  → 聚焦搜索
 *   g  → 跳到第一页
 *   G  → 跳到最后一页（shift+g）
 *   e  → 切换批量选择模式
 *   Escape → 退出选择模式 / 关闭抽屉
 */
export function useLibraryKeyboard({
  enabled,
  onFocusSearch,
  onJumpFirst,
  onJumpLast,
  onToggleSelection,
  onEscape,
}: UseLibraryKeyboardParams) {
  useEffect(() => {
    if (!enabled) return;
    const handler = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (target && isEditable(target) && event.key !== 'Escape') return;

      switch (event.key) {
        case '/':
          event.preventDefault();
          onFocusSearch();
          return;
        case 'g':
          if (event.shiftKey) {
            event.preventDefault();
            onJumpLast();
          } else {
            event.preventDefault();
            onJumpFirst();
          }
          return;
        case 'G':
          event.preventDefault();
          onJumpLast();
          return;
        case 'e':
        case 'E':
          event.preventDefault();
          onToggleSelection();
          return;
        case 'Escape':
          onEscape();
          return;
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [enabled, onFocusSearch, onJumpFirst, onJumpLast, onToggleSelection, onEscape]);
}

function isEditable(el: HTMLElement) {
  if (el.isContentEditable) return true;
  const tag = el.tagName.toLowerCase();
  if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
  return false;
}
