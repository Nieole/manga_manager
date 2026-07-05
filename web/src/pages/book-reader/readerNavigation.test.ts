import { describe, expect, it } from 'vitest';
import { computeReaderBack } from './readerNavigation';

describe('computeReaderBack', () => {
  // 回归：从系列页(站内)进入阅读器后，返回必须走浏览器回退(navigate -1)而不是再 push 一个系列页——
  // 否则「阅读器→返回→系列页→再返回」会退回到阅读器而非资源库。
  it('goes back through history when there is in-app history (the reported bug fix)', () => {
    expect(computeReaderBack({ seriesId: 123, hasInAppHistory: true })).toEqual({ kind: 'history' });
  });

  it('goes back through history even when the series id is not yet known', () => {
    expect(computeReaderBack({ seriesId: null, hasInAppHistory: true })).toEqual({ kind: 'history' });
  });

  // 直达阅读器(深链/新标签，无站内历史)：导航到系列页。
  it('navigates to the series page when opened directly with no in-app history', () => {
    expect(computeReaderBack({ seriesId: 123, hasInAppHistory: false })).toEqual({
      kind: 'series',
      url: '/series/123',
    });
  });

  it('includes the volume anchor when the volume is known', () => {
    expect(computeReaderBack({ seriesId: 5, bookVolume: 'Vol 1', hasInAppHistory: false })).toEqual({
      kind: 'series',
      url: '/series/5?volume=Vol%201',
    });
  });

  it('falls back to home when there is neither series nor in-app history', () => {
    expect(computeReaderBack({ seriesId: null, hasInAppHistory: false })).toEqual({ kind: 'home' });
    expect(computeReaderBack({ seriesId: undefined, hasInAppHistory: false })).toEqual({ kind: 'home' });
    expect(computeReaderBack({ seriesId: '', hasInAppHistory: false })).toEqual({ kind: 'home' });
  });

  // 边界：seriesId 为 0 不属于「空」判定(=== null/undefined/'')，走系列页分支而非首页。
  it('treats a zero series id as a real series (not home)', () => {
    expect(computeReaderBack({ seriesId: 0, hasInAppHistory: false })).toEqual({
      kind: 'series',
      url: '/series/0',
    });
  });

  it('accepts a numeric string series id', () => {
    expect(computeReaderBack({ seriesId: '77', hasInAppHistory: false })).toEqual({
      kind: 'series',
      url: '/series/77',
    });
  });

  // 空/缺省 volume 不应追加锚点。
  it('omits the volume anchor when the volume is empty or null', () => {
    expect(computeReaderBack({ seriesId: 5, bookVolume: '', hasInAppHistory: false })).toEqual({
      kind: 'series',
      url: '/series/5',
    });
    expect(computeReaderBack({ seriesId: 5, bookVolume: null, hasInAppHistory: false })).toEqual({
      kind: 'series',
      url: '/series/5',
    });
  });

  it('percent-encodes special characters in the volume anchor', () => {
    expect(computeReaderBack({ seriesId: 5, bookVolume: 'Vol #3 & 4', hasInAppHistory: false })).toEqual({
      kind: 'series',
      url: '/series/5?volume=Vol%20%233%20%26%204',
    });
  });

  // in-app history 优先级最高：即便同时给了 series/volume，也应回退历史而非导航系列页。
  it('prioritizes history over a known series and volume', () => {
    expect(
      computeReaderBack({ seriesId: 9, bookVolume: 'Vol 2', hasInAppHistory: true }),
    ).toEqual({ kind: 'history' });
  });
});
