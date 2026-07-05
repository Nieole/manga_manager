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
