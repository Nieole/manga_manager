import { describe, expect, it } from 'vitest';
import {
  DOUBLE_TAP_ZOOM,
  MAX_ZOOM,
  MIN_ZOOM,
  clampZoom,
  toggleDoubleTapZoom,
  zoomFromPinch,
  zoomFromWheel,
} from './readerZoomMath';

describe('clampZoom', () => {
  it('never drops below the minimum', () => {
    expect(clampZoom(0.5)).toBe(MIN_ZOOM);
    expect(clampZoom(-3)).toBe(MIN_ZOOM);
    expect(clampZoom(MIN_ZOOM)).toBe(MIN_ZOOM);
  });

  it('never exceeds the maximum', () => {
    expect(clampZoom(10)).toBe(MAX_ZOOM);
    expect(clampZoom(MAX_ZOOM)).toBe(MAX_ZOOM);
  });

  it('passes through in-range values untouched', () => {
    expect(clampZoom(2.5)).toBe(2.5);
    expect(clampZoom(3)).toBe(3);
  });
});

describe('toggleDoubleTapZoom', () => {
  // 已放大 → 回到 1x；处于 1x → 放大到 DOUBLE_TAP_ZOOM。
  it('zooms in from 1x and out from any zoomed state', () => {
    expect(toggleDoubleTapZoom(1)).toBe(DOUBLE_TAP_ZOOM);
    expect(toggleDoubleTapZoom(2.5)).toBe(MIN_ZOOM);
    expect(toggleDoubleTapZoom(4)).toBe(MIN_ZOOM);
  });

  it('treats anything strictly above 1 as zoomed (boundary)', () => {
    expect(toggleDoubleTapZoom(1.0001)).toBe(MIN_ZOOM);
  });
});

describe('zoomFromWheel', () => {
  // deltaY<0（向上滚）放大，deltaY>0（向下滚）缩小。
  it('zooms in on scroll-up and out on scroll-down', () => {
    expect(zoomFromWheel(1, -100)).toBeCloseTo(1.25, 6);
    expect(zoomFromWheel(2, 100)).toBeCloseTo(1.5, 6);
  });

  it('scales the step proportionally to the current zoom', () => {
    // 相同 deltaY 在更高缩放下步长更大：zoom 4 时减 1.0，zoom 1 时仅减 0.25。
    expect(zoomFromWheel(4, 100)).toBeCloseTo(3, 6);
    expect(zoomFromWheel(1, 100)).toBeCloseTo(0.75, 6);
  });

  it('leaves zoom unchanged when there is no wheel delta', () => {
    expect(zoomFromWheel(2, 0)).toBe(2);
  });
});

describe('zoomFromPinch', () => {
  it('scales the start zoom by the distance ratio', () => {
    expect(zoomFromPinch(1, 100, 200)).toBeCloseTo(2, 6);
    expect(zoomFromPinch(2, 100, 50)).toBeCloseTo(1, 6);
  });

  it('is a no-op when the pinch distance is unchanged', () => {
    expect(zoomFromPinch(1.5, 120, 120)).toBeCloseTo(1.5, 6);
  });

  // 起始间距为 0 时以 1 兜底避免除零（返回未钳制值，交由 clampZoom 收敛）。
  it('guards against a zero starting distance', () => {
    expect(zoomFromPinch(1, 0, 200)).toBeCloseTo(200, 6);
  });
});
