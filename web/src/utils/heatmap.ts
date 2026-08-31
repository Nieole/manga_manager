/**
 * 活动热力图的月份标签工具。热力图格子的日期串是 UTC（与后端 DATE('now') 一致），
 * 取月份必须直接按日期串的字面年月构造本地日期，不能用 new Date(dateStr).getMonth() 解析——
 * 该解析把 'YYYY-MM-DD' 当 UTC 午夜，在负时区（如纽约 UTC-5）会把本地月份读早一个月，
 * 导致月份表头错位一列。
 */

// monthIndexFromDateStr 从 'YYYY-MM-DD' 直接取 0 基月份（与时区无关）。
export function monthIndexFromDateStr(dateStr: string): number {
  return Number(dateStr.slice(5, 7)) - 1;
}

// formatHeatmapMonthLabel 用日期串的字面年月日构造本地日期来格式化「短月份名」，避免 UTC 解析的月份偏移。
export function formatHeatmapMonthLabel(dateStr: string, locale: string): string {
  const [y, m, d] = dateStr.split('-').map(Number);
  return new Intl.DateTimeFormat(locale, { month: 'short' }).format(new Date(y, m - 1, d));
}

// dayOfWeekFromDateStr 从 'YYYY-MM-DD' 取 0 基星期几（0=周日），与时区无关。
export function dayOfWeekFromDateStr(dateStr: string): number {
  const [y, m, d] = dateStr.split('-').map(Number);
  return new Date(Date.UTC(y, m - 1, d)).getUTCDay();
}

export interface HeatmapCell {
  date: string;
  dayOfWeek: number;
}

// buildHeatmapCells 生成「从今天往前 totalDays 天、起点对齐到周一」的日期网格。
//
// 日期串与星期几必须出自同一套日历（UTC），不能日期串取 toISOString 而行位置取本地 getDay()：
// 当本地时刻跨过 UTC 日界（UTC+8 当地 08:00 之后、UTC-5 当地 19:00 之后）时，两者会读出
// 不同的一天，整张网格相对星期标签错开一行，用户看到自己的阅读记录落在错误的星期几上。
export function buildHeatmapCells(totalDays: number, now: Date = new Date()): HeatmapCell[] {
  const end = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  const dayMs = 24 * 60 * 60 * 1000;

  let start = end - (totalDays - 1) * dayMs;
  // 对齐到周一：getUTCDay 里 0 是周日，往前退到本周一。
  const startDow = new Date(start).getUTCDay();
  start -= ((startDow + 6) % 7) * dayMs;

  const cells: HeatmapCell[] = [];
  for (let ts = start; ts <= end; ts += dayMs) {
    const d = new Date(ts);
    cells.push({ date: d.toISOString().slice(0, 10), dayOfWeek: d.getUTCDay() });
  }
  return cells;
}
