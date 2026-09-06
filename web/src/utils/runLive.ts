/**
 * 实况帧收到一帧运行快照之后长什么样。抽出来是为了让「谁还在实况区里、三个汇总各是多少」
 * 只有一处判据，且判据与后端 `taskEngine.live` 同形——两边错开一次，「全部恢复」的可用性就会说谎。
 */

import type { RunLive, RunStatus } from '../api/generated';
import { isActiveRunStatus, isLiveRunStatus, isQueuedRunStatus } from './runStatus';

/**
 * applyRunToLive 把一帧运行折进实况帧：仍会变化的留在里面（最新的排在最前），
 * 进了**终态**的摘出去，三个汇总跟着重算。
 *
 * 去重键是**运行标识**而不是**任务键**：同一个任务键如今有多条运行，按键去重会让新一次运行的
 * 每一帧把上一次那条摘掉。槽位上限不由帧决定，因此原样带过去。
 */
export function applyRunToLive(live: RunLive, run: RunStatus): RunLive {
  const withoutRun = live.runs.filter((item) => item.run_id !== run.run_id);
  const runs = isLiveRunStatus(run.status) ? [run, ...withoutRun] : withoutRun;
  return {
    ...live,
    runs,
    active: runs.filter((item) => isActiveRunStatus(item.status)).length,
    queued: runs.filter((item) => isQueuedRunStatus(item.status)).length,
    paused: runs.some((item) => item.status === 'paused'),
  };
}
