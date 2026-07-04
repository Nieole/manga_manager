/**
 * 业务说明：本文件是业务实现，属于前端共享组件层，负责沉淀按钮、面板、列表、封面、进度和反馈等可复用 UI 片段。
 * 它让资料库、阅读器、设置和系列详情在视觉和交互上保持一致。
 * 维护时应关注组件职责边界、可访问性、主题变量、加载态和不同页面的复用语义。
 */

import { useMemo, useState } from 'react';
import { Activity, ChevronDown, ExternalLink, FileText, Pause, Play, RefreshCw, RotateCcw, Search, Trash2, XCircle } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { getTaskActionHint, getTaskMessage, getTaskTypeLabel } from '../../i18n/task';

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
  message_code?: string;
  message_params?: Record<string, string>;
  error?: string;
  current: number;
  total: number;
  percent?: number;
  rate_per_minute?: number;
  eta_seconds?: number;
  can_cancel: boolean;
  can_pause?: boolean;
  can_resume?: boolean;
  retryable: boolean;
  pause_reason?: string;
  phase?: string;
  current_item?: string;
  effective_limit?: TaskLimits;
  metrics?: Record<string, number>;
  labels?: Record<string, string>;
  params?: Record<string, string>;
  started_at?: string;
  updated_at?: string;
  finished_at?: string;
}

export type TaskAction = 'pause' | 'resume' | 'cancel' | 'retry';

export interface TaskCenterFilters {
  status: string;
  scope: string;
  type: string;
  scopeId: string;
  query: string;
}

interface TaskCenterProps {
  tasks: TaskStatus[];
  loading: boolean;
  backgroundPaused?: boolean;
  taskActionKey: string | null;
  filters?: TaskCenterFilters;
  typeOptions?: string[];
  currentFilterCanClear?: boolean;
  onRefresh: () => void;
  onTaskAction: (task: TaskStatus, action: TaskAction) => void;
  onFilterChange?: (patch: Partial<TaskCenterFilters>) => void;
  onClearTasks?: (status?: 'completed' | 'failed', useCurrentFilters?: boolean) => void;
  onOpenTaskTarget?: (task: TaskStatus) => void;
  onViewTaskLogs?: (task: TaskStatus) => void;
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
  'duration_ms',
];

const taskIOParamKeys = [
  'storage_profile',
  'volume_key',
  'opened_archives',
  'hashed_files',
  'io_wait_ms',
  'paused_ms',
  'thumbnail_write_ms',
  'duration_ms',
  'pause_reason',
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
    case 'interrupted':
      return 'border-amber-500/30 bg-amber-500/10 text-amber-500';
    case 'completed':
      return 'border-sky-500/30 bg-sky-500/10 text-sky-200';
    case 'cancelled':
      return 'border-white/10 bg-white/4 text-white/45';
    default:
      return 'border-white/10 bg-white/4 text-white/60';
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

function taskIOParams(task: TaskStatus) {
  return Object.entries(task.params || {}).filter(([key, value]) => taskIOParamKeys.includes(key) && value !== '' && value !== '0');
}

function isInterruptedTask(task: TaskStatus) {
  const error = task.error || '';
  return task.status === 'interrupted' || (task.status === 'failed' && task.retryable && (error.includes('服务重启') || error.toLowerCase().includes('restart')));
}

function hasTaskDetails(task: TaskStatus) {
  return Boolean(
    task.error
    || task.started_at
    || task.finished_at
    || (task.params && Object.keys(task.params).length > 0)
    || (task.labels && Object.keys(task.labels).length > 0),
  );
}

function hasInlineTelemetry(task: TaskStatus) {
  const provider = task.labels?.provider_name || task.labels?.provider || task.params?.provider;
  return Boolean(
    task.effective_limit
    || provider
    || taskMetricKeys.some((key) => taskMetric(task, key) > 0),
  );
}

function TaskSummaryStrip({ tasks, backgroundPaused }: { tasks: TaskStatus[]; backgroundPaused?: boolean }) {
  const { t } = useI18n();
  const items = [
    [t('settings.maintenance.activeTasks'), tasks.filter((task) => activeStatuses.includes(task.status)).length],
    [t('settings.maintenance.pausedTasks'), tasks.filter((task) => task.status === 'paused').length],
    [t('settings.maintenance.failedTasks'), tasks.filter((task) => task.status === 'failed').length],
    [t('logs.metric.completedTasks'), tasks.filter((task) => task.status === 'completed').length],
    [t('logs.metric.interruptedTasks'), tasks.filter(isInterruptedTask).length],
  ] as const;

  return (
    <div className={`grid gap-3 ${backgroundPaused === undefined ? 'md:grid-cols-5' : 'md:grid-cols-3 xl:grid-cols-6'}`}>
      {items.map(([label, value]) => (
        <div key={label} className="rounded-xl border border-white/10 bg-white/3 px-4 py-3">
          <p className="text-xs uppercase tracking-wide text-white/40">{label}</p>
          <p className="mt-2 text-2xl font-semibold text-white">{value}</p>
        </div>
      ))}
      {backgroundPaused !== undefined && (
        <div className="rounded-xl border border-white/10 bg-white/3 px-4 py-3">
          <p className="text-xs uppercase tracking-wide text-white/40">{t('settings.maintenance.backgroundIO')}</p>
          <p className={`mt-2 text-sm font-semibold ${backgroundPaused ? 'text-amber-500' : 'text-emerald-500'}`}>
            {backgroundPaused ? t('settings.maintenance.backgroundPaused') : t('settings.maintenance.backgroundRunning')}
          </p>
        </div>
      )}
    </div>
  );
}

function TaskFilters({
  filters,
  typeOptions,
  currentFilterCanClear,
  onFilterChange,
  onClearTasks,
  onRefresh,
}: {
  filters: TaskCenterFilters;
  typeOptions: string[];
  currentFilterCanClear?: boolean;
  onFilterChange: (patch: Partial<TaskCenterFilters>) => void;
  onClearTasks?: (status?: 'completed' | 'failed', useCurrentFilters?: boolean) => void;
  onRefresh: () => void;
}) {
  const { t } = useI18n();

  return (
    <div className="space-y-3 rounded-xl border border-white/10 bg-gray-950/40 p-3">
      <div className="flex flex-wrap gap-2">
        {onClearTasks && (
          <>
            <button type="button" onClick={() => onClearTasks('completed')} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/60 hover:bg-white/10 hover:text-white">
              <Trash2 className="h-3.5 w-3.5" />
              {t('logs.clearCompleted')}
            </button>
            <button type="button" onClick={() => onClearTasks('failed')} className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/20 px-3 py-2 text-xs text-red-200 hover:bg-red-500/10">
              <Trash2 className="h-3.5 w-3.5" />
              {t('logs.clearFailed')}
            </button>
            <button
              type="button"
              onClick={() => onClearTasks(undefined, true)}
              disabled={!currentFilterCanClear}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/60 hover:bg-white/10 hover:text-white disabled:cursor-not-allowed disabled:opacity-50"
              title={currentFilterCanClear ? t('logs.clearCurrentFilterHint') : t('logs.clearCurrentFilterDisabled')}
            >
              <Trash2 className="h-3.5 w-3.5" />
              {t('logs.clearCurrentFilter')}
            </button>
          </>
        )}
      </div>

      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-5">
        <select value={filters.status} onChange={(event) => onFilterChange({ status: event.target.value })} className="rounded-lg border border-white/10 bg-gray-950 px-3 py-2 text-xs text-white">
          <option value="ALL">{t('logs.taskStatus.all')}</option>
          <option value="running">{t('logs.taskStatus.running')}</option>
          <option value="paused">{t('logs.taskStatus.paused')}</option>
          <option value="cancelling">{t('logs.taskStatus.cancelling')}</option>
          <option value="failed">{t('logs.taskStatus.failed')}</option>
          <option value="interrupted">{t('logs.taskStatus.interrupted')}</option>
          <option value="completed">{t('logs.taskStatus.completed')}</option>
          <option value="cancelled">{t('logs.taskStatus.cancelled')}</option>
        </select>
        <select value={filters.scope} onChange={(event) => onFilterChange({ scope: event.target.value })} className="rounded-lg border border-white/10 bg-gray-950 px-3 py-2 text-xs text-white">
          <option value="ALL">{t('logs.taskScope.all')}</option>
          <option value="system">{t('logs.taskScope.system')}</option>
          <option value="library">{t('logs.taskScope.library')}</option>
          <option value="series">{t('logs.taskScope.series')}</option>
        </select>
        <select value={filters.type} onChange={(event) => onFilterChange({ type: event.target.value })} className="rounded-lg border border-white/10 bg-gray-950 px-3 py-2 text-xs text-white">
          <option value="ALL">{t('logs.taskType.all')}</option>
          {typeOptions.map((type) => (
            <option key={type} value={type}>{getTaskTypeLabel({ type, params: {} }, t)}</option>
          ))}
        </select>
        <input
          value={filters.scopeId}
          onChange={(event) => onFilterChange({ scopeId: event.target.value.replace(/[^\d]/g, '') })}
          onKeyDown={(event) => event.key === 'Enter' && onRefresh()}
          inputMode="numeric"
          placeholder={t('logs.taskScopeIdPlaceholder')}
          className="rounded-lg border border-white/10 bg-gray-950 px-3 py-2 text-xs text-white"
        />
        <div className="flex gap-2">
          <div className="relative min-w-0 flex-1">
            <Search className="absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-white/35" />
            <input
              value={filters.query}
              onChange={(event) => onFilterChange({ query: event.target.value })}
              onKeyDown={(event) => event.key === 'Enter' && onRefresh()}
              placeholder={t('logs.taskSearchPlaceholder')}
              className="w-full rounded-lg border border-white/10 bg-gray-950 py-2 pl-9 pr-3 text-xs text-white"
            />
          </div>
          <button type="button" onClick={onRefresh} className="rounded-lg border border-white/10 bg-gray-950 px-3 py-2 text-xs text-white/60 hover:bg-white/10 hover:text-white">
            {t('logs.query')}
          </button>
        </div>
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
          <p className="rounded-lg border border-white/10 bg-white/3 px-3 py-2 text-white/55">
            {t('settings.maintenance.effectiveWorkers')}<span className="mt-1 block text-white">{limit.scanner_workers_effective || '-'}</span>
          </p>
          <p className="rounded-lg border border-white/10 bg-white/3 px-3 py-2 text-white/55">
            {t('settings.maintenance.archiveOpenConcurrency')}<span className="mt-1 block text-white">{limit.archive_open_concurrency || '-'}</span>
          </p>
          <p className="rounded-lg border border-white/10 bg-white/3 px-3 py-2 text-white/55">
            {t('settings.maintenance.storageProfile')}<span className="mt-1 block text-white">{limit.storage_profile || '-'}</span>
          </p>
          <p className="rounded-lg border border-white/10 bg-white/3 px-3 py-2 text-white/55">
            {t('settings.maintenance.volume')}<span className="mt-1 block text-white">{limit.volume_key || '-'}</span>
          </p>
        </>
      )}
      {provider && (
        <p className="rounded-lg border border-white/10 bg-white/3 px-3 py-2 text-white/55">
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
          <p key={key} className="rounded-lg border border-white/10 bg-white/3 px-3 py-2 text-white/55">
            {t(`settings.maintenance.taskMetric.${key}`)}<span className="mt-1 block text-white">{key.endsWith('_ms') ? `${taskMetric(task, key)} ms` : taskMetric(task, key)}</span>
          </p>
        )
      ))}
    </>
  );
}

function TaskDetailDrawer({ task }: { task: TaskStatus }) {
  const { t, formatDateTime } = useI18n();
  const ioParams = taskIOParams(task);

  return (
    <div className="mt-3 space-y-3">
      {ioParams.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {ioParams.map(([key, value]) => (
            <span key={`${task.key}-io-${key}`} className="rounded-md border border-komgaPrimary/20 bg-komgaPrimary/10 px-2 py-1 text-[11px] text-komgaPrimary">
              {t(`logs.task.io.${key}`)}: {key === 'pause_reason' ? t(`logs.task.pauseReason.${value}`) : value}
            </span>
          ))}
        </div>
      )}
      {(task.started_at || task.finished_at) && (
        <div className="grid gap-2 text-xs sm:grid-cols-2">
          <div className="rounded-lg border border-white/10 bg-white/3 px-3 py-2">
            <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.startedAt')}</p>
            <p className="mt-1 text-white/65">{task.started_at ? formatDateTime(task.started_at) : '-'}</p>
          </div>
          <div className="rounded-lg border border-white/10 bg-white/3 px-3 py-2">
            <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.finishedAt')}</p>
            <p className="mt-1 text-white/65">{task.finished_at ? formatDateTime(task.finished_at) : t('logs.task.runningNow')}</p>
          </div>
        </div>
      )}
      {task.params && Object.keys(task.params).length > 0 && (
        <div>
          <p className="mb-2 text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.params')}</p>
          <div className="flex flex-wrap gap-2">
            {Object.entries(task.params).map(([key, value]) => (
              <span key={`${task.key}-${key}`} className="rounded-full border border-white/10 bg-gray-950 px-2.5 py-1 text-xs text-white/45">
                {key}: {value}
              </span>
            ))}
          </div>
        </div>
      )}
      {task.labels && Object.keys(task.labels).length > 0 && (
        <div>
          <p className="mb-2 text-[11px] uppercase tracking-[0.16em] text-white/35">{t('settings.maintenance.taskLabels')}</p>
          <div className="flex flex-wrap gap-2">
            {Object.entries(task.labels).map(([key, value]) => (
              <span key={`${task.key}-label-${key}`} className="rounded-full border border-white/10 bg-gray-950 px-2.5 py-1 text-xs text-white/45">
                {key}: {value}
              </span>
            ))}
          </div>
        </div>
      )}
      {task.error && (
        <div>
          <p className="mb-2 text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.errorDetails')}</p>
          <pre className="overflow-auto rounded-lg border border-red-500/20 bg-black/30 p-3 text-xs whitespace-pre-wrap wrap-break-word text-red-400">{task.error}</pre>
        </div>
      )}
    </div>
  );
}

function TaskInlineTelemetry({ task }: { task: TaskStatus }) {
  return (
    <div className="mt-3 grid gap-2 text-xs md:grid-cols-2 xl:grid-cols-4">
      <TaskLimitBadges task={task} />
      <TaskMetricsGrid task={task} />
    </div>
  );
}

function TaskCard({
  task,
  expanded,
  taskActionKey,
  onToggleExpanded,
  onTaskAction,
  onOpenTaskTarget,
  onViewTaskLogs,
}: {
  task: TaskStatus;
  expanded: boolean;
  taskActionKey: string | null;
  onToggleExpanded: () => void;
  onTaskAction: (task: TaskStatus, action: TaskAction) => void;
  onOpenTaskTarget?: (task: TaskStatus) => void;
  onViewTaskLogs?: (task: TaskStatus) => void;
}) {
  const { t, formatDateTime, formatRelativeTime } = useI18n();
  const statusLabel = t(`logs.taskStatus.${task.status}`);
  const updatedAt = task.updated_at ? `${formatRelativeTime(task.updated_at)} - ${formatDateTime(task.updated_at)}` : '';

  return (
    <div className="rounded-xl border border-white/10 bg-gray-950/50 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className={`rounded-full border px-2.5 py-1 text-xs ${taskBadgeClass(task.status)}`}>{statusLabel}</span>
            <p className="text-sm font-semibold text-white">{getTaskTypeLabel(task, t)}</p>
            <span className="text-xs text-white/40">{task.scope_name || t(`task.scope.${task.scope}`, undefined, task.scope)}{task.scope_id ? ` #${task.scope_id}` : ''}</span>
          </div>
          <p className="mt-2 truncate text-sm text-white/70" title={task.current_item || getTaskMessage(task, t)}>{getTaskMessage(task, t)}</p>
          <p className="mt-1 truncate text-xs text-white/35" title={task.current_item || undefined}>
            {task.phase ? t(`settings.maintenance.taskPhase.${task.phase}`) : '-'}{task.current_item ? ` - ${task.current_item}` : ''}
          </p>
          {updatedAt && <p className="mt-1 text-xs text-white/35">{updatedAt}</p>}
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <TaskActionButtons task={task} taskActionKey={taskActionKey} onTaskAction={onTaskAction} />
          {onOpenTaskTarget && (
            <button type="button" onClick={() => onOpenTaskTarget(task)} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/60 hover:bg-white/10 hover:text-white">
              <ExternalLink className="h-3.5 w-3.5" />
              {t('logs.task.openPage')}
            </button>
          )}
          {onViewTaskLogs && (
            <button type="button" onClick={() => onViewTaskLogs(task)} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/60 hover:bg-white/10 hover:text-white">
              <FileText className="h-3.5 w-3.5" />
              {t('logs.task.viewLogs')}
            </button>
          )}
          {hasTaskDetails(task) && (
            <button type="button" onClick={onToggleExpanded} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/60 hover:bg-white/10 hover:text-white">
              <ChevronDown className={`h-3.5 w-3.5 transition-transform ${expanded ? 'rotate-180' : ''}`} />
              {expanded ? t('common.collapseDetails') : t('common.viewDetails')}
            </button>
          )}
        </div>
      </div>
      <TaskProgressBar task={task} />
      {hasInlineTelemetry(task) && <TaskInlineTelemetry task={task} />}
      {isInterruptedTask(task) && (
        <p className="mt-3 rounded-lg border border-amber-500/20 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">{t('logs.task.interruptedHint')}</p>
      )}
      {task.error && !expanded && (
        <p className="mt-3 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-400">{task.error}</p>
      )}
      <p className="mt-3 text-xs text-white/35">{getTaskActionHint(task, t)}</p>
      {expanded && (
        <div className="mt-3 rounded-xl border border-white/10 bg-black/20 p-3">
          <TaskDetailDrawer task={task} />
        </div>
      )}
    </div>
  );
}

function TaskList({
  tasks,
  taskActionKey,
  onTaskAction,
  onOpenTaskTarget,
  onViewTaskLogs,
}: {
  tasks: TaskStatus[];
  taskActionKey: string | null;
  onTaskAction: (task: TaskStatus, action: TaskAction) => void;
  onOpenTaskTarget?: (task: TaskStatus) => void;
  onViewTaskLogs?: (task: TaskStatus) => void;
}) {
  const { t } = useI18n();
  const [expandedTaskKey, setExpandedTaskKey] = useState<string | null>(null);

  if (tasks.length === 0) {
    return <p className="rounded-xl border border-white/10 bg-white/3 p-4 text-sm text-white/50">{t('settings.maintenance.noTasks')}</p>;
  }

  return (
    <>
      {tasks.map((task) => (
        <TaskCard
          key={task.key}
          task={task}
          expanded={expandedTaskKey === task.key}
          taskActionKey={taskActionKey}
          onToggleExpanded={() => setExpandedTaskKey((current) => (current === task.key ? null : task.key))}
          onTaskAction={onTaskAction}
          onOpenTaskTarget={onOpenTaskTarget}
          onViewTaskLogs={onViewTaskLogs}
        />
      ))}
    </>
  );
}

export function TaskCenter({
  tasks,
  loading,
  backgroundPaused,
  taskActionKey,
  filters,
  typeOptions = [],
  currentFilterCanClear,
  onRefresh,
  onTaskAction,
  onFilterChange,
  onClearTasks,
  onOpenTaskTarget,
  onViewTaskLogs,
}: TaskCenterProps) {
  const { t } = useI18n();
  const visibleTasks = useMemo(() => tasks.slice(0, 50), [tasks]);

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

      {filters && onFilterChange && (
        <TaskFilters
          filters={filters}
          typeOptions={typeOptions}
          currentFilterCanClear={currentFilterCanClear}
          onFilterChange={onFilterChange}
          onClearTasks={onClearTasks}
          onRefresh={onRefresh}
        />
      )}

      <div className="space-y-3">
        <TaskList tasks={visibleTasks} taskActionKey={taskActionKey} onTaskAction={onTaskAction} onOpenTaskTarget={onOpenTaskTarget} onViewTaskLogs={onViewTaskLogs} />
      </div>
    </section>
  );
}
