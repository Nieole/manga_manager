/**
 * 守页图目标尺寸的档位规则：档位化决定缓存收敛成几份，适应模式决定哪几条边下发。
 * 破了不报错，只表现为「滤镜白选」或「同一本书每个窗口宽度各存一份」。
 */

import { describe, expect, it } from 'vitest';
import {
  READER_MAX_DIMENSION,
  isResamplingFilter,
  readerTargetSize,
  snapReaderDimension,
} from './readerImageSizing';

const viewport = { width: 1100, height: 900, dpr: 1 };

describe('档位取整', () => {
  it('向上取整到 256 的倍数，相邻容器宽度落进同一档', () => {
    expect(snapReaderDimension(1100)).toBe(1280);
    expect(snapReaderDimension(1250)).toBe(1280);
    expect(snapReaderDimension(1400)).toBe(1536);
    expect(snapReaderDimension(256)).toBe(256);
  });

  it('非正数与非法值不构成约束', () => {
    expect(snapReaderDimension(0)).toBe(0);
    expect(snapReaderDimension(-10)).toBe(0);
    expect(snapReaderDimension(Number.NaN)).toBe(0);
  });

  it('单边夹到上限，4K 屏叠 DPR 也拉不出巨图', () => {
    expect(snapReaderDimension(6000)).toBe(READER_MAX_DIMENSION);
  });
});

describe('各适应模式下发的边', () => {
  it('original 不缩放，因此一条边都不给', () => {
    expect(readerTargetSize({ readMode: 'paged', scaleMode: 'original', doublePage: false, viewport }))
      .toEqual({ width: 0, height: 0 });
  });

  it('适应宽度只由宽决定，适应高度只由高决定', () => {
    expect(readerTargetSize({ readMode: 'paged', scaleMode: 'fit-width', doublePage: false, viewport }))
      .toEqual({ width: 1280, height: 0 });
    expect(readerTargetSize({ readMode: 'paged', scaleMode: 'fit-height', doublePage: false, viewport }))
      .toEqual({ width: 0, height: 1024 });
  });

  it('适应屏幕两条边一起给，服务端按框等比缩', () => {
    expect(readerTargetSize({ readMode: 'paged', scaleMode: 'fit-screen', doublePage: false, viewport }))
      .toEqual({ width: 1280, height: 1024 });
  });

  it('双页并排时每页只占一半宽度', () => {
    expect(readerTargetSize({ readMode: 'paged', scaleMode: 'fit-width', doublePage: true, viewport }))
      .toEqual({ width: 768, height: 0 });
  });

  it('条漫按宽铺满纵向滚动，高不构成约束', () => {
    expect(readerTargetSize({ readMode: 'webtoon', scaleMode: 'fit-screen', doublePage: false, viewport }))
      .toEqual({ width: 1280, height: 0 });
  });
});

describe('devicePixelRatio', () => {
  it('高 DPI 屏按物理像素要图，否则浏览器再放大回去会丢锐度', () => {
    expect(readerTargetSize({ readMode: 'paged', scaleMode: 'fit-width', doublePage: false, viewport: { ...viewport, dpr: 2 } }))
      .toEqual({ width: 2304, height: 0 });
  });

  it('DPR 封顶 2：再高只让档位翻番，小屏上看不出差别', () => {
    const capped = readerTargetSize({ readMode: 'paged', scaleMode: 'fit-width', doublePage: false, viewport: { width: 400, height: 800, dpr: 3 } });
    // 按 2 倍算是 800 → 档位 1024；照 3 倍算会是 1200 → 档位 1280。
    expect(capped).toEqual({ width: 1024, height: 0 });
  });
});

describe('哪些滤镜要目标尺寸', () => {
  it('只有六个重采样滤镜要', () => {
    for (const filter of ['bicubic', 'lanczos3', 'mitchell', 'lanczos2', 'bspline', 'catmullrom'] as const) {
      expect(isResamplingFilter(filter)).toBe(true);
    }
    // 纯 CSS 的三项与 AI 放大那条支路都不在此列。
    for (const filter of ['none', 'nearest', 'average', 'bilinear', 'waifu2x', 'realcugan'] as const) {
      expect(isResamplingFilter(filter)).toBe(false);
    }
  });
});
