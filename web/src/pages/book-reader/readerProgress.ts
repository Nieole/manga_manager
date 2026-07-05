/**
 * 业务说明：翻页阅读器的进度页号计算（纯函数，便于测试）。
 * 修复：双页模式下 handleNext 步进 2，末页(偶数总页)时 currentPageIndex 停在倒数第二页，
 * 若按 pages[currentPageIndex] 上报，last_read_page 永远到不了 page_count，书永远不算「读完」。
 * 故双页翻页模式上报当前跨页里更靠后那一页（用户其实已看到它）；单页/webtoon 照旧上报当前页。
 */

// reportedProgressPage 返回应上报给服务端的页号；无有效当前页时返回 null。
export function reportedProgressPage(
  pages: { number: number }[],
  index: number,
  paged: boolean,
  doublePage: boolean,
): number | null {
  const current = pages[index];
  if (!current) return null;
  if (paged && doublePage) {
    const right = pages[Math.min(index + 1, pages.length - 1)];
    return right ? right.number : current.number;
  }
  return current.number;
}
