/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import { useRef, useState, type MouseEvent } from 'react';

export function useReaderPointerDrag() {
  const containerRef = useRef<HTMLDivElement>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [dragStart, setDragStart] = useState({ x: 0, y: 0 });
  const [scrollStart, setScrollStart] = useState({ left: 0, top: 0 });

  const handleMouseDown = (event: MouseEvent<HTMLDivElement>) => {
    if (!containerRef.current) return;
    setIsDragging(true);
    setDragStart({ x: event.pageX, y: event.pageY });
    setScrollStart({
      left: containerRef.current.scrollLeft,
      top: containerRef.current.scrollTop,
    });
  };

  const handleMouseLeave = () => {
    setIsDragging(false);
  };

  const handleMouseUp = () => {
    setIsDragging(false);
  };

  const handleMouseMove = (event: MouseEvent<HTMLDivElement>) => {
    if (!isDragging || !containerRef.current) return;
    event.preventDefault();

    const dx = event.pageX - dragStart.x;
    const dy = event.pageY - dragStart.y;
    containerRef.current.scrollLeft = scrollStart.left - dx;
    containerRef.current.scrollTop = scrollStart.top - dy;
  };

  return {
    containerRef,
    isDragging,
    handleMouseDown,
    handleMouseLeave,
    handleMouseUp,
    handleMouseMove,
  };
}
