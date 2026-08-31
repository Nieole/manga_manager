import { describe, expect, it } from 'vitest';
import { buildBoundedRangeLabel } from './rangeLabel';

describe('buildBoundedRangeLabel', () => {
  it('renders a dash only for a two-sided range', () => {
    expect(buildBoundedRangeLabel(8, 10)).toBe('8–10');
  });
  // 回归：单边区间不得出现悬空/前导连字符。
  it('uses >= for a min-only bound (no trailing dash)', () => {
    expect(buildBoundedRangeLabel(8, null)).toBe('≥8');
  });
  it('uses <= for a max-only bound (no leading dash)', () => {
    expect(buildBoundedRangeLabel(null, 6)).toBe('≤6');
  });
  it('applies the suffix on both bounds', () => {
    expect(buildBoundedRangeLabel(50, 80, '%')).toBe('50%–80%');
    expect(buildBoundedRangeLabel(50, null, '%')).toBe('≥50%');
    expect(buildBoundedRangeLabel(null, 80, '%')).toBe('≤80%');
  });
  it('is empty when neither bound is set', () => {
    expect(buildBoundedRangeLabel(null, undefined)).toBe('');
  });
});

describe('buildBoundedRangeLabel edge cases', () => {
  it('treats a zero bound as a real value, not "unset"', () => {
    // 回归：0 用 != null 判定，min=0 必须渲染成「≥0」而非空串。
    expect(buildBoundedRangeLabel(0, null)).toBe('≥0');
    expect(buildBoundedRangeLabel(null, 0)).toBe('≤0');
    expect(buildBoundedRangeLabel(0, 5)).toBe('0–5');
    expect(buildBoundedRangeLabel(0, null, '%')).toBe('≥0%');
  });

  it('renders negative and decimal bounds verbatim', () => {
    expect(buildBoundedRangeLabel(-2, 3)).toBe('-2–3');
    expect(buildBoundedRangeLabel(1.5, 2.5)).toBe('1.5–2.5');
  });

  it('does not reorder an inverted range (renders bounds as given)', () => {
    // 组件不校验 min<=max，标签按传入值原样输出。
    expect(buildBoundedRangeLabel(9, 1)).toBe('9–1');
  });

  it('treats explicit undefined the same as null on either side', () => {
    expect(buildBoundedRangeLabel(undefined, undefined)).toBe('');
    expect(buildBoundedRangeLabel(undefined, 6)).toBe('≤6');
    expect(buildBoundedRangeLabel(6, undefined)).toBe('≥6');
  });
});
