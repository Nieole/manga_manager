/**
 * 守实况帧收到一帧之后剩下谁：去重按**运行标识**（同一个任务键有多条运行），
 * 进了终态的摘出去，定序与后端的 `taskEngine.live` 同形。那几个汇总数不在这里算——
 * 它们由**实况汇总**帧说了算，这份运行列表本身可能正缺着一条。
 */

import { describe, expect, it } from 'vitest';

import type { RunLive, RunSnapshot } from '../api/generated';
import { applyLiveSummary, applyRunToLive } from './runLive';

function run(overrides: Partial<RunSnapshot>): RunSnapshot {
  return {
    run_id: 1,
    task_id: 1,
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

const empty: RunLive = { active: 0, queued: 0, slots: 2, paused: false, paused_all: false, runs: [] };

describe('applyRunToLive', () => {
  it('新的一帧进实况区', () => {
    const live = applyRunToLive(empty, run({ run_id: 7 }));
    expect(live.runs.map((item) => item.run_id)).toEqual([7]);
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
  });

  it('进了终态的那条离开实况区——它不再会变', () => {
    const running = applyRunToLive(empty, run({ run_id: 7 }));
    for (const status of ['completed', 'cancelled', 'failed', 'interrupted']) {
      const done = applyRunToLive(running, run({ run_id: 7, status }));
      expect(done.runs, `${status} 的运行还留在实况区`).toHaveLength(0);
    }
  });

  // 排队中既不是活动态也不是终态：它留在实况区，等着被放行。
  it('排队中的运行留在实况区', () => {
    const live = applyRunToLive(empty, run({ run_id: 7, status: 'queued' }));
    expect(live.runs).toHaveLength(1);
  });

  // 实况区默认不被自动运行淹没：用户刚发起的那一条一眼能找到。
  it('手动发起的排在自动发起的前面，哪怕自动那条更晚有动静', () => {
    const withManual = applyRunToLive(empty, run({ run_id: 7, trigger: 'manual' }));
    const withScheduled = applyRunToLive(withManual, run({ run_id: 8, trigger: 'scheduled' }));
    expect(withScheduled.runs.map((item) => item.run_id)).toEqual([7, 8]);

    // 自动那条又报了一帧：它仍然排在手动那条后面。
    const nextFrame = applyRunToLive(withScheduled, run({ run_id: 8, trigger: 'scheduled', current: 9 }));
    expect(nextFrame.runs.map((item) => item.run_id)).toEqual([7, 8]);
  });

  it('同一组里最近有动静的在最上面', () => {
    const first = applyRunToLive(empty, run({ run_id: 7, trigger: 'scheduled' }));
    const second = applyRunToLive(first, run({ run_id: 8, trigger: 'watch' }));
    expect(second.runs.map((item) => item.run_id)).toEqual([8, 7]);
  });

  // 槽位占用与全局暂停由后端数好了发过来：一帧运行快照动不了它们，哪怕这条运行刚进终态——
  // 浏览器手上这份列表丢过一帧就少一条，照它现算出来的占用不会报错，只会静静少 1。
  it('一帧运行快照不动那几个汇总数', () => {
    const busy: RunLive = { ...empty, active: 2, queued: 1, paused: true, paused_all: true };
    const live = applyRunToLive(busy, run({ run_id: 7, status: 'completed' }));
    expect(live.active).toBe(2);
    expect(live.queued).toBe(1);
    expect(live.paused).toBe(true);
    expect(live.paused_all).toBe(true);
    expect(live.slots).toBe(2);
  });
});

describe('applyLiveSummary', () => {
  it('汇总帧盖掉那几个数，运行列表原样留着', () => {
    const withRun = applyRunToLive(empty, run({ run_id: 7 }));
    const live = applyLiveSummary(withRun, { active: 1, queued: 2, slots: 5, paused: true, paused_all: true });
    expect(live).toMatchObject({ active: 1, queued: 2, slots: 5, paused: true, paused_all: true });
    expect(live.runs.map((item) => item.run_id)).toEqual([7]);
  });
});
