/**
 * 守推送通道这一层：载荷怎么解、什么算**缺口**。缺口判的是「这一帧的 prev 对不对得上手里那一号」，
 * 不是「序号连不连续」——被节流吞掉的帧照样取了号，按连续判会让繁忙的扫描每收一帧就整份重拉。
 */

import { describe, expect, it, vi } from 'vitest';

import type { RunPush } from '../api/generated';
import { parseRunPush, trackRunPush } from './runPush';

function frame(sequence: number, prev: number): RunPush {
  return { sequence, prev };
}

describe('parseRunPush', () => {
  it('解得开运行快照帧与实况汇总帧', () => {
    const snapshot = parseRunPush('run_snapshot:{"sequence":9,"prev":7,"run":{"run_id":3}}');
    expect(snapshot).toMatchObject({ sequence: 9, prev: 7 });
    expect(snapshot?.run?.run_id).toBe(3);

    const live = parseRunPush('run_live:{"sequence":10,"prev":9,"live":{"active":1,"queued":0,"slots":2,"paused":false,"paused_all":false}}');
    expect(live).toMatchObject({ sequence: 10, prev: 9 });
    expect(live?.live?.active).toBe(1);
  });

  it('不是推送帧的载荷一律 null', () => {
    expect(parseRunPush('refresh')).toBeNull();
    expect(parseRunPush('sse-audience-probe')).toBeNull();
  });

  // 认不出形状的载荷整帧丢掉，而不是拿 undefined 去接链——那会把此后每一帧都判成缺口。
  it('坏掉的或缺序号的载荷一律 null', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    expect(parseRunPush('run_snapshot:{不是 JSON')).toBeNull();
    expect(parseRunPush('run_snapshot:{"run":{"run_id":3}}')).toBeNull();
    warn.mockRestore();
  });
});

describe('trackRunPush', () => {
  it('接得上手上那一号就不是缺口，手上那一号走到这一帧', () => {
    expect(trackRunPush(811, frame(814, 811))).toEqual({ gap: false, sequence: 814 });
  });

  // 这一条是本模块存在的理由：节流吞掉的帧取走了 812、813，送达的序号因此跳了两号，
  // 但链是接着的，不该重拉。
  it('序号跳号但链接得上：不算缺口', () => {
    expect(trackRunPush(811, frame(999, 811)).gap).toBe(false);
  });

  it('prev 对不上手里那一号就是缺口', () => {
    expect(trackRunPush(811, frame(815, 814))).toEqual({ gap: true, sequence: 815 });
  });

  // 刚进页面或 SSE 刚重连上：手上没有可比的号，拿这一帧当新的起点。
  it('手上还没有号时不判缺口，只把起点定下来', () => {
    expect(trackRunPush(null, frame(814, 811))).toEqual({ gap: false, sequence: 814 });
  });

  // 判出缺口之后手上那一号要走到这一帧：不走的话，此后每一帧都会再判一次缺口，重拉一路刷下去。
  it('判出缺口之后下一帧接得上，不再重复重拉', () => {
    const first = trackRunPush(811, frame(815, 814));
    expect(first.gap).toBe(true);
    expect(trackRunPush(first.sequence, frame(816, 815)).gap).toBe(false);
  });

  // 服务端重启之后序号从库里已用掉的最大值接上，可能比手里那一号小：链一样对不上，一样重拉。
  it('序号倒退同样算缺口', () => {
    expect(trackRunPush(811, frame(5, 4)).gap).toBe(true);
  });
});
