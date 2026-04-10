import { useEffect, useMemo, useState } from 'react';
import { AlertCircle, AlertTriangle, CheckCircle2, Copy, Info, RefreshCw, RotateCcw, Search, Terminal } from 'lucide-react';
import { format } from 'date-fns';

interface LogEntry {
  time: string;
  level: string;
  msg: string;
  raw: string;
}

interface LogsResponse {
  items: LogEntry[];
  summary: {
    total: number;
    by_level: Record<string, number>;
  };
}

interface TaskStatus {
  key: string;
  type: string;
  scope: string;
  scope_id?: number;
  status: string;
  message: string;
  error?: string;
  current: number;
  total: number;
  retryable: boolean;
  started_at: string;
  updated_at: string;
  finished_at?: string;
}

export default function Logs() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [summary, setSummary] = useState<LogsResponse['summary']>({ total: 0, by_level: { ERROR: 0, WARN: 0, INFO: 0 } });
  const [tasks, setTasks] = useState<TaskStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filterLevel, setFilterLevel] = useState('ALL');
  const [query, setQuery] = useState('');
  const [taskStatusFilter, setTaskStatusFilter] = useState('ALL');
  const [taskScopeFilter, setTaskScopeFilter] = useState('ALL');
  const [taskQuery, setTaskQuery] = useState('');
  const [retryingTaskKey, setRetryingTaskKey] = useState<string | null>(null);
  const [copiedIndex, setCopiedIndex] = useState<number | null>(null);

  const fetchData = async () => {
    setLoading(true);
    setError(null);
    try {
      const taskParams = new URLSearchParams();
      taskParams.set('limit', '50');
      if (taskStatusFilter !== 'ALL') taskParams.set('status', taskStatusFilter);
      if (taskScopeFilter !== 'ALL') taskParams.set('scope', taskScopeFilter);
      if (taskQuery.trim()) taskParams.set('q', taskQuery.trim());

      const [logsResp, tasksResp] = await Promise.all([
        fetch(`/api/system/logs?limit=300&level=${filterLevel}&q=${encodeURIComponent(query)}`),
        fetch(`/api/system/tasks?${taskParams.toString()}`),
      ]);

      if (!logsResp.ok) {
        throw new Error('无法加载日志');
      }

      const logsData: LogsResponse = await logsResp.json();
      setLogs(logsData.items || []);
      setSummary(logsData.summary || { total: 0, by_level: { ERROR: 0, WARN: 0, INFO: 0 } });

      if (tasksResp.ok) {
        const tasksData: TaskStatus[] = await tasksResp.json();
        setTasks(Array.isArray(tasksData) ? tasksData : []);
      }
    } catch (err: any) {
      setError(err.message || '未知错误');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterLevel, taskStatusFilter, taskScopeFilter]);

  const failedTasks = useMemo(() => tasks.filter((task) => task.status === 'failed'), [tasks]);

  const formatLogTime = (timeStr: string) => {
    try {
      return format(new Date(timeStr), 'yyyy-MM-dd HH:mm:ss');
    } catch {
      return timeStr;
    }
  };

  const copyRawLog = async (raw: string, index: number) => {
    try {
      await navigator.clipboard.writeText(raw);
      setCopiedIndex(index);
      window.setTimeout(() => setCopiedIndex(null), 1500);
    } catch (err) {
      console.error('copy failed', err);
    }
  };

  const badgeClass = (status: string) => {
    switch (status) {
      case 'failed':
        return 'bg-red-500/10 text-red-300 border-red-500/20';
      case 'completed':
        return 'bg-emerald-500/10 text-emerald-300 border-emerald-500/20';
      default:
        return 'bg-blue-500/10 text-blue-300 border-blue-500/20';
    }
  };

  const retryTask = async (taskKey: string) => {
    setRetryingTaskKey(taskKey);
    try {
      const resp = await fetch(`/api/system/tasks/${encodeURIComponent(taskKey)}/retry`, { method: 'POST' });
      if (!resp.ok) {
        const data = await resp.json().catch(() => null);
        throw new Error(data?.error || '任务重试失败');
      }
      await fetchData();
    } catch (err) {
      console.error(err);
      setError(err instanceof Error ? err.message : '任务重试失败');
    } finally {
      setRetryingTaskKey(null);
    }
  };

  return (
    <div className="p-6 max-w-[1600px] mx-auto space-y-6">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white">运维看板</h1>
          <p className="text-slate-400 mt-1">查看结构化日志、最近任务和失败上下文。</p>
        </div>

        <div className="flex flex-col gap-3 sm:flex-row">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-500" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && fetchData()}
              placeholder="搜索日志关键字"
              className="w-full sm:w-64 rounded-lg border border-slate-700 bg-slate-900 pl-10 pr-4 py-2 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
            />
          </div>
          <select
            value={filterLevel}
            onChange={(e) => setFilterLevel(e.target.value)}
            className="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
          >
            <option value="ALL">全部级别</option>
            <option value="ERROR">仅错误</option>
            <option value="WARN">仅警告</option>
            <option value="INFO">仅信息</option>
          </select>
          <button
            onClick={fetchData}
            disabled={loading}
            className="inline-flex items-center justify-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-500 disabled:opacity-60"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            刷新
          </button>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="匹配日志" value={summary.total} tone="blue" />
        <MetricCard label="错误" value={summary.by_level.ERROR || 0} tone="red" />
        <MetricCard label="警告" value={summary.by_level.WARN || 0} tone="amber" />
        <MetricCard label="最近失败任务" value={failedTasks.length} tone="purple" />
      </div>

      <div className="grid gap-6 xl:grid-cols-[1.55fr_1fr]">
        <div className="rounded-2xl border border-slate-800 bg-slate-950 overflow-hidden">
          <div className="flex items-center gap-2 border-b border-slate-800 px-4 py-3">
            <Terminal className="w-4 h-4 text-slate-400" />
            <span className="text-sm text-slate-300">系统日志</span>
          </div>
          <div className="max-h-[70vh] overflow-auto p-4 font-mono text-sm">
            {loading && logs.length === 0 ? (
              <div className="flex h-56 items-center justify-center text-slate-500">
                <RefreshCw className="w-6 h-6 animate-spin" />
              </div>
            ) : error ? (
              <div className="flex h-56 flex-col items-center justify-center gap-2 text-red-300">
                <AlertTriangle className="w-8 h-8" />
                <span>{error}</span>
              </div>
            ) : logs.length === 0 ? (
              <div className="flex h-56 flex-col items-center justify-center gap-2 text-slate-500">
                <Info className="w-8 h-8" />
                <span>当前筛选条件下没有日志。</span>
              </div>
            ) : (
              <div className="space-y-2">
                {logs.map((log, index) => (
                  <div key={`${log.time}-${index}`} className="rounded-xl border border-slate-800 bg-slate-900/70 p-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-slate-500">{formatLogTime(log.time)}</span>
                      <span className={`rounded-full border px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wide ${log.level === 'ERROR' ? 'border-red-500/30 bg-red-500/10 text-red-300' : log.level === 'WARN' ? 'border-amber-500/30 bg-amber-500/10 text-amber-300' : 'border-blue-500/30 bg-blue-500/10 text-blue-300'}`}>
                        {log.level}
                      </span>
                      <button
                        onClick={() => copyRawLog(log.raw, index)}
                        className="ml-auto inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-slate-400 hover:bg-slate-800 hover:text-white"
                      >
                        <Copy className="w-3 h-3" />
                        {copiedIndex === index ? '已复制' : '复制原文'}
                      </button>
                    </div>
                    <p className="mt-2 whitespace-pre-wrap break-words text-slate-200">{log.msg || log.raw}</p>
                    {log.msg !== log.raw && (
                      <p className="mt-2 whitespace-pre-wrap break-words text-xs text-slate-500">{log.raw}</p>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        <div className="space-y-6">
          <div className="rounded-2xl border border-slate-800 bg-slate-900 p-4">
            <div className="mb-3 flex items-center gap-2">
              <AlertCircle className="w-4 h-4 text-slate-400" />
              <h2 className="text-sm font-semibold text-white">任务中心</h2>
            </div>
            <div className="mb-4 grid gap-2">
              <div className="grid grid-cols-2 gap-2">
                <select
                  value={taskStatusFilter}
                  onChange={(e) => setTaskStatusFilter(e.target.value)}
                  className="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-slate-200"
                >
                  <option value="ALL">全部状态</option>
                  <option value="running">运行中</option>
                  <option value="failed">失败</option>
                  <option value="completed">完成</option>
                </select>
                <select
                  value={taskScopeFilter}
                  onChange={(e) => setTaskScopeFilter(e.target.value)}
                  className="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-slate-200"
                >
                  <option value="ALL">全部范围</option>
                  <option value="system">系统</option>
                  <option value="library">资源库</option>
                  <option value="series">系列</option>
                </select>
              </div>
              <div className="flex gap-2">
                <input
                  value={taskQuery}
                  onChange={(e) => setTaskQuery(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && fetchData()}
                  placeholder="搜索任务"
                  className="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-slate-200"
                />
                <button
                  onClick={fetchData}
                  className="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-slate-300 hover:bg-slate-800"
                >
                  查询
                </button>
              </div>
            </div>
            <div className="space-y-3">
              {tasks.length === 0 ? (
                <p className="text-sm text-slate-500">暂时没有后台任务记录。</p>
              ) : (
                tasks.map((task) => (
                  <div key={task.key} className="rounded-xl border border-slate-800 bg-slate-950 p-3">
                    <div className="flex items-center gap-2">
                      <span className={`rounded-full border px-2 py-0.5 text-[11px] font-semibold uppercase ${badgeClass(task.status)}`}>
                        {task.status}
                      </span>
                      <span className="text-xs text-slate-500">{task.scope}{task.scope_id ? ` #${task.scope_id}` : ''}</span>
                      {task.retryable && task.status !== 'running' && (
                        <button
                          onClick={() => retryTask(task.key)}
                          disabled={retryingTaskKey === task.key}
                          className="ml-auto inline-flex items-center gap-1 rounded-md border border-slate-700 px-2 py-1 text-[11px] text-slate-300 hover:bg-slate-800 disabled:opacity-60"
                        >
                          <RotateCcw className={`w-3 h-3 ${retryingTaskKey === task.key ? 'animate-spin' : ''}`} />
                          重试
                        </button>
                      )}
                    </div>
                    <p className="mt-2 text-sm text-slate-100">{task.message}</p>
                    <p className="mt-1 text-xs text-slate-500">{task.current}/{task.total || 1} · {formatLogTime(task.updated_at)}</p>
                    {task.error && (
                      <p className="mt-2 rounded-lg border border-red-500/20 bg-red-500/10 px-2 py-2 text-xs text-red-200">{task.error}</p>
                    )}
                  </div>
                ))
              )}
            </div>
          </div>

          <div className="rounded-2xl border border-slate-800 bg-slate-900 p-4">
            <div className="mb-3 flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-slate-400" />
              <h2 className="text-sm font-semibold text-white">使用建议</h2>
            </div>
            <ul className="space-y-2 text-sm text-slate-400">
              <li>先看失败任务，再回到日志里按关键字搜索同一时间段的上下文。</li>
              <li>当缩略图或搜索异常时，优先在设置页重建对应缓存，而不是直接重扫整库。</li>
              <li>LLM 任务失败时，重点检查协议模式、请求路径和模型名是否匹配。</li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  );
}

function MetricCard({ label, value, tone }: { label: string; value: number; tone: 'blue' | 'red' | 'amber' | 'purple' }) {
  const toneClass = {
    blue: 'border-blue-500/20 bg-blue-500/10 text-blue-200',
    red: 'border-red-500/20 bg-red-500/10 text-red-200',
    amber: 'border-amber-500/20 bg-amber-500/10 text-amber-200',
    purple: 'border-purple-500/20 bg-purple-500/10 text-purple-200',
  }[tone];

  return (
    <div className={`rounded-2xl border p-4 ${toneClass}`}>
      <p className="text-sm opacity-80">{label}</p>
      <p className="mt-2 text-3xl font-semibold text-white">{value}</p>
    </div>
  );
}
