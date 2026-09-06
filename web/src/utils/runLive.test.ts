/**
 * 守实况帧收到一帧之后剩下谁：去重按**运行标识**（同一个任务键有多条运行），
 * 进了终态的摘出去，三个汇总跟着重算。判据与后端的 `taskEngine.live` 同形——
 * 两边错开一次，「全部恢复」的可用性就会说谎。
 */

import { describe, expect, it } from 'vitest';

import type { RunLive, RunStatus } from '../api/generated';
import { applyRunToLive } from './runLive';

function run(overrides: Partial<RunStatus>): RunStatus {
  return {
    run_id: 1,
    task_id: 1,
    key: 'scan_library_1',
    type: 'scan_library',
    scope: 'library',
    status: 'running',
    message: '',
    current: 0,
    total: 100,
    can_cancel: true,
    can_pause: true,
    can_resume: false,
    retryable: true,
    started_at: '2026-08-31T00:00:00Z',
    updated_at: '2026-08-31T00:00:00Z',
    ...overrides,
  };
}

const empty: RunLive = { active: 0, queued: 0, slots: 0, paused: false, runs: [] };

describe('applyRunToLive', () => {
  it('新的一帧进实况区，活动数跟着变', () => {
    const live = applyRunToLive(empty, run({ run_id: 7 }));
    expect(live.runs.map((item) => item.run_id)).toEqual([7]);
    expect(live.active).toBe(1);
    expect(live.queued).toBe(0);
  });

  it('同一条运行的下一帧替换它自己，不叠成两条', () => {
    const first = applyRunToLive(empty, run({ run_id: 7, current: 1 }));
    const second = applyRunToLive(first, run({ run_id: 7, current: 9 }));
    expect(second.runs).toHaveLength(1);
    expect(second.runs[0].current).toBe(9);
  });

  // 重试是同一个任务键的新一次运行：按键去重会让新那条的每一帧把上一次摘掉。
  it('同一个任务键的另一次运行不会把上一次挤掉', () => {
    const first = applyRunToLive(empty, run({ run_id: 7 }));
    const second = applyRunToLive(first, run({ run_id: 8 }));
    expect(second.runs.map((item) => item.run_id)).toEqual([8, 7]);
    expect(second.active).toBe(2);
  });

  it('进了终态的那条离开实况区——它不再会变', () => {
    const running = applyRunToLive(empty, run({ run_id: 7 }));
    for (const status of ['completed', 'cancelled', 'failed', 'interrupted']) {
      const done = applyRunToLive(running, run({ run_id: 7, status }));
      expect(done.runs, `${status} 的运行还留在实况区`).toHaveLength(0);
      expect(done.active).toBe(0);
    }
  });

  // 排队中既不是活动态也不是终态：它留在实况区，但不算进槽位占用。
  it('排队中的运行留在实况区，只算进排队数', () => {
    const live = applyRunToLive(empty, run({ run_id: 7, status: 'queued' }));
    expect(live.runs).toHaveLength(1);
    expect(live.active).toBe(0);
    expect(live.queued).toBe(1);
  });

  it('有一条已暂停就把全局暂停置真，恢复之后置回', () => {
    const paused = applyRunToLive(empty, run({ run_id: 7, status: 'paused' }));
    expect(paused.paused).toBe(true);
    expect(paused.active).toBe(1);
    expect(applyRunToLive(paused, run({ run_id: 7, status: 'running' })).paused).toBe(false);
  });

  it('槽位上限不由帧决定，原样带过去', () => {
    const live = applyRunToLive({ ...empty, slots: 2 }, run({ run_id: 7 }));
    expect(live.slots).toBe(2);
  });
});
