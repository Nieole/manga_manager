import { useState, useRef, useEffect, useCallback } from 'react';
import type { TaskBubbleEntry } from '../../components/SidebarTaskBubble';
import { useSessionRecovery, useSessionRunPush } from '../../session/ApplicationSessionProvider';
import { apiClient } from '../../api/client';
import type { RunSnapshot } from '../../api/generated';
import { isTerminalRunStatus } from '../../utils/runStatus';

interface RunSnapshotPayload {
  // task_id 是气泡的身份：同一个**任务**的历次推送更新同一个气泡。
  // 契约上不再有任务键（ADR 0007）——它认得出「哪件事」，认不出「哪一次」，而队列一开，
  // 同一件事此刻可以有两条仍会变化的运行。
  task_id?: number;
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

export function useTaskBubbles() {
  const [entries, setEntries] = useState<Record<number, TaskBubbleEntry>>({});
  const entriesRef = useRef(entries);
  entriesRef.current = entries;
  const revisions = useRef(new Map<number, number>());
  const generation = useRef(0);
  const cleanupTimers = useRef<Map<number, number>>(new Map());

  // 新增或更新任务气泡，为终态安排延时移除
  //（完成 8s、其余终态 20s）；再次收到同一个任务会先取消旧的延时定时器。
  const ingestRunSnapshot = useCallback((progress: RunSnapshotPayload) => {
    if (progress.task_id === undefined) return;
    const key = progress.task_id;
    revisions.current.set(key, (revisions.current.get(key) ?? 0) + 1);
    const entry: TaskBubbleEntry = {
      taskId: key,
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
    if (isTerminalRunStatus(entry.status)) {
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

  const dismiss = useCallback((key: number) => {
    revisions.current.set(key, (revisions.current.get(key) ?? 0) + 1);
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
      const next: Record<number, TaskBubbleEntry> = {};
      for (const [id, entry] of Object.entries(prev)) {
        const key = Number(id);
        if (!isTerminalRunStatus(entry.status)) {
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

  useSessionRunPush(frame => {
    if (frame.run) ingestRunSnapshot(frame.run);
  });

  useSessionRecovery(() => {
    const recovery = ++generation.current;
    for (const taskId of Object.keys(entriesRef.current).map(Number)) {
      const revision = revisions.current.get(taskId);
      void apiClient.get<RunSnapshot[]>(`/api/system/tasks?task_id=${taskId}&limit=1`).then(({ data }) => {
        if (recovery !== generation.current || revision !== revisions.current.get(taskId) || !entriesRef.current[taskId]) return;
        const run = Array.isArray(data) ? data.find(item => item.task_id === taskId) : undefined;
        if (run) ingestRunSnapshot(run);
      }).catch(error => console.warn('Failed to recover task bubble', error));
    }
  });

  useEffect(() => {
    const timers = cleanupTimers.current;
    return () => {
      generation.current += 1;
      timers.forEach(timer => clearTimeout(timer));
      timers.clear();
    };
  }, []);

  return { entries, dismiss, clearFinished };
}
