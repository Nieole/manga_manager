// CLOSED 需要显式退避重连；CONNECTING 由浏览器恢复，作用域退出必须打断重试。

import { useEffect, useRef } from 'react';

export interface ServerEventsHandlers {
  /** 收到一条 SSE 消息时调用。 */
  onMessage: (data: string) => void;
}

// 指数退避：1s → 2s → 4s … 封顶 30s。封顶值不宜太大，否则服务重启后前端要等很久才恢复。
const INITIAL_RETRY_MS = 1000;
const MAX_RETRY_MS = 30_000;

export function useEventStream(url: string, handlers: ServerEventsHandlers): void {
  // 用 ref 持有回调，避免它每次渲染变化都触发重连。
  const onMessageRef = useRef(handlers.onMessage);
  onMessageRef.current = handlers.onMessage;

  useEffect(() => {
    let disposed = false;
    let source: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let retryDelay = INITIAL_RETRY_MS;

    const scheduleReconnect = () => {
      if (disposed || retryTimer) return;
      const delay = retryDelay;
      retryDelay = Math.min(retryDelay * 2, MAX_RETRY_MS);
      retryTimer = setTimeout(() => {
        retryTimer = null;
        connect();
      }, delay);
    };

    const connect = () => {
      if (disposed) return;
      const es = new EventSource(url);
      source = es;

      es.onopen = () => {
        if (disposed || source !== es) return;
        // 连上了就把退避重置，否则一次长时间离线会让后续每次断线都等满 30s。
        retryDelay = INITIAL_RETRY_MS;
      };

      es.onmessage = (event) => {
        if (disposed || source !== es) return;
        onMessageRef.current(event.data as string);
      };

      es.onerror = () => {
        if (disposed || source !== es) return;
        // readyState 仍为 CONNECTING 时是 EventSource 自己在重试，不要插手（否则会开出双连接）。
        // 只有 CLOSED 才意味着它已经放弃，需要我们接管。
        if (es.readyState !== EventSource.CLOSED) return;
        es.close();
        if (source === es) source = null;
        scheduleReconnect();
      };
    };

    connect();

    return () => {
      disposed = true;
      if (retryTimer) clearTimeout(retryTimer);
      source?.close();
    };
  }, [url]);
}
