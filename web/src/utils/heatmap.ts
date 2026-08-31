/**
 * 活动热力图的日期工具。格子日期是**本地**日历日，与后端记录活动日期的口径一致（见 Go 侧 database.ActivityDayKey）；
 * 网格内部的日期算术挂在 UTC 上，只当不受夏令时干扰的日历坐标用。
 * 日期串一律按字面年月日处理，不能交给 new Date(dateStr) 解析——那会当成 UTC 午夜，负时区下月份读早一个月。
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
// 终点取**本地**今天：后端把阅读记在本地日历日，若按 UTC 今天收尾，UTC+8 当地 00:00–08:00
// 读的书在网格上没有格子可落。终点定下后网格一律用 UTC 坐标推进，日期串与星期几才出自同一套日历——
// 两者若分取本地与 UTC，本地时刻跨过 UTC 日界时整张网格会相对星期标签错开一行。
export function buildHeatmapCells(totalDays: number, now: Date = new Date()): HeatmapCell[] {
  const end = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate());
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
