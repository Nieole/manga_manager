/**
 * 实况帧收到一帧之后长什么样：一帧运行快照只动运行列表，一帧**实况汇总**只动那几个数。
 * 两件事分开，因为汇总由后端数好了发过来——浏览器手上这份运行列表是推来的帧攒出来的，
 * 丢过一帧就少一条，而少一条数出来的槽位占用不会报错，只会静静少 1。
 */

import type { RunLive, RunLiveSummary, RunStatus } from '../api/generated';
import { isLiveRunStatus } from './runStatus';

/**
 * manualFirstRank 把一条运行分进「手动」与「其余」两组，手动为 0。
 *
 * 判据与后端 `task.OrderManualFirst` 逐字同形：只有**发起方**恰好是手动的那一组排在前。
 * 每条运行都带发起方（它是运行行上的必填列），缺了的按「其余」算，与后端 SQL 一致。
 */
function manualFirstRank(run: RunStatus): number {
  return run.trigger === 'manual' ? 0 : 1;
}

/**
 * applyRunToLive 把一帧运行折进实况帧：仍会变化的留在里面，进了**终态**的摘出去。
 *
 * 定序与后端 `taskEngine.live` 同形：**手动**发起的一组在前，组内最近有动静的在最上面。
 * 少了这条，一条聒噪的守护扫描每报一帧就把自己顶到最前，用户刚按下的那一条会一路下沉。
 *
 * 去重键是**运行标识**而不是**任务键**：同一个任务键如今有多条运行，按键去重会让新一次运行的
 * 每一帧把上一次那条摘掉。那几个汇总数不在这里改——它们由**实况汇总**帧说了算。
 */
export function applyRunToLive(live: RunLive, run: RunStatus): RunLive {
  const withoutRun = live.runs.filter((item) => item.run_id !== run.run_id);
  // sort 是稳定的：同一组里的相对次序（最近有动静的在前）原样保留。
  const runs = isLiveRunStatus(run.status)
    ? [run, ...withoutRun].sort((left, right) => manualFirstRank(left) - manualFirstRank(right))
    : withoutRun;
  return { ...live, runs };
}

/** applyLiveSummary 把一帧**实况汇总**盖到实况帧上，运行列表原样留着——那一帧一条运行都不带。 */
export function applyLiveSummary(live: RunLive, summary: RunLiveSummary): RunLive {
  return { ...live, ...summary };
}
