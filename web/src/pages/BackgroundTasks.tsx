/**
 * 任务中心页：取两层各自的数据，展开某一行时再按需取它的历次运行。
 * 三条取数路径分明——实况帧不带筛选，任务清单带筛选，历次运行按 task_id 单独取。
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiClient } from '../api/client';
import { Activity, RefreshCw } from 'lucide-react';
import { TaskCenter, type TaskAction, type TaskCenterFilters, type TaskRunHistory, type TaskTarget, type RunDetailView, type RunLive, type RunSnapshot, type TaskSummary } from '../components/tasks/TaskCenter';
import type { RunEventsResponse, RunPush, RunSamplesResponse } from '../api/generated';
import { useI18n } from '../i18n/LocaleProvider';
import { useToast } from '../components/ToastProvider';
import { applyLiveSummary, applyRunToLive } from '../utils/runLive';
import { trackRunPush } from '../utils/runPush';
import { isLiveRunStatus } from '../utils/runStatus';

const TASK_TYPE_OPTIONS = [
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

// 实况帧取不回来时的兜底：一条运行都没有、也没有上限可报。它不是「系统闲着」的断言，
// 只是「这一刻没有可显示的实况」——界面因此画空区，而不是画一个凭空的槽位占用。
const EMPTY_LIVE: RunLive = { active: 0, queued: 0, slots: 0, paused: false, paused_all: false, runs: [] };

interface BackgroundTasksProps {
  embedded?: boolean;
  // onViewRawLogs 是「原始日志」那个入口：按**这一次运行**过滤全局日志。详情面板不走它——
  // 曲线与事件流是这条运行自己的东西，本页按需取回后交给任务中心渲染。
  onViewRawLogs?: (run: RunSnapshot) => void;
}

export default function BackgroundTasks({ embedded = false, onViewRawLogs }: BackgroundTasksProps = {}) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [live, setLive] = useState<RunLive>(EMPTY_LIVE);
  const [tasks, setTasks] = useState<TaskSummary[]>([]);
  const [loadingTasks, setLoadingTasks] = useState(false);
  const [taskActionKey, setTaskActionKey] = useState<string | null>(null);
  // 展开的那一行与它的历次运行：只留展开中的那一份，收起时连同它一起丢掉——
  // 留着的话再展开会先闪一眼过期的历史。
  const [history, setHistory] = useState<TaskRunHistory | undefined>(undefined);
  // 打开着详情面板的那一张卡片。事件与**采样**都**不进推送通道**，因此两份都是纯按需取回的，
  // 只留一张：留着上一张的话，再点开会先闪一眼别人的曲线。
  const [detail, setDetail] = useState<RunDetailView | undefined>(undefined);
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
  const [bulkPauseBusy, setBulkPauseBusy] = useState(false);
  const taskRequestIDRef = useRef(0);
  const historyRequestIDRef = useRef(0);
  const detailRequestIDRef = useRef(0);
  // lastPushSequenceRef 是上一帧推送帧的序号；null 表示手上还没有可比的号（刚进页面，或 SSE 刚重连上）。
  const lastPushSequenceRef = useRef<number | null>(null);
  const { showToast } = useToast();

  // taskFilters 是输入框的当前值（含还没提交的半截关键词）。
  const taskFilters = useMemo<TaskCenterFilters>(() => ({
    status: runStatusFilter,
    scope: taskScopeFilter,
    type: taskTypeFilter,
    scopeId: taskScopeIdFilter,
    query: taskQuery,
  }), [taskQuery, taskScopeFilter, taskScopeIdFilter, runStatusFilter, taskTypeFilter]);

  const buildTaskParams = useCallback(() => {
    const params = new URLSearchParams({ limit: '50' });
    if (runStatusFilter !== 'ALL') params.set('status', runStatusFilter);
    if (taskScopeFilter !== 'ALL') params.set('scope', taskScopeFilter);
    if (taskTypeFilter !== 'ALL') params.set('type', taskTypeFilter);
    if (appliedTaskScopeId.trim()) params.set('scope_id', appliedTaskScopeId.trim());
    if (appliedTaskQuery.trim()) params.set('q', appliedTaskQuery.trim());
    return params;
  }, [appliedTaskQuery, appliedTaskScopeId, taskScopeFilter, runStatusFilter, taskTypeFilter]);

  const fetchLive = useCallback(async () => {
    try {
      const res = await apiClient.get<RunLive>('/api/system/tasks/live');
      // 认不出形状就当作没有实况可显示：画一片空区，而不是让一份残缺的载荷把整页拖倒。
      setLive(Array.isArray(res.data?.runs) ? res.data : EMPTY_LIVE);
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
      const res = await apiClient.get<TaskSummary[]>(`/api/system/tasks/summary?${buildTaskParams().toString()}`);
      if (requestID !== taskRequestIDRef.current) return;
      setTasks(Array.isArray(res.data) ? res.data : []);
    } catch (error) {
      if (requestID !== taskRequestIDRef.current) return;
      console.error(error);
      showToast(t('settings.maintenance.taskCenterLoadFailed'), 'error');
    } finally {
      if (requestID === taskRequestIDRef.current) setLoadingTasks(false);
    }
  }, [buildTaskParams, showToast, t]);

  // 历次运行按需取：清单接口一条运行都不带回来，展开哪一行才去问哪一个任务。
  //
  // 世代号与清单那条同理：慢网下「展开 A、收起、展开 B」会让 A 的响应后到，而它一到就会把
  // A 的历次运行画在 B 底下。对不上就整份丢弃。
  const fetchTaskRuns = useCallback(async (taskId: number) => {
    const requestID = historyRequestIDRef.current + 1;
    historyRequestIDRef.current = requestID;
    setHistory({ taskId, loading: true });
    try {
      const res = await apiClient.get<RunSnapshot[]>(`/api/system/tasks?task_id=${taskId}&limit=20`);
      if (requestID !== historyRequestIDRef.current) return;
      setHistory({ taskId, runs: Array.isArray(res.data) ? res.data : [] });
    } catch (error) {
      if (requestID !== historyRequestIDRef.current) return;
      console.error(error);
      setHistory({ taskId, runs: [] });
      showToast(t('settings.maintenance.taskCenterLoadFailed'), 'error');
    }
  }, [showToast, t]);

  // 详情面板的两份都按需取：一条运行的事件可能上千条、采样点可能上千个，列表接口一份都不带
  // 回来，点开哪一条才去问哪一条。
  //
  // 两条请求**并发发出、各自成败**：其中一条 500 不该把另一条已经拿到的东西一起抹掉——
  // 用户点开的那一刻，曲线与事件流各自答着一个问题，能答一个总好过一个都不答。
  // 世代号与另外两条取数同理：慢网下「点开 A、关掉、点开 B」会让 A 的响应后到，对不上就整份丢弃。
  const fetchRunDetail = useCallback(async (runID: number, cardId: string) => {
    const requestID = detailRequestIDRef.current + 1;
    detailRequestIDRef.current = requestID;
    setDetail({ cardId, loading: true });
    const [events, samples] = await Promise.all([
      apiClient.get<RunEventsResponse>(`/api/system/runs/${runID}/events`).then((res) => res.data).catch((error) => {
        console.error(error);
        return undefined;
      }),
      apiClient.get<RunSamplesResponse>(`/api/system/runs/${runID}/samples`).then((res) => res.data).catch((error) => {
        console.error(error);
        return undefined;
      }),
    ]);
    if (requestID !== detailRequestIDRef.current) return;
    if (!events && !samples) {
      setDetail(undefined);
      showToast(t('settings.maintenance.taskCenterLoadFailed'), 'error');
      return;
    }
    setDetail({ cardId, events, samples });
  }, [showToast, t]);

  // 再点一次同一张卡片就是关掉它，并让在途的响应作废。认卡片而不是认运行：同一条运行会同时
  // 出现在实况区与展开着的历次运行里，认运行的话两张卡片下面各画一份。
  const toggleRunDetail = useCallback((run: RunSnapshot, cardId: string) => {
    if (detail?.cardId === cardId) {
      detailRequestIDRef.current += 1;
      setDetail(undefined);
      return;
    }
    void fetchRunDetail(run.run_id, cardId);
  }, [detail, fetchRunDetail]);

  // 收起就是把那一份丢掉，并让在途的响应作废——它回来时那一行已经不展开了。
  const toggleTask = useCallback((taskId: number) => {
    if (history?.taskId === taskId) {
      historyRequestIDRef.current += 1;
      setHistory(undefined);
      return;
    }
    void fetchTaskRuns(taskId);
  }, [fetchTaskRuns, history]);

  // 回车 /「查询」/「刷新」都走这里：把输入框里的内容提交为生效条件，并重取一次。
  const applyTaskFilters = useCallback(() => {
    setAppliedTaskQuery(taskQuery);
    setAppliedTaskScopeId(taskScopeIdFilter);
    setTaskReloadToken((token) => token + 1);
  }, [taskQuery, taskScopeIdFilter]);

  useEffect(() => {
    fetchTasks();
    fetchLive();
  }, [fetchLive, fetchTasks, taskReloadToken]);

  useEffect(() => {
    // 复用 Layout 中已挂载的全局 EventSource：它把推送通道上的帧解开之后
    // dispatch 'manga-manager:run-push' 自定义事件。这里只监听自定义事件，
    // 避免对同一 origin 再开第二条 SSE 长连接占用浏览器并发额度。
    const handler = (event: Event) => {
      const frame = (event as CustomEvent<RunPush>).detail;
      if (!frame || typeof frame !== 'object') return;
      const tracked = trackRunPush(lastPushSequenceRef.current, frame);
      lastPushSequenceRef.current = tracked.sequence;

      const { live: summary, run } = frame;
      // 那几个汇总数由后端数好了发过来，不从运行列表里现算：这份列表本身可能正缺着一条。
      if (summary) setLive((prev) => applyLiveSummary(prev, summary));
      if (run) {
        // 谁还留在实况区，判据只有 applyRunToLive 一处——它的定序与后端同形。
        setLive((prev) => applyRunToLive(prev, run));
        // 清单那一行的「上次结果」跟着走：推出来的帧只属于仍会变化的运行，而那正是它所属任务
        // 最近的那一次。不跟的话，用户要等下一轮轮询才看得到刚发起的那条落在哪个任务下面。
        setTasks((prev) => prev.map((item) => (item.task_id === run.task_id ? { ...item, last_run: run } : item)));
        // 展开着的那一行同理：已在里面的按运行标识替换，新起的那一条补到最前。
        setHistory((prev) => {
          if (!prev?.runs || prev.taskId !== run.task_id) return prev;
          const runs = prev.runs.some((item) => item.run_id === run.run_id)
            ? prev.runs.map((item) => (item.run_id === run.run_id ? run : item))
            : [run, ...prev.runs];
          return { ...prev, runs };
        });
      }
      // 接不上上一帧：中间掉了东西，而掉的那一帧若恰好是终态，界面会一直停在过期的进度上。
      // 缺了什么只有库知道，因此整份重拉，而不是猜着补。
      if (tracked.gap) {
        fetchTasks();
        fetchLive();
      }
    };
    window.addEventListener('manga-manager:run-push', handler as EventListener);
    return () => window.removeEventListener('manga-manager:run-push', handler as EventListener);
  }, [fetchLive, fetchTasks]);

  // 轮询只是兜底：真正把丢帧补回来的是上面那条序号缺口判定，它在下一帧就纠正，
  // 而不必等这个定时器。因此这里放到 60s——一分钟一次的两个请求，代价可以忽略。
  useEffect(() => {
    const poll = window.setInterval(() => {
      fetchTasks();
      fetchLive();
    }, 60000);
    return () => window.clearInterval(poll);
  }, [fetchLive, fetchTasks]);

  // 三个控制动作作用在**运行**上，按运行 id 寻址；重试作用在**任务**上，按**任务 id** 寻址。
  // 同一个任务此刻可以有两条仍会变化的运行（一条在跑、一条排队），按任务发控制请求就答不出
  // 用户按的是哪一张卡片上的按钮。忙碌标记同理按运行分，否则一条排队会把在跑那条的按钮一起灰掉。
  const runTaskAction = async (run: RunSnapshot, action: TaskAction) => {
    const onTask = action === 'retry';
    setTaskActionKey(onTask ? `${run.task_id}:${action}` : `${run.run_id}:${action}`);
    try {
      await apiClient.post(onTask
        ? `/api/system/tasks/${run.task_id}/${action}`
        : `/api/system/runs/${run.run_id}/${action}`);
      showToast(t(`settings.maintenance.taskAction.${action}Success`));
      await Promise.all([fetchTasks(), fetchLive()]);
      if (history) await fetchTaskRuns(history.taskId);
    } catch (error) {
      console.error(error);
      showToast(t(`settings.maintenance.taskAction.${action}Failed`), 'error');
    } finally {
      setTaskActionKey(null);
    }
  };

  // 人工禁用作用在**任务**上，因此按 task_id 寻址，而不是像三个控制动作那样按运行 id。
  // 它只关掉自动发起：手动发起（重试）在禁用期间照旧可用，那条出口是刻意留着的。
  const toggleTaskAuto = async (task: TaskSummary) => {
    const action = task.disabled ? 'enable' : 'disable';
    setTaskActionKey(`${task.task_id}:auto`);
    try {
      await apiClient.post(`/api/system/tasks/${task.task_id}/${action}`);
      showToast(t(`settings.maintenance.taskAction.${action}Success`));
      await fetchTasks();
    } catch (error) {
      console.error(error);
      showToast(t(`settings.maintenance.taskAction.${action}Failed`), 'error');
    } finally {
      setTaskActionKey(null);
    }
  };

  // 全部暂停 / 全部恢复：后端把每条运行逐个按下或放行，这里只负责发一次请求再重取。
  const runBulkPause = async (action: 'pause-all' | 'resume-all') => {
    setBulkPauseBusy(true);
    try {
      await apiClient.post(`/api/system/tasks/${action}`);
      showToast(t(action === 'pause-all' ? 'settings.maintenance.pauseAllSuccess' : 'settings.maintenance.resumeAllSuccess'));
      await Promise.all([fetchTasks(), fetchLive()]);
    } catch (error) {
      console.error(error);
      showToast(t(action === 'pause-all' ? 'settings.maintenance.pauseAllFailed' : 'settings.maintenance.resumeAllFailed'), 'error');
    } finally {
      setBulkPauseBusy(false);
    }
  };

  // 仍会变化的运行永不被清除，因此选中那几种状态时「按当前筛选清理」一条都删不掉。
  const currentTaskFilterCanClear = runStatusFilter !== 'ALL' && !isLiveRunStatus(runStatusFilter);

  const updateTaskFilters = (patch: Partial<TaskCenterFilters>) => {
    if (patch.status !== undefined) setRunStatusFilter(patch.status);
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
      } else if (useCurrentFilters && runStatusFilter !== 'ALL') {
        params.set('status', runStatusFilter);
      }
      if (useCurrentFilters) {
        if (taskTypeFilter !== 'ALL') params.set('type', taskTypeFilter);
        if (taskScopeFilter !== 'ALL') params.set('scope', taskScopeFilter);
        if (appliedTaskScopeId.trim()) params.set('scope_id', appliedTaskScopeId.trim());
      }
      await apiClient.delete(`/api/system/tasks?${params.toString()}`);
      await Promise.all([fetchTasks(), fetchLive()]);
    } catch (error) {
      console.error(error);
      showToast(t('organize.toast.actionFailed'), 'error');
    }
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
