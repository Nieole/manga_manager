import { useCallback, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Activity, RefreshCw } from 'lucide-react';
import { TaskCenter, type TaskCenterFilters, type TaskTarget, type RunSnapshot } from '../components/tasks/TaskCenter';
import { useI18n } from '../i18n/LocaleProvider';
import { isLiveRunStatus } from '../utils/runStatus';
import { useTaskObservation } from '../features/task-observation/useTaskObservation';

const TASK_TYPE_OPTIONS = [
  'relocation_check',
  'scan_library',
  'scan_external_library',
  'scan_series',
  'generate_covers',
  'cleanup_library',
  'rebuild_index',
  'rebuild_thumbnails',
  'cleanup_thumbnails',
  'cleanup_run_history',
  'rebuild_file_identities',
  'scrape',
  'ai_grouping',
  'rebuild_book_hashes',
  'reconcile_koreader_progress',
  'refresh_koreader_matching',
  'transfer_external_library',
];

interface BackgroundTasksProps {
  embedded?: boolean;
  // onViewRawLogs 是「原始日志」那个入口：按**这一次运行**过滤全局日志。详情面板不走它——
  // 曲线与事件流是这条运行自己的东西，本页按需取回后交给任务中心渲染。
  onViewRawLogs?: (run: RunSnapshot) => void;
}

export default function BackgroundTasks({ embedded = false, onViewRawLogs }: BackgroundTasksProps = {}) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [runStatusFilter, setRunStatusFilter] = useState('ALL');
  const [taskScopeFilter, setTaskScopeFilter] = useState('ALL');
  const [taskTypeFilter, setTaskTypeFilter] = useState('ALL');
  const [taskScopeIdFilter, setTaskScopeIdFilter] = useState('');
  const [taskQuery, setTaskQuery] = useState('');
  // 两个文本框（搜索、目标 ID）都配了回车与「查询」按钮，本意就是打完再查。已提交的那一份
  // 单独存：绑到请求参数上的是它，不是正在打的字。否则每敲一个字符发一次请求，
  // 且 fetchTasks 的身份跟着变，轮询的定时器被反复重建，打字期间永远不到点。
  const [appliedTaskQuery, setAppliedTaskQuery] = useState('');
  const [appliedTaskScopeId, setAppliedTaskScopeId] = useState('');
  // 提交同一份条件时也要重取一次，靠它把 effect 推一下。
  const [taskReloadToken, setTaskReloadToken] = useState(0);
  // taskFilters 是输入框的当前值（含还没提交的半截关键词）。
  const taskFilters = useMemo<TaskCenterFilters>(() => ({
    status: runStatusFilter,
    scope: taskScopeFilter,
    type: taskTypeFilter,
    scopeId: taskScopeIdFilter,
    query: taskQuery,
  }), [taskQuery, taskScopeFilter, taskScopeIdFilter, runStatusFilter, taskTypeFilter]);

  const appliedFilters = useMemo(() => ({ ...taskFilters, query: appliedTaskQuery, scopeId: appliedTaskScopeId }), [taskFilters, appliedTaskQuery, appliedTaskScopeId]);
  const { live, tasks, loadingTasks, taskActionKey, history, detail, bulkPauseBusy,
    toggleRunDetail, toggleTask, runTaskAction, toggleTaskAuto, runBulkPause, clearTasks } = useTaskObservation(appliedFilters, taskReloadToken);

  // 回车 /「查询」/「刷新」都走这里：把输入框里的内容提交为生效条件，并重取一次。
  const applyTaskFilters = useCallback(() => {
    setAppliedTaskQuery(taskQuery);
    setAppliedTaskScopeId(taskScopeIdFilter);
    setTaskReloadToken((token) => token + 1);
  }, [taskQuery, taskScopeIdFilter]);

  // 仍会变化的运行永不被清除，因此选中那几种状态时「按当前筛选清理」一条都删不掉。
  const currentTaskFilterCanClear = runStatusFilter !== 'ALL' && !isLiveRunStatus(runStatusFilter);

  const updateTaskFilters = (patch: Partial<TaskCenterFilters>) => {
    if (patch.status !== undefined) setRunStatusFilter(patch.status);
    if (patch.scope !== undefined) setTaskScopeFilter(patch.scope);
    if (patch.type !== undefined) setTaskTypeFilter(patch.type);
    if (patch.scopeId !== undefined) setTaskScopeIdFilter(patch.scopeId);
    if (patch.query !== undefined) setTaskQuery(patch.query);
  };

  const openTaskTarget = (target: TaskTarget) => {
    if (target.scope === 'series' && target.scope_id) {
      navigate(`/series/${target.scope_id}`);
      return;
    }
    if (target.scope === 'library' && target.scope_id) {
      navigate(`/library/${target.scope_id}`);
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
        live={live}
        tasks={tasks}
        loading={loadingTasks}
        bulkPauseBusy={bulkPauseBusy}
        taskActionKey={taskActionKey}
        history={history}
        filters={taskFilters}
        typeOptions={TASK_TYPE_OPTIONS}
        currentFilterCanClear={currentTaskFilterCanClear}
        onRefresh={applyTaskFilters}
        onTaskAction={runTaskAction}
        onToggleTask={toggleTask}
        onPauseAll={() => runBulkPause('pause-all')}
        onResumeAll={() => runBulkPause('resume-all')}
        onFilterChange={updateTaskFilters}
        onClearTasks={clearTasks}
        onOpenTaskTarget={openTaskTarget}
        detail={detail}
        onViewRunDetail={toggleRunDetail}
        onViewRawLogs={onViewRawLogs}
        onToggleTaskAuto={toggleTaskAuto}
      />
    </div>
  );
}
