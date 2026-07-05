/**
 * 业务说明：翻页阅读器的页码索引运算（纯函数，便于测试）。
 * 从 useReaderPageNavigation 抽出「下一页 / 上一页 / 跳页 / 末页」的边界计算，
 * 让双页步进（step=2）、末页判定（是否触发续读下一本）、跳页越界钳制等逻辑可被单测覆盖。
 * hook 仅负责把这些结果接到 React state / 副作用，运算本身保持无副作用、行为不变。
 */

// nextPageIndex 计算翻到下一页后的目标索引。
// step：双页模式步进 2，否则 1。
// atEnd=true 表示已到达/越过末页边界（prev+step >= pageCount）——调用方据此决定是否续读下一本，
// 此时索引保持 prev（不前进）。未到末页时钳制到 [.., pageCount-1]。
export function nextPageIndex(
  prev: number,
  pageCount: number,
  doublePage: boolean,
): { index: number; atEnd: boolean } {
  const step = doublePage ? 2 : 1;
  if (prev + step >= pageCount) {
    return { index: prev, atEnd: true };
  }
  return { index: Math.min(prev + step, pageCount - 1), atEnd: false };
}

// prevPageIndex 计算翻到上一页后的目标索引，下界钳制到 0。
export function prevPageIndex(prev: number, doublePage: boolean): number {
  const step = doublePage ? 2 : 1;
  return Math.max(prev - step, 0);
}

// pageNumberToIndex 把 1 基页号转换为 0 基索引，并钳制到 [0, pageCount-1]；
// pageCount=0 时退化为 0（空书无可用页）。
export function pageNumberToIndex(pageNumber: number, pageCount: number): number {
  return Math.max(0, Math.min(pageCount - 1, pageNumber - 1));
}

// lastPageIndex 返回末页索引（空书为 0）。
export function lastPageIndex(pageCount: number): number {
  return Math.max(0, pageCount - 1);
}
