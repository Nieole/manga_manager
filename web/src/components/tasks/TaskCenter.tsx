import { Activity, Pause, Play, RefreshCw, RotateCcw, XCircle } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';

export interface TaskLimits {
  scan_profile?: string;
  scanner_workers_configured?: number;
  scanner_workers_effective?: number;
  storage_profile?: string;
  volume_key?: string;
  scan_concurrency?: number;
  archive_open_concurrency?: number;
  cover_concurrency?: number;
  hash_concurrency?: number;
  pause_background_when_reading?: boolean;
  idle_only_heavy_tasks?: boolean;
  disable_same_disk_page_cache?: boolean;
}

export interface TaskStatus {
  key: string;
  type: string;
  scope: string;
  scope_id?: number;
  scope_name?: string;
  status: string;
  message: string;
  current: number;
  total: number;
  percent?: number;
  rate_per_minute?: number;
  eta_seconds?: number;
  can_cancel: boolean;
  can_pause: boolean;
  can_resume: boolean;
  retryable: boolean;
  pause_reason?: string;
  phase?: string;
  current_item?: string;
  effective_limit?: TaskLimits;
  metrics?: Record<string, number>;
  labels?: Record<string, string>;
  params?: Record<string, string>;
}

export type TaskAction = 'pause' | 'resume' | 'cancel' | 'retry';

interface TaskCenterProps {
  tasks: TaskStatus[];
  loading: boolean;
  backgroundPaused?: boolean;
  taskActionKey: string | null;
  onRefresh: () => void;
  onTaskAction: (task: TaskStatus, action: TaskAction) => void;
}

const activeStatuses = ['running', 'paused', 'cancelling'];

const taskMetricKeys = [
  'processed_archives',
  'opened_archives',
  'failed_archives',
  'hashed_files',
  'queued_covers',
  'generated_covers',
  'processed_books',
  'processed_progress',
  'scanned_files',
  'transferred_files',
  'success_count',
  'failed_count',
  'not_found_count',
  'queued_review_count',
  'provider_requests',
  'provider_errors',
  'rate_limited_wait_ms',
  'paused_ms',
  'io_wait_ms',
  'thumbnail_write_ms',
];

function formatRate(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0/min';
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)}/min`;
}

function formatDuration(seconds?: number) {
  if (!Number.isFinite(seconds || 0) || !seconds || seconds <= 0) return '-';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  const rest = Math.round(seconds % 60);
  if (minutes < 60) return `${minutes}m ${rest}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

function taskBadgeClass(status: string) {
  switch (status) {
    case 'running':
      return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-500';
    case 'paused':
      return 'border-amber-500/30 bg-amber-500/10 text-amber-500';
    case 'cancelling':
      return 'border-orange-500/30 bg-orange-500/10 text-orange-200';
    case 'failed':
      return 'border-red-500/30 bg-red-500/10 text-red-200';
    case 'completed':
      return 'border-sky-500/30 bg-sky-500/10 text-sky-200';
    default:
      return 'border-white/10 bg-white/[0.04] text-white/60';
  }
}

function taskProgressPercent(task: TaskStatus) {
  if (Number.isFinite(task.percent)) return Math.max(0, Math.min(100, task.percent || 0));
  if (task.total > 0) return Math.max(0, Math.min(100, (task.current / task.total) * 100));
  return 0;
}

function taskMetric(task: TaskStatus, key: string) {
  const direct = task.metrics?.[key];
  if (Number.isFinite(direct)) return direct || 0;
  const raw = task.params?.[key] || task.params?.[`metric.${key}`];
  const parsed = raw ? Number(raw) : 0;
  return Number.isFinite(parsed) ? parsed : 0;
}

function TaskSummaryStrip({ tasks, backgroundPaused }: { tasks: TaskStatus[]; backgroundPaused?: boolean }) {
  const { t } = useI18n();
  const activeTasks = tasks.filter((task) => activeStatuses.includes(task.status));
  const failedTasks = tasks.filter((task) => task.status === 'failed');
  const pausedTasks = tasks.filter((task) => task.status === 'paused');
  const items = [
    [t('settings.maintenance.activeTasks'), activeTasks.length],
    [t('settings.maintenance.pausedTasks'), pausedTasks.length],
    [t('settings.maintenance.failedTasks'), failedTasks.length],
  ] as const;

  return (
    <div className="grid gap-3 md:grid-cols-4">
      {items.map(([label, value]) => (
        <div key={label} className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
          <p className="text-xs uppercase tracking-wide text-white/40">{label}</p>
          <p className="mt-2 text-2xl font-semibold text-white">{value}</p>
        </div>
      ))}
      <div className="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
        <p className="text-xs uppercase tracking-wide text-white/40">{t('settings.maintenance.backgroundIO')}</p>
        <p className={`mt-2 text-sm font-semibold ${backgroundPaused ? 'text-amber-500' : 'text-emerald-500'}`}>
          {backgroundPaused ? t('settings.maintenance.backgroundPaused') : t('settings.maintenance.backgroundRunning')}
        </p>
      </div>
    </div>
  );
}

function TaskProgressBar({ task }: { task: TaskStatus }) {
  const percent = taskProgressPercent(task);
  if (task.total <= 0) {
    return (
      <div className="mt-4 h-2 overflow-hidden rounded-full bg-white/10">
        <div className="h-full w-1/3 animate-pulse rounded-full bg-komgaPrimary/80" />
      </div>
    );
  }

  return (
    <div className="mt-4">
      <div className="h-2 overflow-hidden rounded-full bg-white/10">
        <div className="h-full rounded-full bg-komgaPrimary transition-all" style={{ width: `${percent}%` }} />
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-white/45">
        <span>{task.current} / {task.total}</span>
        <span>{percent.toFixed(1)}%</span>
        <span>{formatRate(task.rate_per_minute || 0)}</span>
        <span>ETA {formatDuration(task.eta_seconds)}</span>
      </div>
    </div>
  );
}

function TaskActionButtons({ task, taskActionKey, onTaskAction }: { task: TaskStatus; taskActionKey: string | null; onTaskAction: (task: TaskStatus, action: TaskAction) => void }) {
  const { t } = useI18n();
  return (
    <div className="flex flex-wrap gap-2">
      {task.can_pause && task.status === 'running' && (
        <button type="button" onClick={() => onTaskAction(task, 'pause')} disabled={taskActionKey === `${task.key}:pause`} className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 px-3 py-2 text-xs text-amber-500 hover:bg-amber-500/10 disabled:opacity-50">
          <Pause className="h-3.5 w-3.5" />
          {t('settings.maintenance.pauseTask')}
        </button>
      )}
      {task.can_resume && task.status === 'paused' && (
        <button type="button" onClick={() => onTaskAction(task, 'resume')} disabled={taskActionKey === `${task.key}:resume`} className="inline-flex items-center gap-1.5 rounded-lg border border-emerald-500/30 px-3 py-2 text-xs text-emerald-500 hover:bg-emerald-500/10 disabled:opacity-50">
          <Play className="h-3.5 w-3.5" />
          {t('settings.maintenance.resumeTask')}
        </button>
      )}
      {task.can_cancel && activeStatuses.includes(task.status) && (
        <button type="button" onClick={() => onTaskAction(task, 'cancel')} disabled={taskActionKey === `${task.key}:cancel` || task.status === 'cancelling'} className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/30 px-3 py-2 text-xs text-red-200 hover:bg-red-500/10 disabled:opacity-50">
          <XCircle className="h-3.5 w-3.5" />
          {t('common.cancel')}
        </button>
      )}
      {task.retryable && !activeStatuses.includes(task.status) && (
        <button type="button" onClick={() => onTaskAction(task, 'retry')} disabled={taskActionKey === `${task.key}:retry`} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/70 hover:bg-white/10 disabled:opacity-50">
          <RotateCcw className={`h-3.5 w-3.5 ${taskActionKey === `${task.key}:retry` ? 'animate-spin' : ''}`} />
          {t('common.retry')}
        </button>
      )}
    </div>
  );
}

function TaskLimitBadges({ task }: { task: TaskStatus }) {
  const { t } = useI18n();
  const limit = task.effective_limit;
  const provider = task.labels?.provider_name || task.labels?.provider || task.params?.provider;
  return (
    <>
      {limit && (
        <>
          <p className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-white/55">
            {t('settings.maintenance.effectiveWorkers')}<span className="mt-1 block text-white">{limit.scanner_workers_effective || '-'}</span>
          </p>
          <p className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-white/55">
            {t('settings.maintenance.archiveOpenConcurrency')}<span className="mt-1 block text-white">{limit.archive_open_concurrency || '-'}</span>
          </p>
          <p className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-white/55">
            {t('settings.maintenance.storageProfile')}<span className="mt-1 block text-white">{limit.storage_profile || '-'}</span>
          </p>
          <p className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-white/55">
            {t('settings.maintenance.volume')}<span className="mt-1 block text-white">{limit.volume_key || '-'}</span>
          </p>
        </>
      )}
      {provider && (
        <p className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-white/55">
          Provider<span className="mt-1 block text-white">{provider}</span>
        </p>
      )}
    </>
  );
}

function TaskMetricsGrid({ task }: { task: TaskStatus }) {
  const { t } = useI18n();
  return (
    <>
      {taskMetricKeys.map((key) => (
        taskMetric(task, key) > 0 && (
          <p key={key} className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-white/55">
            {t(`settings.maintenance.taskMetric.${key}`)}<span className="mt-1 block text-white">{key.endsWith('_ms') ? `${taskMetric(task, key)} ms` : taskMetric(task, key)}</span>
          </p>
        )
      ))}
    </>
  );
}

function TaskDetailDrawer({ task }: { task: TaskStatus }) {
  return (
    <div className="mt-3 grid gap-2 text-xs md:grid-cols-2 xl:grid-cols-4">
      <TaskLimitBadges task={task} />
      <TaskMetricsGrid task={task} />
    </div>
  );
}

function TaskCard({ task, taskActionKey, onTaskAction }: { task: TaskStatus; taskActionKey: string | null; onTaskAction: (task: TaskStatus, action: TaskAction) => void }) {
  const { t } = useI18n();
  const statusLabel = t(`logs.taskStatus.${task.status}`);

  return (
    <div className="rounded-xl border border-white/10 bg-gray-950/50 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className={`rounded-full border px-2.5 py-1 text-xs ${taskBadgeClass(task.status)}`}>{statusLabel}</span>
            <p className="text-sm font-semibold text-white">{t(`settings.maintenance.taskType.${task.type}`)}</p>
            <span className="text-xs text-white/40">{task.scope_name || task.scope}{task.scope_id ? ` #${task.scope_id}` : ''}</span>
          </div>
          <p className="mt-2 truncate text-sm text-white/70" title={task.current_item || task.message}>{task.message}</p>
          <p className="mt-1 truncate text-xs text-white/35" title={task.current_item || undefined}>
            {task.phase ? t(`settings.maintenance.taskPhase.${task.phase}`) : '-'}{task.current_item ? ` · ${task.current_item}` : ''}
          </p>
        </div>
        <TaskActionButtons task={task} taskActionKey={taskActionKey} onTaskAction={onTaskAction} />
      </div>
      <TaskProgressBar task={task} />
      <TaskDetailDrawer task={task} />
    </div>
  );
}

function TaskList({ tasks, taskActionKey, onTaskAction }: { tasks: TaskStatus[]; taskActionKey: string | null; onTaskAction: (task: TaskStatus, action: TaskAction) => void }) {
  const { t } = useI18n();
  if (tasks.length === 0) {
    return <p className="rounded-xl border border-white/10 bg-white/[0.03] p-4 text-sm text-white/50">{t('settings.maintenance.noTasks')}</p>;
  }

  return (
    <>
      {tasks.slice(0, 12).map((task) => (
        <TaskCard key={task.key} task={task} taskActionKey={taskActionKey} onTaskAction={onTaskAction} />
      ))}
    </>
  );
}

export function TaskCenter({ tasks, loading, backgroundPaused, taskActionKey, onRefresh, onTaskAction }: TaskCenterProps) {
  const { t } = useI18n();
  return (
    <section className="rounded-xl border border-white/10 bg-gray-900/70 p-5 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2 text-komgaPrimary">
          <Activity className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.maintenance.taskCenterTitle')}</h3>
        </div>
        <button type="button" onClick={onRefresh} disabled={loading} className="inline-flex items-center gap-2 rounded-lg border border-white/10 px-3 py-2 text-sm text-white/70 hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-50">
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          {t('settings.maintenance.refreshTasks')}
        </button>
      </div>

      <TaskSummaryStrip tasks={tasks} backgroundPaused={backgroundPaused} />

      <div className="space-y-3">
        <TaskList tasks={tasks} taskActionKey={taskActionKey} onTaskAction={onTaskAction} />
      </div>
    </section>
  );
}
