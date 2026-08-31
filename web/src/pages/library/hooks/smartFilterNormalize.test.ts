import { describe, expect, it } from 'vitest';
import {
  advancedFromSmartFilter,
  normalizeRemoteSmartFilter,
  smartFilterRequestBody,
  smartFilterToSnapshot,
} from './smartFilterNormalize';
import { EMPTY_ADVANCED_FILTERS } from './libraryFilterParams';
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

  it('defaults the six advanced dimensions to null when the row omits them', () => {
    // 六列全空是库里旧视图与旧本地缓存的真实形状。缺省必须是 null——
    // hasAdvancedFilters 判的是 !== null，undefined 会被当成「设了这一维」。
    const out = normalizeRemoteSmartFilter(remote({ id: '1', name: 'x' }));
    expect(out.readState).toBeNull();
    expect(out.minRating).toBeNull();
    expect(out.maxRating).toBeNull();
    expect(out.minProgress).toBeNull();
    expect(out.maxProgress).toBeNull();
    expect(out.addedWithinDays).toBeNull();
  });

  it('preserves provided advanced dimensions, zero included', () => {
    const out = normalizeRemoteSmartFilter(
      remote({ id: '1', name: 'x', readState: 'unread', minRating: 8, maxRating: 10, minProgress: 0, maxProgress: 50, addedWithinDays: 30 }),
    );
    expect(out.readState).toBe('unread');
    expect(out.minRating).toBe(8);
    expect(out.maxRating).toBe(10);
    // 0 是合法进度下界，不能被 ?? 之外的任何兜底吃成 null。
    expect(out.minProgress).toBe(0);
    expect(out.maxProgress).toBe(50);
    expect(out.addedWithinDays).toBe(30);
  });
});

describe('smartFilterRequestBody', () => {
  it('请求体逐字段对齐后端 smart_filters 的每一列', () => {
    // 与 internal/database/schema.sql 的列一一对应（后端 snake_case ↔ 此处 camelCase）：
    // active_tag / active_author / active_status / active_letter / read_state /
    // min_rating / max_rating / min_progress / max_progress / added_within_days /
    // sort_by_field / sort_dir / page_size，外加 name。id 与 created_at 由后端产生，不在请求体里。
    const body = smartFilterRequestBody(
      remote({ id: '1', name: '高分未读', readState: 'unread', minRating: 8, addedWithinDays: 30 }),
    );
    expect(Object.keys(body).sort()).toEqual(
      [
        'activeAuthor',
        'activeLetter',
        'activeStatus',
        'activeTag',
        'addedWithinDays',
        'maxProgress',
        'maxRating',
        'minProgress',
        'minRating',
        'name',
        'pageSize',
        'readState',
        'sortByField',
        'sortDir',
      ].sort(),
    );
    expect(body.readState).toBe('unread');
    expect(body.minRating).toBe(8);
    expect(body.addedWithinDays).toBe(30);
  });

  it('未设置的维度发成 null，不留 undefined 让字段在 JSON 里整个消失', () => {
    const body = smartFilterRequestBody(remote({ id: '1', name: 'x' }));
    expect(JSON.parse(JSON.stringify(body))).toMatchObject({
      readState: null,
      minRating: null,
      maxRating: null,
      minProgress: null,
      maxProgress: null,
      addedWithinDays: null,
    });
  });
});

describe('smartFilterToSnapshot', () => {
  it('应用视图时把六个高级筛选维度收回 advanced', () => {
    const snapshot = smartFilterToSnapshot(
      remote({ id: '1', name: 'x', activeTag: 'SF', readState: 'unread', minRating: 8, maxRating: null, minProgress: null, maxProgress: null, addedWithinDays: 30 }),
    );
    expect(snapshot.activeTag).toBe('SF');
    expect(snapshot.advanced).toEqual({
      readState: 'unread',
      minRating: 8,
      maxRating: null,
      minProgress: null,
      maxProgress: null,
      addedWithinDays: 30,
    });
  });

  it('六列皆空的旧视图收敛为「哪一维都不筛选」', () => {
    const snapshot = smartFilterToSnapshot(remote({ id: '1', name: 'x' }));
    expect(snapshot.advanced).toEqual(EMPTY_ADVANCED_FILTERS);
    expect(advancedFromSmartFilter(remote({ id: '1', name: 'x' }))).toEqual(EMPTY_ADVANCED_FILTERS);
  });

  it('关键字不属于视图条件，应用时一律清空', () => {
    expect(smartFilterToSnapshot(remote({ id: '1', name: 'x' })).keyword).toBe('');
  });
});
