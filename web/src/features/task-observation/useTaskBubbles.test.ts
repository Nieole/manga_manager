/** @vitest-environment jsdom */
// 终态定时释放气泡；活动态与漏帧恢复不能被误清除。

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, renderHook } from '@testing-library/react';

import { useTaskBubbles } from './useTaskBubbles';
import { ApplicationSessionProvider } from '../../session/ApplicationSessionProvider';
import { apiClient } from '../../api/client';
import { createElement, type PropsWithChildren } from 'react';

class Source {
  static CLOSED = 2;
  static instances: Source[] = [];
  readyState = 0;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onopen: (() => void) | null = null;
  constructor() { Source.instances.push(this); }
  close() {}
}
function wrapper({ children }: PropsWithChildren) {
  return createElement(ApplicationSessionProvider, { identity: '1:admin', isAdmin: true, children });
}
function push(run: object) { Source.instances.at(-1)?.onmessage?.({ data: 'run_snapshot:' + JSON.stringify({ sequence: 1, prev: 0, run }) }); }


// 后端 finalizeTask 与服务重启回收写入的四种终态取值，逐个跑一遍。
const TERMINAL_STATUSES = ['completed', 'cancelled', 'failed', 'interrupted'];

// 气泡的身份是**任务 id**：契约上不再有任务键（ADR 0007）。
const TASK_ID = 41;

// 终态延时移除最长 20s（失败/取消/中断），跳过它足够让四种终态都到期。
const CLEANUP_DELAY_MS = 20000;

describe('useTaskBubbles 的终态清理', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal('EventSource', Source);
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.unstubAllGlobals();
    Source.instances = [];
  });

  it.each(TERMINAL_STATUSES)('收到 %s 的进度后，气泡会自己延时消失', (status) => {
    const { result } = renderHook(() => useTaskBubbles(), { wrapper });

    act(() => {
      push({ task_id: TASK_ID, type: 'scan_library', status });
    });
    expect(result.current.entries[TASK_ID]).toBeTruthy();

    act(() => {
      vi.advanceTimersByTime(CLEANUP_DELAY_MS);
    });
    expect(result.current.entries[TASK_ID]).toBeUndefined();
  });

  it.each(TERMINAL_STATUSES)('「清除已完成」清得掉 %s 的气泡', (status) => {
    const { result } = renderHook(() => useTaskBubbles(), { wrapper });

    act(() => {
      push({ task_id: TASK_ID, type: 'scan_library', status });
    });
    act(() => {
      result.current.clearFinished();
    });

    expect(result.current.entries[TASK_ID]).toBeUndefined();
  });

  it('取消中是活动态：气泡既不自己消失，也不被「清除已完成」清掉', () => {
    const { result } = renderHook(() => useTaskBubbles(), { wrapper });

    act(() => {
      push({ task_id: TASK_ID, type: 'scan_library', status: 'cancelling' });
    });
    act(() => {
      vi.advanceTimersByTime(CLEANUP_DELAY_MS);
      result.current.clearFinished();
    });

    expect(result.current.entries[TASK_ID]?.status).toBe('cancelling');
  });
});

it('recovers a missed terminal frame for an existing bubble and still expires it', async () => {
  vi.useFakeTimers();
  vi.stubGlobal('EventSource', Source);
  const adapter = apiClient.defaults.adapter;
  apiClient.defaults.adapter = async config => ({ data: [{ task_id: TASK_ID, run_id: 9, type: 'scan_library', status: 'completed' }], status: 200, statusText: 'OK', headers: {}, config });
  try {
    const { result, unmount } = renderHook(() => useTaskBubbles(), { wrapper });
    act(() => push({ task_id: TASK_ID, run_id: 9, status: 'running' }));
    await act(async () => {
      Source.instances.at(-1)?.onmessage?.({ data: 'run_live:{"sequence":4,"prev":3}' });
    });
    expect(result.current.entries[TASK_ID]?.status).toBe('completed');
    act(() => vi.advanceTimersByTime(8000));
    expect(result.current.entries[TASK_ID]).toBeUndefined();
    unmount();
  } finally {
    cleanup();
    apiClient.defaults.adapter = adapter;
    vi.useRealTimers();
    vi.unstubAllGlobals();
    Source.instances = [];
  }
});
