/**
 * **运行**状态的唯一判定处：状态取值与后端一致，活动态、排队中与终态在这里互补，
 * 任何组件都不该再自己列一遍状态字符串。词汇释义见 `CONTEXT.md`「后台任务」。
 */

/** 活动态：一次**运行**仍占着运行槽位，含运行中、已暂停、取消中。 */
export const ACTIVE_RUN_STATUSES = ['running', 'paused', 'cancelling'] as const;

/** 排队中：运行已被登记、还没拿到运行槽位。它既不是活动态也不是终态——不占槽位，却仍会开跑。 */
export const QUEUED_RUN_STATUS = 'queued';

/** 终态：一次运行不会再变化的最终状态，共四种。 */
export const TERMINAL_RUN_STATUSES = ['completed', 'cancelled', 'failed', 'interrupted'] as const;

/** isActiveRunStatus 回答「这次运行还会不会再动」。 */
export function isActiveRunStatus(status: string): boolean {
  return (ACTIVE_RUN_STATUSES as readonly string[]).includes(status);
}

/** isQueuedRunStatus 回答「这次运行是不是还在等槽位」。 */
export function isQueuedRunStatus(status: string): boolean {
  return status === QUEUED_RUN_STATUS;
}

/** isLiveRunStatus 回答「这次运行还会不会变」：活动态或排队中。任务中心的实况区装的就是这些。 */
export function isLiveRunStatus(status: string): boolean {
  return isActiveRunStatus(status) || isQueuedRunStatus(status);
}

/** isTerminalRunStatus 回答「这次运行是不是已经停了」——气泡据此可移除、可关闭、不再转圈。 */
export function isTerminalRunStatus(status: string): boolean {
  return (TERMINAL_RUN_STATUSES as readonly string[]).includes(status);
}
