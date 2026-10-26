import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient } from '../../api/client';
import type { RunEventsResponse, RunSamplesResponse, RunLive, RunSnapshot, TaskSummary } from '../../api/generated';
import type { TaskAction, TaskCenterFilters, TaskRunHistory, RunDetailView } from '../../components/tasks/TaskCenter';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';
import { applyLiveSummary, applyRunToLive } from '../../utils/runLive';
import { useSessionRecovery, useSessionRunPush } from '../../session/ApplicationSessionProvider';

const EMPTY_LIVE: RunLive = { active: 0, queued: 0, slots: 0, paused: false, paused_all: false, runs: [] };

/** 每个任务中心的查询与运行投影；筛选草稿由页面持有，恢复信号由会话提供。 */
export function useTaskObservation(filters: TaskCenterFilters, taskReloadToken: number) {
  const { t } = useI18n();
  const { showToast } = useToast();
  const { status: runStatusFilter, scope: taskScopeFilter, type: taskTypeFilter, scopeId: appliedTaskScopeId, query: appliedTaskQuery } = filters;
  const [live, setLive] = useState<RunLive>(EMPTY_LIVE);
  const [tasks, setTasks] = useState<TaskSummary[]>([]);
  const [loadingTasks, setLoadingTasks] = useState(false);
  const [taskActionKey, setTaskActionKey] = useState<string | null>(null);
  const [history, setHistory] = useState<TaskRunHistory | undefined>(undefined);
  const [detail, setDetail] = useState<RunDetailView | undefined>(undefined);
  const [bulkPauseBusy, setBulkPauseBusy] = useState(false);
  const mounted = useRef(true);
  const liveRequestIDRef = useRef(0);
  const taskRequestIDRef = useRef(0);
  const historyRequestIDRef = useRef(0);
  const detailRequestIDRef = useRef(0);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      liveRequestIDRef.current += 1;
      taskRequestIDRef.current += 1;
      historyRequestIDRef.current += 1;
      detailRequestIDRef.current += 1;
    };
  }, []);

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
    if (!mounted.current) return;
    const requestID = ++liveRequestIDRef.current;
    try {
      const res = await apiClient.get<RunLive>('/api/system/tasks/live');
      if (requestID !== liveRequestIDRef.current) return;
      // 认不出形状就当作没有实况可显示：画一片空区，而不是让一份残缺的载荷把整页拖倒。
      setLive(Array.isArray(res.data?.runs) ? res.data : EMPTY_LIVE);
    } catch (error) {
      console.error(error);
    }
  }, []);

  const fetchTasks = useCallback(async () => {
    if (!mounted.current) return;
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
    if (!mounted.current) return;
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

  useEffect(() => {
    fetchTasks();
    fetchLive();
  }, [fetchLive, fetchTasks, taskReloadToken]);

  useSessionRunPush(frame => {
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
  });

  const expandedTaskId = useRef<number | undefined>(undefined);
  expandedTaskId.current = history?.taskId;
  const recover = useCallback(() => {
    void fetchTasks();
    void fetchLive();
    if (expandedTaskId.current !== undefined) void fetchTaskRuns(expandedTaskId.current);
  }, [fetchTasks, fetchLive, fetchTaskRuns]);
  useSessionRecovery(recover);

  // 会话恢复立即补拉；每分钟轮询继续兜住没有后续推送的遗漏。
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
      if (!mounted.current) return;
      showToast(t(`settings.maintenance.taskAction.${action}Success`));
      await Promise.all([fetchTasks(), fetchLive()]);
      if (history) await fetchTaskRuns(history.taskId);
    } catch (error) {
      console.error(error);
      if (!mounted.current) return;
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
      if (!mounted.current) return;
      showToast(t(`settings.maintenance.taskAction.${action}Success`));
      await fetchTasks();
    } catch (error) {
      console.error(error);
      if (!mounted.current) return;
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
      if (!mounted.current) return;
      showToast(t(action === 'pause-all' ? 'settings.maintenance.pauseAllSuccess' : 'settings.maintenance.resumeAllSuccess'));
      await Promise.all([fetchTasks(), fetchLive()]);
    } catch (error) {
      console.error(error);
      if (!mounted.current) return;
      showToast(t(action === 'pause-all' ? 'settings.maintenance.pauseAllFailed' : 'settings.maintenance.resumeAllFailed'), 'error');
    } finally {
      setBulkPauseBusy(false);
    }
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
      if (!mounted.current) return;
      await Promise.all([fetchTasks(), fetchLive()]);
    } catch (error) {
      console.error(error);
      if (!mounted.current) return;
      showToast(t('organize.toast.actionFailed'), 'error');
    }
  };

  return { live, tasks, loadingTasks, taskActionKey, history, detail, bulkPauseBusy,
    toggleRunDetail, toggleTask, runTaskAction, toggleTaskAuto, runBulkPause, clearTasks };
}
