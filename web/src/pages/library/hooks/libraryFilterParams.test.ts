import { describe, expect, it } from 'vitest';
import {
  EMPTY_ADVANCED_FILTERS,
  VALID_SORT_DIRS,
  hasAdvancedFilters,
  parseFiltersFromSearch,
  parseNumberParam,
  parseReadStateParam,
  setOrDelete,
  supportsCursorPagination,
  type AdvancedFilters,
} from './libraryFilterParams';
import { DEFAULT_PAGE_SIZE } from '../types';

// 与 useLibraryFilters 写回 URL 时完全一致的高级筛选序列化，用于验证「序列化→解析」往返不丢字段。
function serializeAdvanced(params: URLSearchParams, a: AdvancedFilters) {
  setOrDelete(params, 'read', a.readState);
  setOrDelete(params, 'rmin', a.minRating !== null ? String(a.minRating) : null);
  setOrDelete(params, 'rmax', a.maxRating !== null ? String(a.maxRating) : null);
  setOrDelete(params, 'pmin', a.minProgress !== null ? String(a.minProgress) : null);
  setOrDelete(params, 'pmax', a.maxProgress !== null ? String(a.maxProgress) : null);
  setOrDelete(params, 'days', a.addedWithinDays !== null ? String(a.addedWithinDays) : null);
}

describe('parseNumberParam', () => {
  it('treats null / empty / whitespace as "no filter"', () => {
    expect(parseNumberParam(null)).toBeNull();
    expect(parseNumberParam('')).toBeNull();
    expect(parseNumberParam('   ')).toBeNull();
  });

  it('preserves zero as a real value (not null)', () => {
    // 边界：0 是合法评分/进度下界，必须与「未设置」区分。
    expect(parseNumberParam('0')).toBe(0);
  });

  it('parses integers, decimals and negatives', () => {
    expect(parseNumberParam('8')).toBe(8);
    expect(parseNumberParam('5.5')).toBe(5.5);
    expect(parseNumberParam('-3')).toBe(-3);
    expect(parseNumberParam('1e2')).toBe(100);
  });

  it('rejects non-finite / non-numeric input', () => {
    expect(parseNumberParam('abc')).toBeNull();
    expect(parseNumberParam('Infinity')).toBeNull();
    expect(parseNumberParam('NaN')).toBeNull();
  });
});

describe('parseReadStateParam', () => {
  it('accepts only the three known reading states', () => {
    expect(parseReadStateParam('unread')).toBe('unread');
    expect(parseReadStateParam('reading')).toBe('reading');
    expect(parseReadStateParam('completed')).toBe('completed');
  });

  it('rejects anything else', () => {
    expect(parseReadStateParam('done')).toBeNull();
    expect(parseReadStateParam('')).toBeNull();
    expect(parseReadStateParam(null)).toBeNull();
    expect(parseReadStateParam('UNREAD')).toBeNull();
  });
});

describe('setOrDelete', () => {
  it('sets non-empty values and deletes null / empty ones', () => {
    const p = new URLSearchParams('a=1&b=2&c=3');
    setOrDelete(p, 'a', 'x');
    setOrDelete(p, 'b', null);
    setOrDelete(p, 'c', '');
    expect(p.get('a')).toBe('x');
    expect(p.has('b')).toBe(false);
    expect(p.has('c')).toBe(false);
  });

  it('keeps the string "0" (truthy string, a valid bound)', () => {
    const p = new URLSearchParams();
    setOrDelete(p, 'rmin', '0');
    expect(p.get('rmin')).toBe('0');
  });
});

describe('supportsCursorPagination', () => {
  it('is true for cursor-capable sort fields (must match backend supportsCursor)', () => {
    expect(supportsCursorPagination('name')).toBe(true);
    expect(supportsCursorPagination('updated')).toBe(true);
    expect(supportsCursorPagination('created')).toBe(true);
    expect(supportsCursorPagination('favorite')).toBe(true);
    expect(supportsCursorPagination('books')).toBe(true);
    expect(supportsCursorPagination('volumes')).toBe(true);
    expect(supportsCursorPagination('pages')).toBe(true);
  });

  it('is false for offset-only sort fields (rating nullable, read per-user derived)', () => {
    expect(supportsCursorPagination('rating')).toBe(false);
    expect(supportsCursorPagination('read')).toBe(false);
    expect(supportsCursorPagination('')).toBe(false);
  });
});

describe('EMPTY_ADVANCED_FILTERS / hasAdvancedFilters', () => {
  it('EMPTY_ADVANCED_FILTERS has every dimension unset', () => {
    expect(EMPTY_ADVANCED_FILTERS).toEqual({
      readState: null,
      minRating: null,
      maxRating: null,
      minProgress: null,
      maxProgress: null,
      addedWithinDays: null,
    });
    expect(hasAdvancedFilters(EMPTY_ADVANCED_FILTERS)).toBe(false);
  });

  it('reports active when any single dimension is set', () => {
    expect(hasAdvancedFilters({ ...EMPTY_ADVANCED_FILTERS, readState: 'reading' })).toBe(true);
    expect(hasAdvancedFilters({ ...EMPTY_ADVANCED_FILTERS, maxRating: 9 })).toBe(true);
    expect(hasAdvancedFilters({ ...EMPTY_ADVANCED_FILTERS, addedWithinDays: 7 })).toBe(true);
  });

  it('treats a zero bound as an active filter (0 !== null)', () => {
    // 回归：minRating=0 / minProgress=0 是真实筛选，不能被当成「无筛选」。
    expect(hasAdvancedFilters({ ...EMPTY_ADVANCED_FILTERS, minRating: 0 })).toBe(true);
    expect(hasAdvancedFilters({ ...EMPTY_ADVANCED_FILTERS, minProgress: 0 })).toBe(true);
  });
});

describe('parseFiltersFromSearch', () => {
  it('returns null when no recognised query keys are present', () => {
    expect(parseFiltersFromSearch(new URLSearchParams(''))).toBeNull();
    expect(parseFiltersFromSearch(new URLSearchParams('foo=bar&baz=1'))).toBeNull();
  });

  it('returns a full intent object when at least one recognised key exists', () => {
    const r = parseFiltersFromSearch(new URLSearchParams('tag=action'));
    expect(r).not.toBeNull();
    expect(r?.activeTag).toBe('action');
    // 缺省字段回落到默认。
    expect(r?.activeAuthor).toBeNull();
    expect(r?.keyword).toBe('');
    expect(r?.sortByField).toBe('name');
    expect(r?.sortDir).toBe('asc');
    expect(r?.page).toBe(1);
    expect(r?.pageSize).toBe(DEFAULT_PAGE_SIZE);
    expect(r?.advanced).toEqual(EMPTY_ADVANCED_FILTERS);
  });

  it('maps simple string filters', () => {
    const r = parseFiltersFromSearch(new URLSearchParams('tag=t&author=a&status=ongoing&letter=A&q=hello'));
    expect(r?.activeTag).toBe('t');
    expect(r?.activeAuthor).toBe('a');
    expect(r?.activeStatus).toBe('ongoing');
    expect(r?.activeLetter).toBe('A');
    expect(r?.keyword).toBe('hello');
  });

  it('only accepts asc/desc for dir, else falls back to asc', () => {
    expect(parseFiltersFromSearch(new URLSearchParams('dir=desc'))?.sortDir).toBe('desc');
    expect(parseFiltersFromSearch(new URLSearchParams('dir=asc'))?.sortDir).toBe('asc');
    expect(parseFiltersFromSearch(new URLSearchParams('dir=sideways'))?.sortDir).toBe('asc');
    expect(parseFiltersFromSearch(new URLSearchParams('dir=DESC'))?.sortDir).toBe('asc');
  });

  it('clamps pageSize: non-positive / non-numeric fall back to the default', () => {
    expect(parseFiltersFromSearch(new URLSearchParams('size=50'))?.pageSize).toBe(50);
    expect(parseFiltersFromSearch(new URLSearchParams('size=0'))?.pageSize).toBe(DEFAULT_PAGE_SIZE);
    expect(parseFiltersFromSearch(new URLSearchParams('size=-5'))?.pageSize).toBe(DEFAULT_PAGE_SIZE);
    expect(parseFiltersFromSearch(new URLSearchParams('size=abc'))?.pageSize).toBe(DEFAULT_PAGE_SIZE);
    // parseInt tolerates a trailing suffix.
    expect(parseFiltersFromSearch(new URLSearchParams('size=50abc'))?.pageSize).toBe(50);
  });

  it('clamps page: non-positive / non-numeric fall back to 1', () => {
    expect(parseFiltersFromSearch(new URLSearchParams('page=4'))?.page).toBe(4);
    expect(parseFiltersFromSearch(new URLSearchParams('page=0'))?.page).toBe(1);
    expect(parseFiltersFromSearch(new URLSearchParams('page=-2'))?.page).toBe(1);
    expect(parseFiltersFromSearch(new URLSearchParams('page=xyz'))?.page).toBe(1);
  });

  it('parses advanced numeric filters, keeping zero and dropping invalids', () => {
    const r = parseFiltersFromSearch(new URLSearchParams('rmin=0&rmax=10&pmin=25&pmax=&days=abc&read=reading'));
    expect(r?.advanced).toEqual({
      readState: 'reading',
      minRating: 0, // zero preserved
      maxRating: 10,
      minProgress: 25,
      maxProgress: null, // empty -> null
      addedWithinDays: null, // non-numeric -> null
    });
  });

  it('ignores an unknown read state while still returning an object (key presence triggers parse)', () => {
    const r = parseFiltersFromSearch(new URLSearchParams('read=bogus'));
    expect(r).not.toBeNull();
    expect(r?.advanced.readState).toBeNull();
  });

  it('round-trips advanced filters through serialize -> parse without losing zero or null fields', () => {
    const original: AdvancedFilters = {
      readState: 'reading',
      minRating: 8,
      maxRating: null,
      minProgress: 0, // zero must survive the URL round-trip
      maxProgress: 100,
      addedWithinDays: 30,
    };
    const params = new URLSearchParams();
    serializeAdvanced(params, original);
    // sanity: null bound is absent from the URL entirely
    expect(params.has('rmax')).toBe(false);
    expect(params.get('pmin')).toBe('0');

    const parsed = parseFiltersFromSearch(params);
    expect(parsed?.advanced).toEqual(original);
  });

  it('VALID_SORT_DIRS exposes exactly asc and desc', () => {
    expect(VALID_SORT_DIRS.has('asc')).toBe(true);
    expect(VALID_SORT_DIRS.has('desc')).toBe(true);
    expect(VALID_SORT_DIRS.has('random')).toBe(false);
  });
});
