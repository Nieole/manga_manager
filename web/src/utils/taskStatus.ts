/**
 * 后台任务状态的唯一判定处：状态取值与后端一致，活动态与终态在这里互补，
 * 任何组件都不该再自己列一遍状态字符串。词汇释义见 `CONTEXT.md`「后台任务」。
 */

/** 活动态：任务仍占着运行槽位，含运行中、已暂停、取消中。 */
export const ACTIVE_TASK_STATUSES = ['running', 'paused', 'cancelling'] as const;

/** 终态：任务不再占用运行槽位后的最终状态，共四种。 */
export const TERMINAL_TASK_STATUSES = ['completed', 'cancelled', 'failed', 'interrupted'] as const;

/** isActiveTaskStatus 回答「这个任务还会不会再动」。 */
export function isActiveTaskStatus(status: string): boolean {
  return (ACTIVE_TASK_STATUSES as readonly string[]).includes(status);
}

/** isTerminalTaskStatus 回答「这个任务是不是已经停了」——气泡据此可移除、可关闭、不再转圈。 */
export function isTerminalTaskStatus(status: string): boolean {
  return (TERMINAL_TASK_STATUSES as readonly string[]).includes(status);
}
