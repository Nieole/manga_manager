import type { RunSnapshot, RunEventsResponse, RunSamplesResponse } from '../../api/generated';
export type { RunLimits, RunSnapshot, RunLive, TaskSummary, RunEvent, RunPhaseSpan, RunEventsResponse, RunSample, RunSamplesResponse } from '../../api/generated';

export type TaskAction = 'pause' | 'resume' | 'cancel' | 'retry';

export interface TaskCenterFilters {
  status: string;
  scope: string;
  type: string;
  scopeId: string;
  query: string;
}

/** Navigation scope for a task target. */
export interface TaskTarget {
  scope: string;
  scope_id?: number;
}

/** History belongs to the selected task, including its loading state. */
export interface TaskRunHistory {
  taskId: number;
  /** runs 为 undefined 表示还没取回来；空数组表示这个任务确实没留下运行记录。 */
  runs?: RunSnapshot[];
  loading?: boolean;
}

/** Events and samples belong to one selected run card. */
export interface RunDetailView {
  cardId: string;
  events?: RunEventsResponse;
  samples?: RunSamplesResponse;
  loading?: boolean;
}

export interface TaskBubbleEntry {
  // taskId 是气泡的身份：同一个**任务**的历次推送更新同一个气泡。
  // 它不是**任务键**——契约上已经没有那一格（ADR 0007），键也认不出「哪一次」。
  taskId: number;
  type: string;
  status: string;
  message: string;
  message_code?: string;
  message_params?: Record<string, string>;
  error?: string;
  current: number;
  total: number;
  scope_name?: string;
  updatedAt: number;
}
