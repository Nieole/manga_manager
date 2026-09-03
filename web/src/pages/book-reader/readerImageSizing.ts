/**
 * 阅读器向服务端要页图时的目标尺寸档位。重采样滤镜只是缩放用的插值核，服务端拿不到目标尺寸
 * 就无事可做，「让六个滤镜生效」等价于「把显示尺寸按档位告诉服务端」。
 * 档位、上限与 internal/images 的 SnapTargetDimension 必须是同一套。
 */

import type { ImageFilter, ReadMode, ScaleMode } from './types';

// READER_SIZE_STEP 是档位步长：请求的图略大于显示尺寸，浏览器再做一次小幅缩小，画质损失可忽略，
// 而缓存份数从「每个窗口宽度一份」收敛成每本书几档。
export const READER_SIZE_STEP = 256;

// READER_MAX_DIMENSION 是单边上限。超过显示需要的部分只是白付编码与带宽，4K 屏叠上 DPR 也不该
// 把一页拉成巨图；真实漫画页本身通常也没有这么宽。
export const READER_MAX_DIMENSION = 3072;

// READER_MAX_DPR 是 devicePixelRatio 的采纳上限。高 DPI 屏上 CSS 像素少于物理像素，不乘 DPR 的图
// 会被浏览器放大回去、白丢锐度；但 DPR 3 的手机再翻一倍只换来小屏上看不出的差别，却让档位翻番、
// 流量与缓存同步上涨。封顶 2 取的是这条曲线的拐点。
export const READER_MAX_DPR = 2;

// RESAMPLING_FILTERS 是六个纯重采样滤镜：它们要目标尺寸才有意义。
// nearest / average / bilinear 是纯 CSS 选项，waifu2x / realcugan 走 AI 放大，两类都不在此列。
const RESAMPLING_FILTERS: ImageFilter[] = ['bicubic', 'lanczos3', 'mitchell', 'lanczos2', 'bspline', 'catmullrom'];

export function isResamplingFilter(filter: ImageFilter): boolean {
  return (RESAMPLING_FILTERS as string[]).includes(filter);
}

export interface ReaderViewport {
  width: number;
  height: number;
  dpr: number;
}

// ReaderTargetSize 的 0 表示「这条边不作约束」，与服务端「缺省即未指定」同义。
export interface ReaderTargetSize {
  width: number;
  height: number;
}

export const NO_READER_TARGET_SIZE: ReaderTargetSize = { width: 0, height: 0 };

// snapReaderDimension 把一条边向上取整到档位，并夹到单边上限。
export function snapReaderDimension(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return 0;
  const snapped = Math.ceil(value / READER_SIZE_STEP) * READER_SIZE_STEP;
  return Math.min(snapped, READER_MAX_DIMENSION);
}

interface ReaderTargetSizeInput {
  readMode: ReadMode;
  scaleMode: ScaleMode;
  doublePage: boolean;
  viewport: ReaderViewport;
}

// readerTargetSize 按适应模式算出该下发哪几条边。
//
// original 不缩放，因此不给尺寸——那一档下重采样滤镜本就无事可做。适应宽度只由宽决定、
// 适应高度只由高决定、适应屏幕两条边一起给（服务端按框等比缩）。条漫无论选哪种适应模式都是
// 按宽铺满纵向滚动，高不构成约束，给了反而会把图缩得比显示尺寸还小。
export function readerTargetSize({ readMode, scaleMode, doublePage, viewport }: ReaderTargetSizeInput): ReaderTargetSize {
  if (scaleMode === 'original') return NO_READER_TARGET_SIZE;

  const dpr = Math.min(Math.max(viewport.dpr || 1, 1), READER_MAX_DPR);
  // 双页并排时每页只占一半宽度，档位要按单页可用宽度算。
  const width = snapReaderDimension((doublePage ? viewport.width / 2 : viewport.width) * dpr);
  const height = snapReaderDimension(viewport.height * dpr);

  if (readMode === 'webtoon') return { width, height: 0 };

  switch (scaleMode) {
    case 'fit-width':
      return { width, height: 0 };
    case 'fit-height':
      return { width: 0, height };
    case 'fit-screen':
    default:
      return { width, height };
  }
}
