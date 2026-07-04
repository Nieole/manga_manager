/**
 * 业务说明：本文件是应用外壳的后台任务气泡 hook，聚合任务进度气泡的状态、终态延时清理定时器、
 * 进度覆盖事件监听，以及 SSE 进度载荷的接入（ingest）与手动关闭/清理已完成的逻辑。
 * 维护时应关注：终态（完成/失败/取消）气泡的延时移除、message 与 message_code 的互斥、卸载时清空定时器。
 */

import { useState, useRef, useEffect, useCallback } from 'react';
import type { TaskBubbleEntry } from '../SidebarTaskBubble';

interface TaskProgressPayload {
  key?: string;
  type?: string;
  status?: string;
  message?: string;
  message_code?: string;
  message_params?: Record<string, string>;
  error?: string;
  current?: number;
  total?: number;
  scope_name?: string;
}

const isTerminal = (status: string) => status === 'completed' || status === 'failed' || status === 'canceled';

export function useTaskBubbles() {
  const [entries, setEntries] = useState<Record<string, TaskBubbleEntry>>({});
  const cleanupTimers = useRef<Map<string, number>>(new Map());

  // ingestProgress 接入一条 SSE task_progress 载荷：新增/更新对应气泡，并为终态气泡安排延时移除
  //（完成 8s、失败/取消 20s）；再次收到同 key 会先取消旧的延时定时器。
  const ingestProgress = useCallback((progress: TaskProgressPayload) => {
    if (!progress.key) return;
    const key = progress.key;
    const entry: TaskBubbleEntry = {
      key,
      type: progress.type || '',
      status: progress.status || 'running',
      message: progress.message || '',
      message_code: progress.message_code,
      message_params: progress.message_params,
      error: progress.error,
      current: progress.current ?? 0,
      total: progress.total ?? 0,
      scope_name: progress.scope_name,
      updatedAt: Date.now(),
    };
    setEntries((prev) => ({ ...prev, [key]: entry }));
    const existingTimer = cleanupTimers.current.get(key);
    if (existingTimer) {
      clearTimeout(existingTimer);
      cleanupTimers.current.delete(key);
    }
    if (isTerminal(entry.status)) {
      const timer = window.setTimeout(() => {
        setEntries((prev) => {
          if (!prev[key]) return prev;
          const next = { ...prev };
          delete next[key];
          return next;
        });
        cleanupTimers.current.delete(key);
      }, entry.status === 'completed' ? 8000 : 20000);
      cleanupTimers.current.set(key, timer);
    }
  }, []);

  const dismiss = useCallback((key: string) => {
    setEntries((prev) => {
      if (!prev[key]) return prev;
      const next = { ...prev };
      delete next[key];
      return next;
    });
    const timer = cleanupTimers.current.get(key);
    if (timer) {
      clearTimeout(timer);
      cleanupTimers.current.delete(key);
    }
  }, []);

  const clearFinished = useCallback(() => {
    setEntries((prev) => {
      const next: Record<string, TaskBubbleEntry> = {};
      for (const [key, entry] of Object.entries(prev)) {
        if (!isTerminal(entry.status)) {
          next[key] = entry;
        } else {
          const timer = cleanupTimers.current.get(key);
          if (timer) {
            clearTimeout(timer);
            cleanupTimers.current.delete(key);
          }
        }
      }
      return next;
    });
  }, []);

  // 监听进度覆盖事件（如系列详情页在触发操作后乐观更新对应任务气泡的进度/状态），
  // 并在卸载时清空全部延时定时器。message 与 message_code 互斥：带了新 legacy message 的覆盖清掉 code。
  useEffect(() => {
    const timers = cleanupTimers.current;
    const handleOverride = (event: Event) => {
      const customEvent = event as CustomEvent<TaskProgressPayload>;
      const detail = customEvent.detail;
      if (!detail?.key) return;
      const key = detail.key;
      setEntries((prev) => {
        const existing = prev[key];
        if (!existing) return prev;
        return {
          ...prev,
          [key]: {
            ...existing,
            status: detail.status || existing.status,
            message: detail.message || existing.message,
            message_code: detail.message ? undefined : (detail.message_code ?? existing.message_code),
            message_params: detail.message ? undefined : (detail.message_params ?? existing.message_params),
            error: detail.error ?? existing.error,
            current: detail.current ?? existing.current,
            total: detail.total ?? existing.total,
            type: detail.type || existing.type,
            updatedAt: Date.now(),
          },
        };
      });
    };
    window.addEventListener('manga-manager:task-progress-override', handleOverride as EventListener);
    return () => {
      window.removeEventListener('manga-manager:task-progress-override', handleOverride as EventListener);
      timers.forEach((timer) => clearTimeout(timer));
      timers.clear();
    };
  }, []);

  return { entries, ingestProgress, dismiss, clearFinished };
}
