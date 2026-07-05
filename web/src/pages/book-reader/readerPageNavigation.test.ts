import { describe, expect, it } from 'vitest';
import { lastPageIndex, nextPageIndex, pageNumberToIndex, prevPageIndex } from './readerPageNavigation';

describe('nextPageIndex', () => {
  it('advances by one in single-page mode', () => {
    expect(nextPageIndex(0, 10, false)).toEqual({ index: 1, atEnd: false });
    expect(nextPageIndex(8, 10, false)).toEqual({ index: 9, atEnd: false });
  });

  it('advances by two in double-page mode', () => {
    expect(nextPageIndex(0, 10, true)).toEqual({ index: 2, atEnd: false });
    expect(nextPageIndex(4, 10, true)).toEqual({ index: 6, atEnd: false });
  });

  // 回归：单页末页——prev+1 达到 pageCount 即触发 atEnd，索引停在末页不再前进。
  it('flags end-of-book and holds the index at the last single page', () => {
    expect(nextPageIndex(9, 10, false)).toEqual({ index: 9, atEnd: true });
  });

  // 回归：双页且总页为偶数时，末跨页(索引8显示9、10页)再翻页应 atEnd 并停在 8——
  // 这正是 reportedProgressPage 需要上报右页(第10页=page_count)的场景。
  it('flags end-of-book for an even page count and holds the last spread index', () => {
    expect(nextPageIndex(8, 10, true)).toEqual({ index: 8, atEnd: true });
  });

  // 双页且总页为奇数：从索引7(step2)会越过末页(7+2=9>=9)，停在 7 并 atEnd。
  it('flags end-of-book for an odd page count', () => {
    expect(nextPageIndex(7, 9, true)).toEqual({ index: 7, atEnd: true });
    expect(nextPageIndex(6, 9, true)).toEqual({ index: 8, atEnd: false });
  });

  it('treats an empty book as immediately at the end', () => {
    expect(nextPageIndex(0, 0, false)).toEqual({ index: 0, atEnd: true });
    expect(nextPageIndex(0, 0, true)).toEqual({ index: 0, atEnd: true });
  });
});

describe('prevPageIndex', () => {
  it('steps back by one in single-page mode and clamps at zero', () => {
    expect(prevPageIndex(9, false)).toBe(8);
    expect(prevPageIndex(1, false)).toBe(0);
    expect(prevPageIndex(0, false)).toBe(0);
  });

  it('steps back by two in double-page mode and clamps at zero', () => {
    expect(prevPageIndex(8, true)).toBe(6);
    expect(prevPageIndex(1, true)).toBe(0);
    expect(prevPageIndex(0, true)).toBe(0);
  });
});

describe('pageNumberToIndex', () => {
  it('converts a 1-based page number to a 0-based index', () => {
    expect(pageNumberToIndex(1, 10)).toBe(0);
    expect(pageNumberToIndex(10, 10)).toBe(9);
  });

  it('clamps out-of-range page numbers into the valid index range', () => {
    expect(pageNumberToIndex(99, 10)).toBe(9);
    expect(pageNumberToIndex(0, 10)).toBe(0);
    expect(pageNumberToIndex(-5, 10)).toBe(0);
  });

  it('degrades to 0 for an empty book', () => {
    expect(pageNumberToIndex(5, 0)).toBe(0);
  });
});

describe('lastPageIndex', () => {
  it('returns the final index, and 0 for an empty book', () => {
    expect(lastPageIndex(10)).toBe(9);
    expect(lastPageIndex(1)).toBe(0);
    expect(lastPageIndex(0)).toBe(0);
  });
});
