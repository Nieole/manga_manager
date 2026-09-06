/**
 * **运行**状态的唯一判定处：状态取值与后端一致，活动态与终态在这里互补，
 * 任何组件都不该再自己列一遍状态字符串。词汇释义见 `CONTEXT.md`「后台任务」。
 */

/** 活动态：一次**运行**仍占着运行槽位，含运行中、已暂停、取消中。 */
export const ACTIVE_RUN_STATUSES = ['running', 'paused', 'cancelling'] as const;

/** 终态：一次运行不会再变化的最终状态，共四种。 */
export const TERMINAL_RUN_STATUSES = ['completed', 'cancelled', 'failed', 'interrupted'] as const;

/** isActiveRunStatus 回答「这次运行还会不会再动」。 */
export function isActiveRunStatus(status: string): boolean {
  return (ACTIVE_RUN_STATUSES as readonly string[]).includes(status);
}

/** isTerminalRunStatus 回答「这次运行是不是已经停了」——气泡据此可移除、可关闭、不再转圈。 */
export function isTerminalRunStatus(status: string): boolean {
  return (TERMINAL_RUN_STATUSES as readonly string[]).includes(status);
}
