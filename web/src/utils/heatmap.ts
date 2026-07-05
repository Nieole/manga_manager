/**
 * 业务说明：活动热力图的月份标签工具。热力图格子的日期串是 UTC（与后端 DATE('now') 一致），
 * 但月份标签此前用 new Date(dateStr).getMonth() 重新解析——该解析把 'YYYY-MM-DD' 当 UTC 午夜，
 * 在负时区（如纽约 UTC-5）读出的本地月份会偏到上一个月，导致月份表头错位一列。
 * 这里直接按日期串的字面年月构造本地日期/取月份，与时区无关。
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
