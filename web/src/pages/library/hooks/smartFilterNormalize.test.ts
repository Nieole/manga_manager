import { describe, expect, it } from 'vitest';
import { normalizeRemoteSmartFilter } from './smartFilterNormalize';
import { DEFAULT_PAGE_SIZE, type SavedSmartFilter } from '../types';

// 远端行常常字段缺省或类型不严格（如数字 id），用 partial + 断言构造这些「不干净」输入。
function remote(fields: Partial<Record<keyof SavedSmartFilter, unknown>>): SavedSmartFilter {
  return fields as unknown as SavedSmartFilter;
}

describe('normalizeRemoteSmartFilter', () => {
  it('coerces a numeric id to a string', () => {
    const out = normalizeRemoteSmartFilter(remote({ id: 123, name: 'x' }));
    expect(out.id).toBe('123');
    expect(typeof out.id).toBe('string');
  });

  it('defaults missing active* dimensions to null', () => {
    const out = normalizeRemoteSmartFilter(remote({ id: '1', name: 'x' }));
    expect(out.activeTag).toBeNull();
    expect(out.activeAuthor).toBeNull();
    expect(out.activeStatus).toBeNull();
    expect(out.activeLetter).toBeNull();
  });

  it('preserves provided active* values', () => {
    const out = normalizeRemoteSmartFilter(
      remote({ id: '1', name: 'x', activeTag: 'action', activeAuthor: 'ito', activeStatus: 'ongoing', activeLetter: 'B' }),
    );
    expect(out.activeTag).toBe('action');
    expect(out.activeAuthor).toBe('ito');
    expect(out.activeStatus).toBe('ongoing');
    expect(out.activeLetter).toBe('B');
  });

  it('falls back sortByField -> name and sortDir -> asc when empty', () => {
    const out = normalizeRemoteSmartFilter(remote({ id: '1', name: 'x', sortByField: '', sortDir: '' }));
    expect(out.sortByField).toBe('name');
    expect(out.sortDir).toBe('asc');
  });

  it('keeps explicit sort field and direction', () => {
    const out = normalizeRemoteSmartFilter(remote({ id: '1', name: 'x', sortByField: 'rating', sortDir: 'desc' }));
    expect(out.sortByField).toBe('rating');
    expect(out.sortDir).toBe('desc');
  });

  it('falls back pageSize to the default when zero or missing, keeps a real value', () => {
    expect(normalizeRemoteSmartFilter(remote({ id: '1', name: 'x', pageSize: 0 })).pageSize).toBe(DEFAULT_PAGE_SIZE);
    expect(normalizeRemoteSmartFilter(remote({ id: '1', name: 'x' })).pageSize).toBe(DEFAULT_PAGE_SIZE);
    expect(normalizeRemoteSmartFilter(remote({ id: '1', name: 'x', pageSize: 100 })).pageSize).toBe(100);
  });

  it('synthesizes a createdAt when missing and preserves an existing one', () => {
    const withDate = normalizeRemoteSmartFilter(remote({ id: '1', name: 'x', createdAt: '2024-01-02T00:00:00.000Z' }));
    expect(withDate.createdAt).toBe('2024-01-02T00:00:00.000Z');

    const withoutDate = normalizeRemoteSmartFilter(remote({ id: '1', name: 'x' }));
    expect(withoutDate.createdAt).not.toBe('');
    expect(Number.isNaN(Date.parse(withoutDate.createdAt))).toBe(false);
  });

  it('passes the name through unchanged', () => {
    expect(normalizeRemoteSmartFilter(remote({ id: '1', name: 'My Filter' })).name).toBe('My Filter');
  });
});
