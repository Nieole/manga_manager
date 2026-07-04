/**
 * 业务说明：本文件是应用外壳的全局键盘快捷键 hook，统一处理搜索（⌘K / /）、快捷键面板（?）、
 * 侧栏折叠（[）、以及 g 前缀跳转（gh/gr/go/gc/gl/gs/gf）。在可编辑元素聚焦时不拦截按键。
 * 维护时应关注前缀超时清理、修饰键组合与目标可编辑判定。
 */

import { useEffect, useRef } from 'react';

interface LayoutShortcutHandlers {
  onOpenSearch: () => void;
  onToggleShortcuts: () => void;
  onCloseShortcuts: () => void;
  onToggleSidebar: () => void;
  onNavigate: (path: string) => void;
}

export function useLayoutShortcuts(handlers: LayoutShortcutHandlers) {
  // 用 ref 持有最新回调，使 keydown 监听器只安装一次（不随每次渲染重装），又总能调用到最新的处理函数。
  const handlersRef = useRef(handlers);
  handlersRef.current = handlers;

  useEffect(() => {
    let pendingPrefix: string | null = null;
    let pendingTimer: number | null = null;
    const isEditable = (target: EventTarget | null) => {
      if (!(target instanceof HTMLElement)) return false;
      const tag = target.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
      if (target.isContentEditable) return true;
      return false;
    };
    const clearPrefix = () => {
      pendingPrefix = null;
      if (pendingTimer) {
        window.clearTimeout(pendingTimer);
        pendingTimer = null;
      }
    };
    const handler = (e: KeyboardEvent) => {
      const h = handlersRef.current;
      if (e.metaKey || e.ctrlKey || e.altKey) {
        if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
          e.preventDefault();
          h.onOpenSearch();
        }
        return;
      }
      if (isEditable(e.target)) return;
      if (e.key === '?' || (e.shiftKey && e.key === '/')) {
        e.preventDefault();
        h.onToggleShortcuts();
        clearPrefix();
        return;
      }
      if (e.key === '/') {
        e.preventDefault();
        h.onOpenSearch();
        clearPrefix();
        return;
      }
      if (e.key === 'Escape') {
        h.onCloseShortcuts();
        clearPrefix();
        return;
      }
      if (e.key === '[') {
        e.preventDefault();
        h.onToggleSidebar();
        clearPrefix();
        return;
      }
      if (pendingPrefix === 'g') {
        const key = e.key.toLowerCase();
        const map: Record<string, string> = {
          h: '/',
          r: '/reviews',
          o: '/ops',
          c: '/collections',
          l: '/reading-lists',
          s: '/settings',
          f: '/offline',
        };
        if (map[key]) {
          e.preventDefault();
          h.onNavigate(map[key]);
        }
        clearPrefix();
        return;
      }
      if (e.key === 'g') {
        pendingPrefix = 'g';
        if (pendingTimer) window.clearTimeout(pendingTimer);
        pendingTimer = window.setTimeout(clearPrefix, 1200);
      }
    };
    window.addEventListener('keydown', handler);
    return () => {
      window.removeEventListener('keydown', handler);
      if (pendingTimer) window.clearTimeout(pendingTimer);
    };
  }, []);
}
