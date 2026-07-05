/**
 * 业务说明：本 hook 统计「每本书的活跃阅读时长」（第 6 项）。以 1 秒为节拍累加"活跃阅读"秒数——
 * 仅当标签页可见且近期有过操作（翻页/滚动/鼠标/键盘/触摸，见 IDLE_MS）时才计入，切后台或长时间无操作即暂停。
 * 累加值经心跳、切书/卸载、以及标签页隐藏/关闭时上报到 POST /api/books/{id}/reading-time（服务端累加）。
 * 隐藏/关闭走 navigator.sendBeacon（卸载期 axios 无法可靠完成；该端点已豁免 CSRF，同源自动带会话 Cookie）。
 * 维护要点：每个 bookId 一个独立会话——effect 依赖 bookId，闭包内的 bookId 即本会话归属，切书时先结算旧书。
 */

import { useEffect, useRef } from 'react';
import { apiClient } from '../../api/client';

const TICK_MS = 1000;
const IDLE_MS = 90_000; // 超过 90 秒无任何操作视为离开，暂停计时（漫画翻页通常远比这频繁）
const HEARTBEAT_MS = 30_000; // 心跳上报间隔
const MIN_FLUSH_SECONDS = 5; // 少于 5 秒不上报，过滤噪声

interface Options {
  bookId: string | null | undefined;
  enabled?: boolean;
}

export function useReaderReadingTime({ bookId, enabled = true }: Options) {
  const activeSecondsRef = useRef(0);
  const lastActivityRef = useRef(0);

  useEffect(() => {
    if (!enabled || !bookId) return undefined;

    // 每本书从零开始累计。
    activeSecondsRef.current = 0;
    lastActivityRef.current = performance.now();

    const markActivity = () => {
      lastActivityRef.current = performance.now();
    };
    const activityEvents = ['mousemove', 'mousedown', 'keydown', 'touchstart', 'wheel', 'scroll'] as const;
    activityEvents.forEach((e) => window.addEventListener(e, markActivity, { passive: true }));

    // flush 结算本会话（闭包 bookId）已累计的秒数。useBeacon 用于卸载/隐藏期。
    const flush = (useBeacon: boolean) => {
      const secs = Math.round(activeSecondsRef.current);
      if (secs < MIN_FLUSH_SECONDS) return;
      activeSecondsRef.current = 0;
      const url = `/api/books/${bookId}/reading-time`;
      const body = { seconds: secs };
      if (useBeacon && typeof navigator !== 'undefined' && typeof navigator.sendBeacon === 'function') {
        try {
          navigator.sendBeacon(url, new Blob([JSON.stringify(body)], { type: 'application/json' }));
          return;
        } catch {
          // 落到 axios 兜底
        }
      }
      // 尽力而为：上报失败即丢弃本次未送达的秒数，不退回累加器。
      // 退回会引入两个 bug：(1) 切书时失败的 .catch 在下一本的会话里执行，把上一本的秒数错记到新书；
      // (2) 提交成功但响应丢失（lost-ack）时退回会导致下次重复上报、重复计数。阅读时长是软统计，可接受偶发少计。
      apiClient.post(url, body).catch(() => undefined);
    };

    const ticker = window.setInterval(() => {
      if (document.visibilityState === 'visible' && performance.now() - lastActivityRef.current < IDLE_MS) {
        activeSecondsRef.current += TICK_MS / 1000;
      }
    }, TICK_MS);
    const heartbeat = window.setInterval(() => flush(false), HEARTBEAT_MS);

    const onVisibility = () => {
      if (document.visibilityState === 'hidden') flush(true);
    };
    const onPageHide = () => flush(true);
    document.addEventListener('visibilitychange', onVisibility);
    window.addEventListener('pagehide', onPageHide);

    return () => {
      window.clearInterval(ticker);
      window.clearInterval(heartbeat);
      document.removeEventListener('visibilitychange', onVisibility);
      window.removeEventListener('pagehide', onPageHide);
      activityEvents.forEach((e) => window.removeEventListener(e, markActivity));
      // 切书 / 卸载：组件仍存活，用普通请求结算旧书剩余时长。
      flush(false);
    };
  }, [bookId, enabled]);
}
