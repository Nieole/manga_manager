import { describe, expect, it } from 'vitest';
import { reportedProgressPage } from './readerProgress';

const pages = Array.from({ length: 10 }, (_, i) => ({ number: i + 1 })); // pages 1..10

describe('reportedProgressPage', () => {
  // 回归：双页模式末页跨页(indices 8,9 = pages 9,10)，currentPageIndex 停在 8，
  // 必须上报更靠后的第 10 页(=page_count)，否则书永远不算读完。
  it('reports the rightmost page of the spread in paged double-page mode', () => {
    expect(reportedProgressPage(pages, 8, true, true)).toBe(10);
  });

  it('reports the current page in single-page mode', () => {
    expect(reportedProgressPage(pages, 8, true, false)).toBe(9);
  });

  it('does not overshoot past the last page for an odd final page', () => {
    const odd = Array.from({ length: 9 }, (_, i) => ({ number: i + 1 })); // 1..9
    expect(reportedProgressPage(odd, 8, true, true)).toBe(9);
  });

  it('ignores double-page in webtoon mode', () => {
    expect(reportedProgressPage(pages, 8, false, true)).toBe(9);
  });

  it('returns null when the index has no page', () => {
    expect(reportedProgressPage(pages, 99, true, true)).toBeNull();
  });

  it('returns null for a negative index or an empty page list', () => {
    expect(reportedProgressPage(pages, -1, true, true)).toBeNull();
    expect(reportedProgressPage([], 0, true, true)).toBeNull();
  });

  // 双页首跨页：应上报右页(第2页)，而非当前左页(第1页)。
  it('reports the rightmost page of the first spread in paged double-page mode', () => {
    expect(reportedProgressPage(pages, 0, true, true)).toBe(2);
  });

  // 上报的是页「号」而非索引：页号非连续时不能用 index+1 代替。
  it('reports the page number, not the index, for non-contiguous page numbers', () => {
    const custom = [{ number: 5 }, { number: 6 }, { number: 7 }];
    expect(reportedProgressPage(custom, 0, true, true)).toBe(6); // right page of spread
    expect(reportedProgressPage(custom, 1, true, false)).toBe(6); // single page -> current
  });

  // 双页但只剩一页(奇数末页在索引处)：右页钳制回当前页，不越界。
  it('clamps the spread partner to the current page when there is no partner', () => {
    expect(reportedProgressPage(pages, 9, true, true)).toBe(10); // last index, partner clamps to itself
    const single = [{ number: 1 }];
    expect(reportedProgressPage(single, 0, true, true)).toBe(1);
  });

  it('reports the current page in webtoon single-page mode', () => {
    expect(reportedProgressPage(pages, 3, false, false)).toBe(4);
  });
});
