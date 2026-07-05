/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

import type { Book } from '../types';

// 单位含日文卷字「巻」(U+5DFB) 与中文「卷」(U+5377)——否则「2019年 第1巻」会漏配序数正则、退化到抓首个数字(年份)。
const chineseOrdinalPattern = /第?\s*([零〇一二两三四五六七八九十百千万萬壹贰貳叁參肆伍陆陸柒捌玖拾佰仟]+)\s*(话|話|回|章|卷|巻|集|册|冊|期|部)/;

function parseChineseOrdinalNumber(value: string): number | null {
  const match = value.match(chineseOrdinalPattern);
  if (!match) return null;
  const digits: Record<string, number> = {
    零: 0, 〇: 0,
    一: 1, 壹: 1,
    二: 2, 两: 2, 贰: 2, 貳: 2,
    三: 3, 叁: 3, 參: 3,
    四: 4, 肆: 4,
    五: 5, 伍: 5,
    六: 6, 陆: 6, 陸: 6,
    七: 7, 柒: 7,
    八: 8, 捌: 8,
    九: 9, 玖: 9,
  };
  const units: Record<string, number> = { 十: 10, 拾: 10, 百: 100, 佰: 100, 千: 1000, 仟: 1000, 万: 10000, 萬: 10000 };
  let total = 0;
  let section = 0;
  let current = 0;
  for (const ch of match[1]) {
    if (ch in digits) {
      current = digits[ch];
      continue;
    }
    const unit = units[ch];
    if (!unit) return null;
    if (unit === 10000) {
      section += current;
      total += (section || 1) * unit;
      section = 0;
      current = 0;
    } else {
      section += (current || 1) * unit;
      current = 0;
    }
  }
  return total + section + current;
}

function extractOrdinalNumber(value: string): number | null {
  const arabicOrdinal = value.match(/第?\s*(\d+(\.\d+)?)\s*(话|話|回|章|卷|巻|集|册|冊|期|部)/);
  if (arabicOrdinal) return Number(arabicOrdinal[1]);
  const chineseOrdinal = parseChineseOrdinalNumber(value);
  if (chineseOrdinal !== null) return chineseOrdinal;
  const arabic = value.match(/\d+(\.\d+)?/);
  if (arabic) return Number(arabic[0]);
  return null;
}

export function compareOrdinalLabels(a: string, b: string) {
  const an = extractOrdinalNumber(a);
  const bn = extractOrdinalNumber(b);
  if (an !== null && bn !== null && an !== bn) return an - bn;
  if (an !== null && bn === null) return -1;
  if (an === null && bn !== null) return 1;
  return a.localeCompare(b, undefined, { numeric: true });
}

export function compareBooksForDisplay(a: Book, b: Book) {
  const volumeCmp = compareOrdinalLabels(a.volume || '', b.volume || '');
  if (volumeCmp !== 0) return volumeCmp;
  return compareOrdinalLabels(a.name, b.name);
}
