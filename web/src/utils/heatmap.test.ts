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

  // --- added coverage: every month boundary + edge days ---

  it('returns the correct 0-based index for every month', () => {
    const expected = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11];
    for (let m = 1; m <= 12; m++) {
      const mm = String(m).padStart(2, '0');
      expect(monthIndexFromDateStr(`2026-${mm}-06`)).toBe(expected[m - 1]);
    }
  });

  it('slices month out of the string and ignores the day/year segments', () => {
    // slice(5,7) must pick the month field regardless of the day value.
    expect(monthIndexFromDateStr('2000-07-01')).toBe(6);
    expect(monthIndexFromDateStr('2099-07-31')).toBe(6);
    // A leading zero month parses as its numeric value, not octal.
    expect(monthIndexFromDateStr('2026-08-09')).toBe(7);
    expect(monthIndexFromDateStr('2026-09-09')).toBe(8);
  });

  it('does not shift the month for late-in-month days (the tz regression)', () => {
    // The bug this file guards against pushed a day-30/31 date into the prior month
    // in negative timezones; using the literal fields keeps the month stable.
    expect(formatHeatmapMonthLabel('2026-06-30', 'en-US')).toBe('Jun');
    expect(formatHeatmapMonthLabel('2026-03-31', 'en-US')).toBe('Mar');
    expect(formatHeatmapMonthLabel('2026-12-31', 'en-US')).toBe('Dec');
    expect(formatHeatmapMonthLabel('2026-01-01', 'en-US')).toBe('Jan');
  });

  it('formats short month names in en-US for each quarter boundary', () => {
    expect(formatHeatmapMonthLabel('2026-02-15', 'en-US')).toBe('Feb');
    expect(formatHeatmapMonthLabel('2026-07-15', 'en-US')).toBe('Jul');
    expect(formatHeatmapMonthLabel('2026-09-15', 'en-US')).toBe('Sep');
    expect(formatHeatmapMonthLabel('2026-11-15', 'en-US')).toBe('Nov');
  });

  it('honours a non-english locale for the short month name', () => {
    // zh-CN renders the short month as "<n>月"; the number must match the literal month field.
    expect(formatHeatmapMonthLabel('2026-03-01', 'zh-CN')).toBe('3月');
    expect(formatHeatmapMonthLabel('2026-11-30', 'zh-CN')).toBe('11月');
  });
});
