import { describe, expect, it } from 'vitest';
import { monthIndexFromDateStr, formatHeatmapMonthLabel } from './heatmap';

describe('heatmap month labels', () => {
  // 回归：月份取值必须只看日期串字面，不受运行时区影响（此前 new Date('2026-06-01').getMonth() 在负时区读成 5 月）。
  it('reads the 0-based month straight from the date string, tz-independent', () => {
    expect(monthIndexFromDateStr('2026-06-01')).toBe(5); // June
    expect(monthIndexFromDateStr('2026-01-31')).toBe(0);
    expect(monthIndexFromDateStr('2026-12-15')).toBe(11);
  });

  it('formats the month name from the literal date, not a UTC-parsed one', () => {
    expect(formatHeatmapMonthLabel('2026-06-01', 'en-US')).toBe('Jun');
    expect(formatHeatmapMonthLabel('2026-01-01', 'en-US')).toBe('Jan');
  });
});
