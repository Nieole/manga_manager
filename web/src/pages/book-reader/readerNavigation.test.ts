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
    expect(computeReaderBack({ seriesId: '', hasInAppHistory: false })).toEqual({ kind: 'home' });
  });
});
