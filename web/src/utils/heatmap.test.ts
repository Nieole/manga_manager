import { afterEach, describe, expect, it, vi } from 'vitest';
import { buildHeatmapCells, dayOfWeekFromDateStr, formatHeatmapMonthLabel, monthIndexFromDateStr } from './heatmap';

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

describe('热力图日期网格', () => {
  const origTZ = process.env.TZ;

  afterEach(() => {
    vi.useRealTimers();
    process.env.TZ = origTZ;
  });

  // 每个格子的行位置由 dayOfWeek 决定、取数由 date 决定。两者若来自不同日历，
  // 用户会看到自己的阅读记录落在错误的星期几上。
  // 触发条件不是「非零时区必错」，而是「本地时刻跨过 UTC 日界」——
  // UTC+8 是当地 08:00 之后、UTC-5 是当地 19:00 之后，也就是大半天。
  const cases = [
    { name: 'UTC+8 当地凌晨（已跨过 UTC 日界）', tz: 'Asia/Shanghai', at: '2026-07-28T17:00:00Z' },
    { name: 'UTC-5 当地深夜（已跨过 UTC 日界）', tz: 'America/New_York', at: '2026-07-30T01:00:00Z' },
    { name: 'UTC 本身', tz: 'UTC', at: '2026-07-29T12:00:00Z' },
    { name: 'UTC+8 当地下午（未跨界）', tz: 'Asia/Shanghai', at: '2026-07-29T09:00:00Z' },
  ];

  for (const tc of cases) {
    it(`${tc.name}：每个格子的星期与自身日期一致`, () => {
      process.env.TZ = tc.tz;
      vi.useFakeTimers();
      vi.setSystemTime(new Date(tc.at));

      const mismatched = buildHeatmapCells(28).filter(
        (cell) => cell.dayOfWeek !== dayOfWeekFromDateStr(cell.date),
      );
      expect(mismatched.map((c) => `${c.date}=${c.dayOfWeek}`)).toEqual([]);
    });
  }

  it('起点对齐到周一，终点是今天', () => {
    process.env.TZ = 'Asia/Shanghai';
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-28T17:00:00Z'));

    const cells = buildHeatmapCells(28);
    expect(cells[0].dayOfWeek).toBe(1);
    expect(cells[cells.length - 1].date).toBe('2026-07-28');
  });
});
