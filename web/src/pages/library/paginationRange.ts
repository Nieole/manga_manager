/**
 * 业务说明：本文件是资料库分页器的纯逻辑层，负责计算「当前页附近可见的页码窗口」。
 * 从 LibraryPagination 组件抽出为无 React 依赖的纯函数，便于单元测试窗口居中、首尾夹取等边界规则，
 * 防止后续调整分页 UI 时静默改坏页码窗口（越界、窗口不足 5 个、末页贴边等）。
 */

// getVisiblePageNumbers：返回围绕 page 居中、长度最多 5 的连续页码数组。
// 窗口整体夹取在 [1, totalPages]，末页附近会向左贴边以保持窗口长度。
export function getVisiblePageNumbers(page: number, totalPages: number): number[] {
  const visibleCount = Math.min(5, totalPages);
  const currentPage = Math.max(1, Math.min(page, totalPages));
  let startPage = currentPage - Math.floor(visibleCount / 2);
  startPage = Math.max(1, Math.min(startPage, totalPages - visibleCount + 1));

  return Array.from({ length: visibleCount }, (_, index) => startPage + index);
}
