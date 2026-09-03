import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiClient } from '../api/client';
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
  // 两个文本框（搜索、目标 ID）都配了回车与「查询」按钮，本意就是打完再查。已提交的那一份
  // 单独存：绑到请求参数上的是它，不是正在打的字。否则每敲一个字符发一次 /api/system/tasks，
  // 且 fetchTasks 的身份跟着变，15s 轮询的定时器被反复重建，打字期间永远不到点。
  const [appliedTaskQuery, setAppliedTaskQuery] = useState('');
  const [appliedTaskScopeId, setAppliedTaskScopeId] = useState('');
  // 提交同一份条件时也要重取一次，靠它把 effect 推一下。
  const [taskReloadToken, setTaskReloadToken] = useState(0);
  const [storageIO, setStorageIO] = useState<StorageIODiagnostics | null>(null);
  const taskRequestIDRef = useRef(0);
  const { showToast } = useToast();

  // taskFilters 是输入框的当前值（含还没提交的半截关键词）。
  const taskFilters = useMemo<TaskCenterFilters>(() => ({
    status: taskStatusFilter,
    scope: taskScopeFilter,
    type: taskTypeFilter,
    scopeId: taskScopeIdFilter,
    query: taskQuery,
  }), [taskQuery, taskScopeFilter, taskScopeIdFilter, taskStatusFilter, taskTypeFilter]);

  // SSE 增量帧按**已提交**的条件判去留：与列表里那批任务是同一把尺子。
  const appliedTaskFilters = useMemo<TaskCenterFilters>(() => ({
    status: taskStatusFilter,
    scope: taskScopeFilter,
    type: taskTypeFilter,
    scopeId: appliedTaskScopeId,
    query: appliedTaskQuery,
  }), [appliedTaskQuery, appliedTaskScopeId, taskScopeFilter, taskStatusFilter, taskTypeFilter]);

  const buildTaskParams = useCallback((status?: string) => {
    const params = new URLSearchParams({ limit: '50' });
    if (status) params.set('status', status);
    if (!status && taskStatusFilter !== 'ALL') params.set('status', taskStatusFilter);
    if (taskScopeFilter !== 'ALL') params.set('scope', taskScopeFilter);
    if (taskTypeFilter !== 'ALL') params.set('type', taskTypeFilter);
    if (appliedTaskScopeId.trim()) params.set('scope_id', appliedTaskScopeId.trim());
    if (appliedTaskQuery.trim()) params.set('q', appliedTaskQuery.trim());
    return params;
  }, [appliedTaskQuery, appliedTaskScopeId, taskScopeFilter, taskStatusFilter, taskTypeFilter]);

  const fetchStorageIO = useCallback(async () => {
    try {
      const res = await apiClient.get<StorageIODiagnostics>('/api/system/storage-io');
      setStorageIO(res.data);
    } catch (error) {
      console.error(error);
    }
  }, []);

  const fetchTasks = useCallback(async () => {
    // 世代号对不上就整份丢弃：慢网下先发的响应后到，不能盖掉按新条件取回的那一份。
    const requestID = taskRequestIDRef.current + 1;
    taskRequestIDRef.current = requestID;
    setLoadingTasks(true);
    try {
      const res = await apiClient.get<TaskStatus[]>(`/api/system/tasks?${buildTaskParams().toString()}`);
      if (requestID !== taskRequestIDRef.current) return;
      const items = Array.isArray(res.data) ? res.data : [];
      const seen = new Set<string>();
      setTasks(items.filter((task) => {
        if (seen.has(task.key)) return false;
        seen.add(task.key);
        return true;
      }).slice(0, 50));
    } catch (error) {
      if (requestID !== taskRequestIDRef.current) return;
      console.error(error);
      showToast(t('settings.maintenance.taskCenterLoadFailed'), 'error');
    } finally {
      if (requestID === taskRequestIDRef.current) setLoadingTasks(false);
    }
  }, [buildTaskParams, showToast, t]);

  // 回车 / 「查询」/「刷新」都走这里：把输入框里的内容提交为生效条件，并重取一次。
  const applyTaskFilters = useCallback(() => {
    setAppliedTaskQuery(taskQuery);
    setAppliedTaskScopeId(taskScopeIdFilter);
    setTaskReloadToken((token) => token + 1);
  }, [taskQuery, taskScopeIdFilter]);

  useEffect(() => {
    fetchTasks();
    fetchStorageIO();
  }, [fetchStorageIO, fetchTasks, taskReloadToken]);

  useEffect(() => {
    // 复用 Layout 中已挂载的全局 EventSource：它接收 task_progress 后会
    // dispatch 'manga-manager:task-progress' 自定义事件。这里只监听自定义事件，
    // 避免对同一 origin 再开第二条 SSE 长连接占用浏览器并发额度。
    const handler = (event: Event) => {
      const task = (event as CustomEvent<TaskStatus>).detail;
      if (!task || typeof task !== 'object') return;
      setTasks((prev) => {
        const matchesStatus = appliedTaskFilters.status === 'ALL' || task.status === appliedTaskFilters.status;
        const matchesScope = appliedTaskFilters.scope === 'ALL' || task.scope === appliedTaskFilters.scope;
        const matchesType = appliedTaskFilters.type === 'ALL' || task.type === appliedTaskFilters.type;
        const matchesScopeId = !appliedTaskFilters.scopeId.trim() || String(task.scope_id || '') === appliedTaskFilters.scopeId.trim();
        const q = appliedTaskFilters.query.trim().toLowerCase();
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
  }, [appliedTaskFilters]);

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
      await apiClient.post(`/api/system/tasks/${encodeURIComponent(task.key)}/${action}`);
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
        if (appliedTaskScopeId.trim()) params.set('scope_id', appliedTaskScopeId.trim());
      }
      await apiClient.delete(`/api/system/tasks?${params.toString()}`);
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
          onClick={applyTaskFilters}
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
        onRefresh={applyTaskFilters}
        onTaskAction={runTaskAction}
        onFilterChange={updateTaskFilters}
        onClearTasks={clearTasks}
        onOpenTaskTarget={openTaskTarget}
        onViewTaskLogs={onViewTaskLogs}
      />
    </div>
  );
}
