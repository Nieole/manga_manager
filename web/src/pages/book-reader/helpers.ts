import type { CSSProperties } from 'react';
import type { ImageFilter, Page, ReadDirection, ScaleMode } from './types';

export function getPagedImages(
  pages: Page[],
  currentPageIndex: number,
  doublePage: boolean,
  readDirection: ReadDirection,
) {
  if (pages.length === 0) return [];

  const current = pages[currentPageIndex];
  if (!doublePage) return [current];

  if (currentPageIndex + 1 < pages.length) {
    const next = pages[currentPageIndex + 1];
    return readDirection === 'ltr' ? [current, next] : [next, current];
  }

  return [current];
}

export function getScaleClasses(
  scaleMode: ScaleMode,
  doublePage: boolean,
  baseClasses: string,
) {
  let classes = `${baseClasses} block m-0 p-0`;
  switch (scaleMode) {
    case 'original':
      classes += ' w-auto h-auto max-w-none max-h-none';
      break;
    case 'fit-width':
      if (doublePage) {
        classes += ' w-[50vw] h-auto object-contain';
      } else {
        classes += ' w-full h-auto object-contain';
      }
      break;
    case 'fit-screen':
      if (doublePage) {
        classes += ' h-full w-auto object-contain max-w-[50vw]';
      } else {
        classes += ' w-full h-full object-contain';
      }
      break;
    case 'fit-height':
    default:
      if (doublePage) {
        classes += ' h-full w-auto object-contain max-w-[50vw]';
      } else {
        classes += ' h-full w-auto object-contain max-w-none';
      }
      break;
  }
  return classes;
}

export function getFilterStyle(imageFilter: ImageFilter): CSSProperties {
  switch (imageFilter) {
    case 'nearest':
      return { imageRendering: 'pixelated' };
    case 'average':
    case 'bilinear':
      return { imageRendering: 'auto' };
    case 'bicubic':
    case 'lanczos3':
    case 'mitchell':
    case 'lanczos2':
    case 'bspline':
    case 'catmullrom':
    case 'waifu2x':
    case 'realcugan':
      return { imageRendering: 'high-quality' as CSSProperties['imageRendering'] };
    default:
      return {};
  }
}
