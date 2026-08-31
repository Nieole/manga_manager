/**
 * @vitest-environment jsdom
 *
 * 守卫任务气泡对终态的认定与 `CONTEXT.md`「后台任务」一致：四种终态（完成/已取消/失败/中断）
 * 都要能延时自动移除、都要被「清除已完成」清掉；取消中属于活动态，两者都不能碰它。
 * 认错任何一种，那种气泡就永久钉在侧边栏，用户只能刷新页面才赶得走。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

import { useTaskBubbles } from './useTaskBubbles';

// 后端 finalizeTask 与服务重启回收写入的四种终态取值，逐个跑一遍。
const TERMINAL_STATUSES = ['completed', 'cancelled', 'failed', 'interrupted'];

const TASK_KEY = 'scan_library:1';

// 终态延时移除最长 20s（失败/取消/中断），跳过它足够让四种终态都到期。
const CLEANUP_DELAY_MS = 20000;

describe('useTaskBubbles 的终态清理', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it.each(TERMINAL_STATUSES)('收到 %s 的进度后，气泡会自己延时消失', (status) => {
    const { result } = renderHook(() => useTaskBubbles());

    act(() => {
      result.current.ingestProgress({ key: TASK_KEY, type: 'scan_library', status });
    });
    expect(result.current.entries[TASK_KEY]).toBeTruthy();

    act(() => {
      vi.advanceTimersByTime(CLEANUP_DELAY_MS);
    });
    expect(result.current.entries[TASK_KEY]).toBeUndefined();
  });

  it.each(TERMINAL_STATUSES)('「清除已完成」清得掉 %s 的气泡', (status) => {
    const { result } = renderHook(() => useTaskBubbles());

    act(() => {
      result.current.ingestProgress({ key: TASK_KEY, type: 'scan_library', status });
    });
    act(() => {
      result.current.clearFinished();
    });

    expect(result.current.entries[TASK_KEY]).toBeUndefined();
  });

  it('取消中是活动态：气泡既不自己消失，也不被「清除已完成」清掉', () => {
    const { result } = renderHook(() => useTaskBubbles());

    act(() => {
      result.current.ingestProgress({ key: TASK_KEY, type: 'scan_library', status: 'cancelling' });
    });
    act(() => {
      vi.advanceTimersByTime(CLEANUP_DELAY_MS);
      result.current.clearFinished();
    });

    expect(result.current.entries[TASK_KEY]?.status).toBe('cancelling');
  });
});
