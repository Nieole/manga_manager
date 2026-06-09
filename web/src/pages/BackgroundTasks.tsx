/**
 * 业务说明：本文件是业务实现，属于项目源码的一部分，负责支撑漫画管理器在资料库、阅读器、扫描、元数据或系统设置中的具体业务能力。
 * 它与相邻模块共同组成前后端业务链路，修改时需要结合调用方理解数据流和用户可见行为。
 * 维护时应关注输入输出契约、错误处理、状态同步和与既有业务语义的一致性。
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import axios from 'axios';
import { Activity, RefreshCw } from 'lucide-react';
import { TaskCenter, type TaskAction, type TaskCenterFilters, type TaskStatus } from '../components/tasks/TaskCenter';
import { useI18n } from '../i18n/LocaleProvider';
import { useToast } from '../components/ToastProvider';

const TASK_TYPE_OPTIONS = [
  'scan_library',
  'scan_external_library',
  'scan_series',
  'cleanup_library',
  'rebuild_index',
  'rebuild_thumbnails',
  'cleanup_thumbnails',
  'rebuild_file_identities',
  'scrape',
  'ai_grouping',
  'rebuild_book_hashes',
  'reconcile_koreader_progress',
  'refresh_koreader_matching',
  'transfer_external_library',
];

interface StorageIODiagnostics {
  paused: boolean;
}

interface BackgroundTasksProps {
  embedded?: boolean;
  onViewTaskLogs?: (task: TaskStatus) => void;
}

export default function BackgroundTasks({ embedded = false, onViewTaskLogs }: BackgroundTasksProps = {}) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [tasks, setTasks] = useState<TaskStatus[]>([]);
  const [loadingTasks, setLoadingTasks] = useState(false);
  const [taskActionKey, setTaskActionKey] = useState<string | null>(null);
  const [taskStatusFilter, setTaskStatusFilter] = useState('ALL');
  const [taskScopeFilter, setTaskScopeFilter] = useState('ALL');
  const [taskTypeFilter, setTaskTypeFilter] = useState('ALL');
  const [taskScopeIdFilter, setTaskScopeIdFilter] = useState('');
  const [taskQuery, setTaskQuery] = useState('');
  const [storageIO, setStorageIO] = useState<StorageIODiagnostics | null>(null);
  const { showToast } = useToast();

  const taskFilters = useMemo<TaskCenterFilters>(() => ({
    status: taskStatusFilter,
    scope: taskScopeFilter,
    type: taskTypeFilter,
    scopeId: taskScopeIdFilter,
    query: taskQuery,
  }), [taskQuery, taskScopeFilter, taskScopeIdFilter, taskStatusFilter, taskTypeFilter]);

  const buildTaskParams = useCallback((status?: string) => {
    const params = new URLSearchParams({ limit: '50' });
    if (status) params.set('status', status);
    if (!status && taskStatusFilter !== 'ALL') params.set('status', taskStatusFilter);
    if (taskScopeFilter !== 'ALL') params.set('scope', taskScopeFilter);
    if (taskTypeFilter !== 'ALL') params.set('type', taskTypeFilter);
    if (taskScopeIdFilter.trim()) params.set('scope_id', taskScopeIdFilter.trim());
    if (taskQuery.trim()) params.set('q', taskQuery.trim());
    return params;
  }, [taskQuery, taskScopeFilter, taskScopeIdFilter, taskStatusFilter, taskTypeFilter]);

  const fetchStorageIO = useCallback(async () => {
    try {
      const res = await axios.get<StorageIODiagnostics>('/api/system/storage-io');
      setStorageIO(res.data);
    } catch (error) {
      console.error(error);
    }
  }, []);

  const fetchTasks = useCallback(async () => {
    setLoadingTasks(true);
    try {
      const res = await axios.get<TaskStatus[]>(`/api/system/tasks?${buildTaskParams().toString()}`);
      const items = Array.isArray(res.data) ? res.data : [];
      const seen = new Set<string>();
      setTasks(items.filter((task) => {
        if (seen.has(task.key)) return false;
        seen.add(task.key);
        return true;
      }).slice(0, 50));
    } catch (error) {
      console.error(error);
      showToast(t('settings.maintenance.taskCenterLoadFailed'), 'error');
    } finally {
      setLoadingTasks(false);
    }
  }, [buildTaskParams, showToast, t]);

  useEffect(() => {
    fetchTasks();
    fetchStorageIO();
  }, [fetchStorageIO, fetchTasks]);

  useEffect(() => {
    // 复用 Layout 中已挂载的全局 EventSource：它接收 task_progress 后会
    // dispatch 'manga-manager:task-progress' 自定义事件。这里只监听自定义事件，
    // 避免对同一 origin 再开第二条 SSE 长连接占用浏览器并发额度。
    const handler = (event: Event) => {
      const task = (event as CustomEvent<TaskStatus>).detail;
      if (!task || typeof task !== 'object') return;
      setTasks((prev) => {
        const matchesStatus = taskFilters.status === 'ALL' || task.status === taskFilters.status;
        const matchesScope = taskFilters.scope === 'ALL' || task.scope === taskFilters.scope;
        const matchesType = taskFilters.type === 'ALL' || task.type === taskFilters.type;
        const matchesScopeId = !taskFilters.scopeId.trim() || String(task.scope_id || '') === taskFilters.scopeId.trim();
        const q = taskFilters.query.trim().toLowerCase();
        const matchesQuery = !q || [
          task.key,
          task.type,
          task.scope,
          task.scope_name,
          task.message,
          task.error,
          task.current_item,
        ].some((value) => String(value || '').toLowerCase().includes(q));
        const nextWithoutTask = prev.filter((item) => item.key !== task.key);
        if (!matchesStatus || !matchesScope || !matchesType || !matchesScopeId || !matchesQuery) {
          return nextWithoutTask;
        }
        return [task, ...nextWithoutTask].slice(0, 50);
      });
    };
    window.addEventListener('manga-manager:task-progress', handler as EventListener);
    return () => window.removeEventListener('manga-manager:task-progress', handler as EventListener);
  }, [taskFilters]);

  useEffect(() => {
    const poll = window.setInterval(() => {
      fetchTasks();
      fetchStorageIO();
    }, 15000);
    return () => window.clearInterval(poll);
  }, [fetchStorageIO, fetchTasks]);

  const runTaskAction = async (task: TaskStatus, action: TaskAction) => {
    setTaskActionKey(`${task.key}:${action}`);
    try {
      await axios.post(`/api/system/tasks/${encodeURIComponent(task.key)}/${action}`);
      showToast(t(`settings.maintenance.taskAction.${action}Success`));
      await fetchTasks();
      if (action === 'pause' || action === 'resume') {
        await fetchStorageIO();
      }
    } catch (error) {
      console.error(error);
      showToast(t(`settings.maintenance.taskAction.${action}Failed`), 'error');
    } finally {
      setTaskActionKey(null);
    }
  };

  const currentTaskFilterCanClear = !['ALL', 'running', 'paused', 'cancelling'].includes(taskStatusFilter);

  const updateTaskFilters = (patch: Partial<TaskCenterFilters>) => {
    if (patch.status !== undefined) setTaskStatusFilter(patch.status);
    if (patch.scope !== undefined) setTaskScopeFilter(patch.scope);
    if (patch.type !== undefined) setTaskTypeFilter(patch.type);
    if (patch.scopeId !== undefined) setTaskScopeIdFilter(patch.scopeId);
    if (patch.query !== undefined) setTaskQuery(patch.query);
  };

  const clearTasks = async (status?: 'completed' | 'failed', useCurrentFilters = false) => {
    try {
      const params = new URLSearchParams();
      if (status) {
        params.set('status', status);
      } else if (useCurrentFilters && taskStatusFilter !== 'ALL') {
        params.set('status', taskStatusFilter);
      }
      if (useCurrentFilters) {
        if (taskScopeFilter !== 'ALL') params.set('scope', taskScopeFilter);
        if (taskTypeFilter !== 'ALL') params.set('type', taskTypeFilter);
        if (taskScopeIdFilter.trim()) params.set('scope_id', taskScopeIdFilter.trim());
      }
      await axios.delete(`/api/system/tasks?${params.toString()}`);
      await fetchTasks();
    } catch (error) {
      console.error(error);
      showToast(t('organize.toast.actionFailed'), 'error');
    }
  };

  const openTaskTarget = (task: TaskStatus) => {
    if (task.scope === 'series' && task.scope_id) {
      navigate(`/series/${task.scope_id}`);
      return;
    }
    if (task.scope === 'library' && task.scope_id) {
      navigate(`/library/${task.scope_id}`);
      return;
    }
    navigate('/ops?tab=tasks');
  };

  return (
    <div className={embedded ? 'space-y-6 select-none' : 'mx-auto max-w-[1600px] space-y-6 p-4 sm:p-8 select-none'}>
      {!embedded && (
      <div className="flex flex-col gap-4 border-b border-gray-800/60 pb-6 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="inline-flex items-center gap-2 rounded-full border border-emerald-500/20 bg-emerald-500/10 px-3 py-1 text-xs font-semibold uppercase tracking-wide text-emerald-300">
            <Activity className="h-4 w-4" />
            {t('organize.tasks.badge')}
          </div>
          <h1 className="mt-3 text-3xl font-bold tracking-tight text-white">{t('organize.tasks.title')}</h1>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-gray-400">{t('organize.tasks.description')}</p>
        </div>
        <button
          onClick={fetchTasks}
          disabled={loadingTasks}
          className="inline-flex shrink-0 items-center justify-center gap-2 rounded-xl border border-gray-700 bg-gray-900 px-4 py-2.5 text-sm text-gray-200 transition-all hover:bg-gray-800 active:scale-95 disabled:opacity-60"
        >
          <RefreshCw className={`h-4 w-4 ${loadingTasks ? 'animate-spin' : ''}`} />
          {t('common.refresh')}
        </button>
      </div>
      )}

      <TaskCenter
        tasks={tasks}
        loading={loadingTasks}
        backgroundPaused={storageIO?.paused}
        taskActionKey={taskActionKey}
        filters={taskFilters}
        typeOptions={TASK_TYPE_OPTIONS}
        currentFilterCanClear={currentTaskFilterCanClear}
        onRefresh={fetchTasks}
        onTaskAction={runTaskAction}
        onFilterChange={updateTaskFilters}
        onClearTasks={clearTasks}
        onOpenTaskTarget={openTaskTarget}
        onViewTaskLogs={onViewTaskLogs}
      />
    </div>
  );
}
