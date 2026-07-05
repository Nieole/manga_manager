import { describe, expect, it } from 'vitest';
import { getVisiblePageNumbers } from './paginationRange';

describe('getVisiblePageNumbers', () => {
  it('returns every page when there are 5 or fewer', () => {
    expect(getVisiblePageNumbers(1, 1)).toEqual([1]);
    expect(getVisiblePageNumbers(2, 3)).toEqual([1, 2, 3]);
    expect(getVisiblePageNumbers(3, 5)).toEqual([1, 2, 3, 4, 5]);
  });

  it('centers a 5-wide window around the current page in the middle', () => {
    expect(getVisiblePageNumbers(5, 10)).toEqual([3, 4, 5, 6, 7]);
    expect(getVisiblePageNumbers(6, 20)).toEqual([4, 5, 6, 7, 8]);
  });

  it('clamps the window to the left edge near the start', () => {
    expect(getVisiblePageNumbers(1, 10)).toEqual([1, 2, 3, 4, 5]);
    expect(getVisiblePageNumbers(2, 10)).toEqual([1, 2, 3, 4, 5]);
  });

  it('clamps the window to the right edge near the end', () => {
    expect(getVisiblePageNumbers(10, 10)).toEqual([6, 7, 8, 9, 10]);
    expect(getVisiblePageNumbers(9, 10)).toEqual([6, 7, 8, 9, 10]);
  });

  it('sanitizes an out-of-range current page', () => {
    // page beyond the last -> treated as the last page.
    expect(getVisiblePageNumbers(100, 10)).toEqual([6, 7, 8, 9, 10]);
    // page below 1 -> treated as page 1.
    expect(getVisiblePageNumbers(0, 10)).toEqual([1, 2, 3, 4, 5]);
    expect(getVisiblePageNumbers(-4, 10)).toEqual([1, 2, 3, 4, 5]);
  });

  it('handles a 4-page list where the window equals the whole range', () => {
    expect(getVisiblePageNumbers(3, 4)).toEqual([1, 2, 3, 4]);
  });

  it('returns an empty window when there are zero pages', () => {
    expect(getVisiblePageNumbers(1, 0)).toEqual([]);
  });
});
