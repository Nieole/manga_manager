/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import type { ReactNode } from 'react';

interface ReaderImmersiveShellProps {
  visible: boolean;
  topBar: ReactNode;
  tray: ReactNode;
  onEdgeReveal: () => void;
}

export function ReaderImmersiveShell({ visible, topBar, tray, onEdgeReveal }: ReaderImmersiveShellProps) {
  return (
    <>
      <div
        aria-hidden
        onMouseEnter={onEdgeReveal}
        onTouchStart={onEdgeReveal}
        className={`absolute top-0 inset-x-0 h-10 z-30 ${visible ? 'pointer-events-none' : 'pointer-events-auto'}`}
      />
      <div
        className={`absolute top-0 inset-x-0 px-6 pt-4 pb-3 bg-linear-to-b from-komgaDark/90 via-komgaDark/55 to-transparent z-20 transition-all duration-300 ${
          visible ? 'opacity-100 translate-y-0' : 'opacity-0 -translate-y-4 pointer-events-none'
        }`}
      >
        {topBar}
      </div>
      <div
        aria-hidden
        onMouseEnter={onEdgeReveal}
        onTouchStart={onEdgeReveal}
        className={`absolute bottom-0 inset-x-0 h-12 z-30 ${visible ? 'pointer-events-none' : 'pointer-events-auto'}`}
      />
      <div
        className={`absolute bottom-0 inset-x-0 z-20 transition-all duration-300 ${
          visible ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-4 pointer-events-none'
        }`}
      >
        {tray}
      </div>
    </>
  );
}
