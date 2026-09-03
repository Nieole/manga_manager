import { useEffect, useMemo, useState } from 'react';
import { readerTargetSize, type ReaderTargetSize, type ReaderViewport } from './readerImageSizing';
import type { ReadMode, ScaleMode } from './types';

// VIEWPORT_SETTLE_MS 是窗口尺寸稳定多久才换档。拖窗口与转屏是连续事件，每一帧换档会把整批已
// 预加载的 URL 作废、触发一轮重下。档位本身有 256 像素的滞后，防抖补上跨档边界那一下的抖动。
const VIEWPORT_SETTLE_MS = 250;

function readViewport(): ReaderViewport {
  if (typeof window === 'undefined') return { width: 0, height: 0, dpr: 1 };
  return { width: window.innerWidth, height: window.innerHeight, dpr: window.devicePixelRatio || 1 };
}

// useReaderTargetSize 交出当前该向服务端请求的页图尺寸档位。
// 阅读器是沉浸式全屏，容器尺寸与窗口尺寸同量级，直接量窗口即可，无需把容器 ref 串到两个阅读器组件里。
export function useReaderTargetSize({ readMode, scaleMode, doublePage }: {
  readMode: ReadMode;
  scaleMode: ScaleMode;
  doublePage: boolean;
}): ReaderTargetSize {
  const [viewport, setViewport] = useState<ReaderViewport>(readViewport);

  useEffect(() => {
    if (typeof window === 'undefined') return undefined;
    let timer = 0;
    const schedule = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        setViewport((prev) => {
          const next = readViewport();
          // 尺寸没真变就保留原对象：URL 与预加载去重表都挂在这上面，白换一次身份会白重算一轮。
          return prev.width === next.width && prev.height === next.height && prev.dpr === next.dpr ? prev : next;
        });
      }, VIEWPORT_SETTLE_MS);
    };
    window.addEventListener('resize', schedule);
    window.addEventListener('orientationchange', schedule);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener('resize', schedule);
      window.removeEventListener('orientationchange', schedule);
    };
  }, []);

  return useMemo(
    () => readerTargetSize({ readMode, scaleMode, doublePage, viewport }),
    [doublePage, readMode, scaleMode, viewport],
  );
}
