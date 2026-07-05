/**
 * 业务说明：本文件为翻页阅读器提供“自由缩放 + 平移 + 点按消歧”能力（捏合 / 双击 / Ctrl+滚轮 / 拖拽），
 * 仅作用于 PagedReader；条漫（WebtoonReader）维持竖向滚动不引入缩放。
 * 它统一接管指针：单指点按经单/双击消歧后，双击缩放、单击回调交还调用方（翻页/中央切换）；
 * 双指捏合缩放；缩放态单指拖拽平移。缩放状态收在阅读器内部，通过 transform 变换渲染。
 * 维护时应关注：切页复位、缩放态与 1x 态的指针归属、Ctrl+滚轮需原生非被动监听才能 preventDefault。
 */

import { useCallback, useEffect, useRef, useState, type CSSProperties, type RefObject } from 'react';
import { clampZoom, toggleDoubleTapZoom, zoomFromPinch, zoomFromWheel } from './readerZoomMath';

const TAP_MOVE_TOLERANCE = 8; // px：位移小于此值才算“点按”
const TAP_MAX_DURATION = 400; // ms
const DOUBLE_TAP_WINDOW = 250; // ms：两次点按间隔小于此值算双击
const DOUBLE_TAP_DISTANCE = 40; // px
// 单击延迟执行以等待可能的第二次点按；取 = DOUBLE_TAP_WINDOW（最小安全值）。
// 若大于它，会出现 [DOUBLE_TAP_WINDOW, SINGLE_TAP_DELAY) 的空档：该区间内的第二次点按既不算双击、
// 又会取消前一次尚未触发的单击，导致丢失一次翻页（两次单击只翻一页）。取相等即消除该空档。
const SINGLE_TAP_DELAY = DOUBLE_TAP_WINDOW; // ms

interface PointerPos {
  x: number;
  y: number;
}

// useReaderZoom 管理翻页阅读器的缩放/平移与点按消歧。
// containerRef：挂载原生 wheel 监听（Ctrl+滚轮缩放需 passive:false）。
// resetKey：翻页 / 切换双页时变化，用于每页复位到 1x。
// onSingleTap：确认的单击（非双击、非拖拽）在 1x 态回调，传入 clientX 供调用方判定翻页/中央区。
export function useReaderZoom(
  containerRef: RefObject<HTMLDivElement | null>,
  resetKey: unknown,
  onSingleTap: (clientX: number) => void,
) {
  const [zoom, setZoom] = useState(1);
  const [offset, setOffset] = useState<PointerPos>({ x: 0, y: 0 });

  const pointersRef = useRef<Map<number, PointerPos>>(new Map());
  const pinchRef = useRef<{ dist: number; zoom: number } | null>(null);
  const panRef = useRef<{ x: number; y: number; ox: number; oy: number } | null>(null);
  const tapStartRef = useRef<{ x: number; y: number; t: number } | null>(null);
  const movedRef = useRef(false);
  const lastTapRef = useRef<{ x: number; t: number } | null>(null);
  const singleTapTimerRef = useRef<number | null>(null);

  // 镜像最新值，供原生 wheel 监听与稳定回调在不重挂的情况下读取。
  const zoomRef = useRef(1);
  zoomRef.current = zoom;
  const onSingleTapRef = useRef(onSingleTap);
  onSingleTapRef.current = onSingleTap;

  const applyZoom = useCallback((next: number) => {
    const clamped = clampZoom(next);
    setZoom(clamped);
    if (clamped <= 1) setOffset({ x: 0, y: 0 });
  }, []);
  const applyZoomRef = useRef(applyZoom);
  applyZoomRef.current = applyZoom;

  const clearSingleTapTimer = () => {
    if (singleTapTimerRef.current !== null) {
      window.clearTimeout(singleTapTimerRef.current);
      singleTapTimerRef.current = null;
    }
  };

  // 切页 / 双页切换时复位缩放，让每页从 1x 起。
  useEffect(() => {
    setZoom(1);
    setOffset({ x: 0, y: 0 });
    pointersRef.current.clear();
    pinchRef.current = null;
    panRef.current = null;
    movedRef.current = false;
    lastTapRef.current = null;
    clearSingleTapTimer();
  }, [resetKey]);

  // 组件卸载时清理待执行的单击定时器。
  useEffect(() => clearSingleTapTimer, []);

  // Ctrl+滚轮缩放：React 的 onWheel 为被动监听，preventDefault 无效（会触发整页缩放），故用原生非被动监听。
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const handler = (event: WheelEvent) => {
      if (!event.ctrlKey) return;
      event.preventDefault();
      applyZoomRef.current(zoomFromWheel(zoomRef.current, event.deltaY));
    };
    el.addEventListener('wheel', handler, { passive: false });
    return () => el.removeEventListener('wheel', handler);
  }, [containerRef]);

  // 指针处理返回 true 表示交互已被缩放消费，调用方据此跳过滚动拖拽。
  const onPointerDown = useCallback((event: React.PointerEvent): boolean => {
    pointersRef.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    movedRef.current = false;
    tapStartRef.current = { x: event.clientX, y: event.clientY, t: performance.now() };
    const pts = [...pointersRef.current.values()];
    if (pts.length === 2) {
      const dist = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
      pinchRef.current = { dist, zoom: zoomRef.current };
      panRef.current = null;
      return true;
    }
    if (zoomRef.current > 1) {
      panRef.current = { x: event.clientX, y: event.clientY, ox: offset.x, oy: offset.y };
      return true;
    }
    return false; // 1x 单指：可能是点按，也可能是滚动拖拽宽图 —— 不消费，交还调用方
  }, [offset]);

  const onPointerMove = useCallback((event: React.PointerEvent): boolean => {
    if (!pointersRef.current.has(event.pointerId)) return false;
    pointersRef.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    const pts = [...pointersRef.current.values()];
    if (pts.length === 2 && pinchRef.current) {
      const dist = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
      applyZoom(zoomFromPinch(pinchRef.current.zoom, pinchRef.current.dist, dist));
      movedRef.current = true;
      return true;
    }
    if (panRef.current && zoomRef.current > 1) {
      const dx = event.clientX - panRef.current.x;
      const dy = event.clientY - panRef.current.y;
      if (Math.abs(dx) > TAP_MOVE_TOLERANCE || Math.abs(dy) > TAP_MOVE_TOLERANCE) movedRef.current = true;
      setOffset({ x: panRef.current.ox + dx, y: panRef.current.oy + dy });
      return true;
    }
    // 1x 单指：记录是否移动（用于点按有效性判定），但不消费，让宽图滚动拖拽正常工作。
    const start = tapStartRef.current;
    if (start && (Math.abs(event.clientX - start.x) > TAP_MOVE_TOLERANCE || Math.abs(event.clientY - start.y) > TAP_MOVE_TOLERANCE)) {
      movedRef.current = true;
    }
    return false;
  }, [applyZoom]);

  const onPointerUp = useCallback((event: React.PointerEvent): boolean => {
    const wasMulti = pointersRef.current.size >= 2;
    const start = tapStartRef.current;
    pointersRef.current.delete(event.pointerId);
    if (pointersRef.current.size < 2) pinchRef.current = null;
    if (pointersRef.current.size === 0) panRef.current = null;
    tapStartRef.current = null;

    const isTap =
      !!start &&
      !wasMulti &&
      !movedRef.current &&
      Math.abs(event.clientX - start.x) < TAP_MOVE_TOLERANCE &&
      Math.abs(event.clientY - start.y) < TAP_MOVE_TOLERANCE &&
      performance.now() - start.t < TAP_MAX_DURATION;

    if (isTap) {
      const now = performance.now();
      const last = lastTapRef.current;
      const isDouble = !!last && now - last.t < DOUBLE_TAP_WINDOW && Math.abs(event.clientX - last.x) < DOUBLE_TAP_DISTANCE;
      if (isDouble) {
        clearSingleTapTimer();
        lastTapRef.current = null;
        applyZoom(toggleDoubleTapZoom(zoomRef.current)); // 双击缩放切换
        return true;
      }
      lastTapRef.current = { x: event.clientX, t: now };
      if (zoomRef.current === 1) {
        // 1x 单击延迟执行，等待可能的第二次点按（双击缩放）后再翻页/切换 UI。
        const clientX = event.clientX;
        clearSingleTapTimer();
        singleTapTimerRef.current = window.setTimeout(() => {
          singleTapTimerRef.current = null;
          onSingleTapRef.current(clientX);
        }, SINGLE_TAP_DELAY);
      }
      return true; // 点按已由本 hook 接管（含单击回调），调用方不再自行导航
    }

    return wasMulti || movedRef.current || zoomRef.current > 1;
  }, [applyZoom]);

  const wrapperStyle: CSSProperties = {
    transform: `translate(${offset.x}px, ${offset.y}px) scale(${zoom})`,
    transformOrigin: 'center center',
    willChange: 'transform',
  };

  return {
    zoom,
    isZoomed: zoom > 1,
    wrapperStyle,
    onPointerDown,
    onPointerMove,
    onPointerUp,
  };
}
