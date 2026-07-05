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
});
