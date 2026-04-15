import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { AlertCircle, AlertTriangle, CheckCircle2, Copy, Info, RefreshCw, RotateCcw, Search, Terminal } from 'lucide-react';
import { format, formatDistanceToNow, isToday, isYesterday } from 'date-fns';

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
  scope_name?: string;
  status: string;
  message: string;
  error?: string;
  current: number;
  total: number;
  retryable: boolean;
  params?: Record<string, string>;
  started_at: string;
  updated_at: string;
  finished_at?: string;
}

export default function Logs() {
  const navigate = useNavigate();
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
  const [expandedTaskKey, setExpandedTaskKey] = useState<string | null>(null);
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
  const runningTasks = useMemo(() => tasks.filter((task) => task.status === 'running'), [tasks]);
  const completedTasks = useMemo(() => tasks.filter((task) => task.status === 'completed'), [tasks]);
  const groupedTasks = useMemo(() => {
    const today: TaskStatus[] = [];
    const yesterday: TaskStatus[] = [];
    const earlier: TaskStatus[] = [];

    tasks.forEach((task) => {
      const date = new Date(task.updated_at);
      if (isToday(date)) {
        today.push(task);
      } else if (isYesterday(date)) {
        yesterday.push(task);
      } else {
        earlier.push(task);
      }
    });

    return [
      { title: '今天', items: today },
      { title: '昨天', items: yesterday },
      { title: '更早', items: earlier },
    ].filter((group) => group.items.length > 0);
  }, [tasks]);

  const formatLogTime = (timeStr: string) => {
    try {
      return format(new Date(timeStr), 'yyyy-MM-dd HH:mm:ss');
    } catch {
      return timeStr;
    }
  };

  const formatTaskRelativeTime = (timeStr: string) => {
    try {
      return `${formatDistanceToNow(new Date(timeStr), { addSuffix: true })}`;
    } catch {
      return timeStr;
    }
  };

  const formatProgress = (task: TaskStatus) => {
    const total = task.total || 1;
    const percent = Math.max(0, Math.min(100, Math.round((task.current / total) * 100)));
    return `${task.current}/${total} · ${percent}%`;
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

  const taskTypeLabel = (type: string) => {
    switch (type) {
      case 'scan_library':
        return '资源库扫描';
      case 'scan_series':
        return '系列扫描';
      case 'cleanup_library':
        return '资源清理';
      case 'rebuild_index':
        return '索引重建';
      case 'rebuild_thumbnails':
        return '缩略图重建';
      case 'scrape':
        return '元数据刮削';
      case 'ai_grouping':
        return 'AI 分组';
      case 'rebuild_book_hashes':
        return '书籍指纹重建';
      case 'reconcile_koreader_progress':
        return 'KOReader 重关联';
      case 'refresh_koreader_matching':
        return '应用 KOReader 匹配规则';
      default:
        return type;
    }
  };

  const taskActionHint = (task: TaskStatus) => {
    switch (task.type) {
      case 'scan_library':
      case 'scan_series':
        return '建议检查目录可读性、归档结构和扫描格式。';
      case 'rebuild_index':
        return '建议确认数据库和搜索索引目录的写权限。';
      case 'rebuild_thumbnails':
        return '建议检查缓存目录权限和封面源文件是否仍然可用。';
      case 'scrape':
        return '建议检查元数据来源、网络连通性或 LLM 配置。';
      case 'ai_grouping':
        return '建议检查 LLM 配置和系列摘要是否足够完整。';
      case 'rebuild_book_hashes':
        return '建议检查书库文件可读性，以及数据库是否允许写入书籍身份字段。';
      case 'reconcile_koreader_progress':
        return '建议先重建书籍指纹，再重试未匹配的 KOReader 同步记录。';
      case 'refresh_koreader_matching':
        return '会顺序执行匹配索引重建和未匹配记录重关联，适合在切换匹配模式后使用。';
      default:
        return '建议先查看任务错误详情，再决定是否重试。';
    }
  };

  const hasTaskDetails = (task: TaskStatus) =>
    Boolean(task.error || (task.params && Object.keys(task.params).length > 0) || task.started_at || task.finished_at);

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

  const clearTasks = async (status: 'completed' | 'failed') => {
    try {
      const resp = await fetch(`/api/system/tasks?status=${status}`, { method: 'DELETE' });
      if (!resp.ok) {
        throw new Error('任务清理失败');
      }
      await fetchData();
    } catch (err) {
      console.error(err);
      setError(err instanceof Error ? err.message : '任务清理失败');
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
    navigate('/settings');
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

      <div className="grid gap-4 md:grid-cols-3">
        <TaskMetricCard label="运行中任务" value={runningTasks.length} hint="当前仍在后台执行" tone="blue" />
        <TaskMetricCard label="失败任务" value={failedTasks.length} hint="建议优先处理" tone="red" />
        <TaskMetricCard label="已完成任务" value={completedTasks.length} hint="当前筛选结果内" tone="emerald" />
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
            <div className="mb-4 flex flex-wrap gap-2">
              <button
                onClick={() => clearTasks('completed')}
                className="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-slate-300 hover:bg-slate-800"
              >
                清理已完成任务
              </button>
              <button
                onClick={() => clearTasks('failed')}
                className="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-slate-300 hover:bg-slate-800"
              >
                清理失败任务
              </button>
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
                groupedTasks.map((group) => (
                  <div key={group.title} className="space-y-3">
                    <div className="flex items-center justify-between">
                      <h3 className="text-xs font-semibold uppercase tracking-[0.18em] text-slate-500">{group.title}</h3>
                      <span className="text-xs text-slate-600">{group.items.length} 项</span>
                    </div>
                    {group.items.map((task) => (
                      <div key={task.key} className="rounded-xl border border-slate-800 bg-slate-950 p-3">
                        <div className="flex items-center gap-2">
                          <span className={`rounded-full border px-2 py-0.5 text-[11px] font-semibold uppercase ${badgeClass(task.status)}`}>
                            {task.status}
                          </span>
                          <span className="text-xs text-slate-500">{taskTypeLabel(task.type)}</span>
                          <span className="text-xs text-slate-500">
                            {task.scope_name || task.scope}{task.scope_id ? ` #${task.scope_id}` : ''}
                          </span>
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
                          {hasTaskDetails(task) && (
                            <button
                              onClick={() => setExpandedTaskKey((current) => current === task.key ? null : task.key)}
                              className="inline-flex items-center gap-1 rounded-md border border-slate-700 px-2 py-1 text-[11px] text-slate-300 hover:bg-slate-800"
                            >
                              {expandedTaskKey === task.key ? '收起详情' : '查看详情'}
                            </button>
                          )}
                        </div>
                        <p className="mt-2 text-sm text-slate-100">{task.message}</p>
                        <p className="mt-1 text-xs text-slate-500">
                          {formatProgress(task)} · {formatTaskRelativeTime(task.updated_at)} · {formatLogTime(task.updated_at)}
                        </p>
                        {task.error && (
                          <p className="mt-2 rounded-lg border border-red-500/20 bg-red-500/10 px-2 py-2 text-xs text-red-200">{task.error}</p>
                        )}
                        <p className="mt-2 text-xs text-slate-500">{taskActionHint(task)}</p>
                        {expandedTaskKey === task.key && (
                          <div className="mt-3 rounded-xl border border-slate-800 bg-slate-900/60 p-3 space-y-3">
                            <div className="grid gap-2 sm:grid-cols-2">
                              <div>
                                <p className="text-[11px] uppercase tracking-[0.16em] text-slate-500">开始时间</p>
                                <p className="mt-1 text-xs text-slate-300">{formatLogTime(task.started_at)}</p>
                              </div>
                              <div>
                                <p className="text-[11px] uppercase tracking-[0.16em] text-slate-500">结束时间</p>
                                <p className="mt-1 text-xs text-slate-300">{task.finished_at ? formatLogTime(task.finished_at) : '仍在运行中'}</p>
                              </div>
                            </div>
                            {task.params && Object.keys(task.params).length > 0 && (
                              <div>
                                <p className="text-[11px] uppercase tracking-[0.16em] text-slate-500 mb-2">参数快照</p>
                                <div className="flex flex-wrap gap-2">
                                  {Object.entries(task.params).map(([key, value]) => (
                                    <span
                                      key={`${task.key}-${key}`}
                                      className="rounded-full border border-slate-700 bg-slate-950 px-2.5 py-1 text-xs text-slate-300"
                                    >
                                      {key}: {value}
                                    </span>
                                  ))}
                                </div>
                              </div>
                            )}
                            {task.error && (
                              <div>
                                <p className="text-[11px] uppercase tracking-[0.16em] text-slate-500 mb-2">错误详情</p>
                                <pre className="overflow-auto rounded-lg border border-red-500/20 bg-black/30 p-3 text-xs text-red-100 whitespace-pre-wrap break-words">
                                  {task.error}
                                </pre>
                              </div>
                            )}
                          </div>
                        )}
                        <button
                          onClick={() => openTaskTarget(task)}
                          className="mt-3 rounded-md border border-slate-700 px-2.5 py-1.5 text-xs text-slate-300 hover:bg-slate-800"
                        >
                          打开关联页面
                        </button>
                      </div>
                    ))}
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

function TaskMetricCard({
  label,
  value,
  hint,
  tone,
}: {
  label: string;
  value: number;
  hint: string;
  tone: 'blue' | 'red' | 'emerald';
}) {
  const toneClass = {
    blue: 'border-blue-500/20 bg-blue-500/10 text-blue-200',
    red: 'border-red-500/20 bg-red-500/10 text-red-200',
    emerald: 'border-emerald-500/20 bg-emerald-500/10 text-emerald-200',
  }[tone];

  return (
    <div className={`rounded-2xl border p-4 ${toneClass}`}>
      <p className="text-sm opacity-80">{label}</p>
      <p className="mt-2 text-2xl font-semibold text-white">{value}</p>
      <p className="mt-1 text-xs opacity-80">{hint}</p>
    </div>
  );
}
