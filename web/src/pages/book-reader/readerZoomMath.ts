/**
 * 业务说明：翻页阅读器缩放的纯数值运算（便于测试）。
 * 从 useReaderZoom 抽出缩放钳制、双击缩放切换、Ctrl+滚轮与双指捏合的缩放换算，
 * 让这些无副作用的数值决策可被单测覆盖；hook 仍负责把结果接到 state 与指针状态。
 */

export const MIN_ZOOM = 1;
export const MAX_ZOOM = 4;
export const DOUBLE_TAP_ZOOM = 2.5;

// clampZoom 把任意目标缩放钳制到 [MIN_ZOOM, MAX_ZOOM]。NaN 会被 Math.max/Math.min 传导为 NaN，
// 与原实现一致（调用方不产生 NaN，无需在此额外处理）。
export function clampZoom(next: number): number {
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, next));
}

// toggleDoubleTapZoom：双击时在「1x」与「DOUBLE_TAP_ZOOM」间切换——已放大则回到 1x，否则放大。
export function toggleDoubleTapZoom(current: number): number {
  return current > 1 ? MIN_ZOOM : DOUBLE_TAP_ZOOM;
}

// zoomFromWheel：Ctrl+滚轮缩放，向上滚（deltaY<0）放大、向下滚缩小；步长与当前缩放成正比，
// 使高缩放下手感更快。返回未钳制值，交由 clampZoom 收敛。
export function zoomFromWheel(current: number, deltaY: number): number {
  return current - deltaY * 0.0025 * current;
}

// zoomFromPinch：双指捏合，按当前双指间距 / 起始间距的比例缩放起始缩放值。
// startDist 为 0 时以 1 兜底避免除零（与原实现一致）。返回未钳制值。
export function zoomFromPinch(startZoom: number, startDist: number, dist: number): number {
  const ratio = dist / (startDist || 1);
  return startZoom * ratio;
}
