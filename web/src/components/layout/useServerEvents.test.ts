/**
 * @vitest-environment jsdom
 *
 * 本文件是业务回归测试，覆盖全站 SSE 订阅的自动重连。
 * 锁住的性质是：EventSource 进入 CLOSED（服务端返回非 2xx，如会话过期的 401 或反代 502）
 * 之后必须由我们接管重连，否则全站实时刷新与任务进度气泡会静默死亡直到用户手动刷页。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useServerEvents } from './useServerEvents';

// FakeEventSource 复刻我们依赖的那一小块 EventSource 契约，并暴露触发钩子供测试驱动。
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;

  url: string;
  readyState = FakeEventSource.CONNECTING;
  closed = false;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  close() {
    this.closed = true;
    this.readyState = FakeEventSource.CLOSED;
  }

  /** 模拟「服务端返回非 2xx，浏览器放弃重试」——真实 EventSource 在此状态下永远不会自愈。 */
  failFatally() {
    this.readyState = FakeEventSource.CLOSED;
    this.onerror?.();
  }

  /** 模拟「网络抖动，浏览器自己在重试」——此时我们不该插手，否则会开出双连接。 */
  failTransiently() {
    this.readyState = FakeEventSource.CONNECTING;
    this.onerror?.();
  }
}

describe('useServerEvents', () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('delivers messages to the handler', () => {
    const onMessage = vi.fn();
    renderHook(() => useServerEvents('/api/events', { onMessage }));

    act(() => {
      FakeEventSource.instances[0].onmessage?.({ data: 'refresh' });
    });

    expect(onMessage).toHaveBeenCalledWith('refresh');
  });

  it('reconnects with backoff after the browser gives up (readyState CLOSED)', () => {
    renderHook(() => useServerEvents('/api/events', { onMessage: vi.fn() }));
    expect(FakeEventSource.instances).toHaveLength(1);

    act(() => {
      FakeEventSource.instances[0].failFatally();
    });
    // 退避未到期之前不该重连。
    act(() => {
      vi.advanceTimersByTime(999);
    });
    expect(FakeEventSource.instances).toHaveLength(1);

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(FakeEventSource.instances).toHaveLength(2);

    // 第二次失败的等待时间应翻倍（1s → 2s）。
    act(() => {
      FakeEventSource.instances[1].failFatally();
      vi.advanceTimersByTime(1000);
    });
    expect(FakeEventSource.instances).toHaveLength(2);
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(FakeEventSource.instances).toHaveLength(3);
  });

  it('does not interfere while the browser is retrying on its own', () => {
    renderHook(() => useServerEvents('/api/events', { onMessage: vi.fn() }));

    act(() => {
      FakeEventSource.instances[0].failTransiently();
      vi.advanceTimersByTime(60_000);
    });

    // readyState 仍是 CONNECTING：EventSource 自己在重试，我们插手会开出第二条连接。
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].closed).toBe(false);
  });

  it('resets the backoff after a successful reconnect', () => {
    renderHook(() => useServerEvents('/api/events', { onMessage: vi.fn() }));

    act(() => {
      FakeEventSource.instances[0].failFatally();
      vi.advanceTimersByTime(1000);
    });
    expect(FakeEventSource.instances).toHaveLength(2);

    // 连上了 → 退避重置；否则一次长时间离线会让后续每次断线都要等满 30s。
    act(() => {
      FakeEventSource.instances[1].onopen?.();
      FakeEventSource.instances[1].failFatally();
      vi.advanceTimersByTime(1000);
    });
    expect(FakeEventSource.instances).toHaveLength(3);
  });

  it('closes the connection and cancels pending retries on unmount', () => {
    const { unmount } = renderHook(() => useServerEvents('/api/events', { onMessage: vi.fn() }));
    const first = FakeEventSource.instances[0];

    act(() => {
      first.failFatally();
    });
    unmount();

    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    // 卸载后不得再建连接，否则路由切走/登出后仍会留下后台重连。
    expect(FakeEventSource.instances).toHaveLength(1);
  });
});
