/**
 * 任务中心的两层结构：上半**实况区**是仍会变化的那些**运行**，常驻置顶、不受筛选影响；
 * 下半**任务清单**一个**任务**一行，写着上次结果，展开才按需取它的历次运行。
 * 本组件不取数——两层各自的数据由页面交进来，展开哪一行也由页面决定（它要据此发请求）。
 */

import { useState } from 'react';
import { Activity, ChevronDown, ExternalLink, FileText, ListTree, Pause, PauseCircle, Play, RefreshCw, RotateCcw, Search, Trash2, XCircle } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { getTaskActionHint, getTaskMessage, getTaskTypeLabel } from '../../i18n/task';
import { isActiveRunStatus } from '../../utils/runStatus';

// TaskLimits / RunStatus / RunLive / TaskSummary 由 cmd/tsgen 从 Go 后端响应结构体生成
// （单一事实源，见 api/generated.ts），此处再导出以保持既有 import 路径不变。
export type { TaskLimits, RunStatus, RunLive, TaskSummary } from '../../api/generated';
import type { RunLive, RunStatus, TaskSummary } from '../../api/generated';

// 运行上的动作作用在**运行**上，重试作用在**任务**上（它重新发起一次，不改动被重试的那一条）。
export type TaskAction = 'pause' | 'resume' | 'cancel' | 'retry';

export interface TaskCenterFilters {
  status: string;
  scope: string;
  type: string;
  scopeId: string;
  query: string;
}

/** TaskTarget 是「打开这个任务对着的那个页面」要的最小形状：作用域与它的 id。 */
export interface TaskTarget {
  scope: string;
  scope_id?: number;
}

/**
 * TaskRunHistory 是展开着的那一行与它的**历次运行**：三样总是一起出现、一起消失，
 * 因此收成一个类型而不是三个各自可空的 prop——只给其中两个不是一种有意义的状态。
 */
export interface TaskRunHistory {
  taskId: number;
  /** runs 为 undefined 表示还没取回来；空数组表示这个任务确实没留下运行记录。 */
  runs?: RunStatus[];
  loading?: boolean;
}

interface TaskCenterProps {
  // live 是实况区那一帧：在跑的与**排队中**的运行，加上槽位占用与全局暂停。
  live: RunLive;
  // tasks 是任务清单，一个任务一行；历次运行不在里面，由展开的那一行经 history 单独交进来。
  tasks: TaskSummary[];
  loading: boolean;
  taskActionKey: string | null;
  // history 是展开着的那一行与它的历次运行；不给即一行都没展开。
  history?: TaskRunHistory;
  bulkPauseBusy?: boolean;
  filters?: TaskCenterFilters;
  typeOptions?: string[];
  currentFilterCanClear?: boolean;
  onRefresh: () => void;
  onTaskAction: (run: RunStatus, action: TaskAction) => void;
  onToggleTask?: (taskId: number) => void;
  // 全部暂停 / 全部恢复。两者都给了才画那个按钮。
  onPauseAll?: () => void;
  onResumeAll?: () => void;
  onFilterChange?: (patch: Partial<TaskCenterFilters>) => void;
  onClearTasks?: (status?: 'completed' | 'failed', useCurrentFilters?: boolean) => void;
  onOpenTaskTarget?: (target: TaskTarget) => void;
  onViewTaskLogs?: (run: RunStatus) => void;
}

const taskMetricKeys = [
  'processed_archives',
  'opened_archives',
  'failed_archives',
  'rehomed_books',
  'stale_series_stats',
  'format_filtered_archives',
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

function runBadgeClass(status: string) {
  switch (status) {
    case 'running':
      return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-500';
    case 'queued':
      return 'border-sky-500/30 bg-sky-500/10 text-sky-300';
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

function runProgressPercent(run: RunStatus) {
  if (Number.isFinite(run.percent)) return Math.max(0, Math.min(100, run.percent || 0));
  if (run.total > 0) return Math.max(0, Math.min(100, (run.current / run.total) * 100));
  return 0;
}

function runMetric(run: RunStatus, key: string) {
  const direct = run.metrics?.[key];
  if (Number.isFinite(direct)) return direct || 0;
  const raw = run.params?.[key] || run.params?.[`metric.${key}`];
  const parsed = raw ? Number(raw) : 0;
  return Number.isFinite(parsed) ? parsed : 0;
}

function runIOParams(run: RunStatus) {
  return Object.entries(run.params || {}).filter(([key, value]) => taskIOParamKeys.includes(key) && value !== '' && value !== '0');
}

function isInterruptedRun(run: RunStatus) {
  const error = run.error || '';
  return run.status === 'interrupted' || (run.status === 'failed' && run.retryable && (error.includes('服务重启') || error.toLowerCase().includes('restart')));
}

/**
 * runTimestamp 取这条运行「最后一次有动静」的时刻：仍在动的读最后一次上报，停了的读收尾时刻。
 *
 * 两者只在**中断**上不同——那一笔批量转写把这一行的最后写入时刻记成了重启时刻，
 * 照它显示的话，一条八小时前就没动静的中断运行在重启后写着「刚刚」。
 */
function runTimestamp(run: RunStatus) {
  if (!isActiveRunStatus(run.status) && run.finished_at) return run.finished_at;
  return run.updated_at;
}

function hasRunDetails(run: RunStatus) {
  return Boolean(
    run.error
    || run.started_at
    || run.finished_at
    || (run.params && Object.keys(run.params).length > 0)
    || (run.labels && Object.keys(run.labels).length > 0),
  );
}

function hasRunTelemetry(run: RunStatus) {
  const provider = run.labels?.provider_name || run.labels?.provider || run.params?.provider;
  return Boolean(
    run.effective_limit
    || provider
    || taskMetricKeys.some((key) => runMetric(run, key) > 0),
  );
}

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>, defaultValue?: string) => string;

/** scopeLabel 是作用域在界面上的说法：有显示名用显示名，没有就回落到作用域加 id。 */
function scopeLabel(target: { scope: string; scope_id?: number; scope_name?: string }, t: Translate) {
  const name = target.scope_name || t(`task.scope.${target.scope}`, undefined, target.scope);
  return target.scope_id ? `${name} #${target.scope_id}` : name;
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
      {/* 筛的是任务还是运行必须写出来：同一排筛选器里三条判身份、两条判它最近一次运行，
          而下面那排清理按钮删的是运行记录。不写清楚，用户按下「按当前筛选清理」时不知道会删掉什么。 */}
      <p className="text-xs text-white/40">{t('logs.taskFilterScopeHint')}</p>
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
          <option value="queued">{t('logs.taskStatus.queued')}</option>
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

function RunProgressBar({ run }: { run: RunStatus }) {
  const percent = runProgressPercent(run);
  if (run.total <= 0) {
    // 总数未知：只有还在动的任务才画那条来回跑的不定进度条。停了的任务画它，看着像还在跑，
    // 而这条进度条一个数字都答不出——它连「做完了多少」都不知道。
    if (!isActiveRunStatus(run.status)) {
      return null;
    }
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
        <span>{run.current} / {run.total}</span>
        <span>{percent.toFixed(1)}%</span>
        {/* 有没有由后端决定，缺了就整格不显示——`0/min` 是另一种谎，它看着像「一分钟一条都没跑」。
            前端不得自己按状态判一遍：判据一旦与后端不同步，这一格就会多出一个它没算的数、或少掉一个。 */}
        {run.rate_per_minute !== undefined && <span>{formatRate(run.rate_per_minute)}</span>}
        {run.eta_seconds !== undefined && <span>ETA {formatDuration(run.eta_seconds)}</span>}
      </div>
    </div>
  );
}

/** RunControlButtons 是作用在**运行**上的那几个动作。重试不在其中：它作用在任务上，画在任务行上。 */
function RunControlButtons({ run, taskActionKey, onTaskAction }: { run: RunStatus; taskActionKey: string | null; onTaskAction: (run: RunStatus, action: TaskAction) => void }) {
  const { t } = useI18n();
  return (
    <div className="flex flex-wrap gap-2">
      {run.can_pause && run.status === 'running' && (
        <button type="button" onClick={() => onTaskAction(run, 'pause')} disabled={taskActionKey === `${run.key}:pause`} className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 px-3 py-2 text-xs text-amber-500 hover:bg-amber-500/10 disabled:opacity-50">
          <Pause className="h-3.5 w-3.5" />
          {t('settings.maintenance.pauseTask')}
        </button>
      )}
      {/* 不可暂停的运行要**明说**，否则用户按下全部暂停后看到它还在跑，会以为暂停失灵了。 */}
      {!run.can_pause && run.status === 'running' && (
        <span title={t('settings.maintenance.taskNotPausableHint')} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/45">
          <PauseCircle className="h-3.5 w-3.5" />
          {t('settings.maintenance.taskNotPausable')}
        </span>
      )}
      {run.can_resume && run.status === 'paused' && (
        <button type="button" onClick={() => onTaskAction(run, 'resume')} disabled={taskActionKey === `${run.key}:resume`} className="inline-flex items-center gap-1.5 rounded-lg border border-emerald-500/30 px-3 py-2 text-xs text-emerald-500 hover:bg-emerald-500/10 disabled:opacity-50">
          <Play className="h-3.5 w-3.5" />
          {t('settings.maintenance.resumeTask')}
        </button>
      )}
      {run.can_cancel && isActiveRunStatus(run.status) && (
        <button type="button" onClick={() => onTaskAction(run, 'cancel')} disabled={taskActionKey === `${run.key}:cancel` || run.status === 'cancelling'} className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/30 px-3 py-2 text-xs text-red-200 hover:bg-red-500/10 disabled:opacity-50">
          <XCircle className="h-3.5 w-3.5" />
          {t('common.cancel')}
        </button>
      )}
    </div>
  );
}

function RunLimitBadges({ run }: { run: RunStatus }) {
  const { t } = useI18n();
  const limit = run.effective_limit;
  const provider = run.labels?.provider_name || run.labels?.provider || run.params?.provider;
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

function RunMetricsGrid({ run }: { run: RunStatus }) {
  const { t } = useI18n();
  return (
    <>
      {taskMetricKeys.map((key) => (
        runMetric(run, key) > 0 && (
          <p key={key} className="rounded-lg border border-white/10 bg-white/3 px-3 py-2 text-white/55">
            {t(`settings.maintenance.taskMetric.${key}`)}<span className="mt-1 block text-white">{key.endsWith('_ms') ? `${runMetric(run, key)} ms` : runMetric(run, key)}</span>
          </p>
        )
      ))}
    </>
  );
}

function RunDetailDrawer({ run }: { run: RunStatus }) {
  const { t, formatDateTime } = useI18n();
  const ioParams = runIOParams(run);

  return (
    <div className="mt-3 space-y-3">
      {ioParams.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {ioParams.map(([key, value]) => (
            <span key={`${run.run_id}-io-${key}`} className="rounded-md border border-komgaPrimary/20 bg-komgaPrimary/10 px-2 py-1 text-[11px] text-komgaPrimary">
              {t(`logs.task.io.${key}`)}: {value}
            </span>
          ))}
        </div>
      )}
      {/* 暂停的来由只在暂停期间有话说：它答的是「谁把它按下的」，单条暂停还是全部暂停。 */}
      {run.pause_reason && (
        <p className="rounded-lg border border-amber-500/20 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
          {t('logs.task.pauseReason')}: {t(`logs.task.pauseReason.${run.pause_reason}`)}
        </p>
      )}
      {(run.started_at || run.finished_at) && (
        <div className="grid gap-2 text-xs sm:grid-cols-2">
          <div className="rounded-lg border border-white/10 bg-white/3 px-3 py-2">
            <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.startedAt')}</p>
            <p className="mt-1 text-white/65">{run.started_at ? formatDateTime(run.started_at) : '-'}</p>
          </div>
          <div className="rounded-lg border border-white/10 bg-white/3 px-3 py-2">
            <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.finishedAt')}</p>
            <p className="mt-1 text-white/65">{run.finished_at ? formatDateTime(run.finished_at) : t('logs.task.runningNow')}</p>
          </div>
        </div>
      )}
      {run.params && Object.keys(run.params).length > 0 && (
        <div>
          <p className="mb-2 text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.params')}</p>
          <div className="flex flex-wrap gap-2">
            {Object.entries(run.params).map(([key, value]) => (
              <span key={`${run.run_id}-${key}`} className="rounded-full border border-white/10 bg-gray-950 px-2.5 py-1 text-xs text-white/45">
                {key}: {value}
              </span>
            ))}
          </div>
        </div>
      )}
      {run.labels && Object.keys(run.labels).length > 0 && (
        <div>
          <p className="mb-2 text-[11px] uppercase tracking-[0.16em] text-white/35">{t('settings.maintenance.taskLabels')}</p>
          <div className="flex flex-wrap gap-2">
            {Object.entries(run.labels).map(([key, value]) => (
              <span key={`${run.run_id}-label-${key}`} className="rounded-full border border-white/10 bg-gray-950 px-2.5 py-1 text-xs text-white/45">
                {key}: {value}
              </span>
            ))}
          </div>
        </div>
      )}
      {run.error && (
        <div>
          <p className="mb-2 text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.errorDetails')}</p>
          <pre className="overflow-auto rounded-lg border border-red-500/20 bg-black/30 p-3 text-xs whitespace-pre-wrap wrap-break-word text-red-400">{run.error}</pre>
        </div>
      )}
    </div>
  );
}

function RunTelemetry({ run }: { run: RunStatus }) {
  return (
    <div className="mt-3 grid gap-2 text-xs md:grid-cols-2 xl:grid-cols-4">
      <RunLimitBadges run={run} />
      <RunMetricsGrid run={run} />
    </div>
  );
}

/**
 * RunCard 是一次**运行**的卡片，实况区与展开后的历次运行共用它。
 *
 * 它只画这一次运行的事：状态、**发起方**、进度与控制动作。任务层面的东西（重试、打开页面、
 * 连败与停发）画在任务行上——同一个按钮在两层各画一份，用户就分不清它作用在哪个对象上。
 */
function RunCard({
  run,
  expanded,
  taskActionKey,
  onToggleExpanded,
  onTaskAction,
  onViewTaskLogs,
}: {
  run: RunStatus;
  expanded: boolean;
  taskActionKey: string | null;
  onToggleExpanded: () => void;
  onTaskAction: (run: RunStatus, action: TaskAction) => void;
  onViewTaskLogs?: (run: RunStatus) => void;
}) {
  const { t, formatDateTime, formatRelativeTime } = useI18n();
  const statusLabel = t(`logs.taskStatus.${run.status}`);
  const timestamp = runTimestamp(run);

  return (
    <div className="rounded-xl border border-white/10 bg-gray-950/50 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className={`rounded-full border px-2.5 py-1 text-xs ${runBadgeClass(run.status)}`}>{statusLabel}</span>
            <p className="text-sm font-semibold text-white">{getTaskTypeLabel(run, t)}</p>
            <span className="text-xs text-white/40">{scopeLabel(run, t)}</span>
            {/* **发起方**：半夜转盘的那条究竟是谁叫来的。后端不发就整格不显示。 */}
            {run.trigger && (
              <span className="rounded-full border border-white/10 bg-white/4 px-2 py-0.5 text-[11px] text-white/50">
                {t(`logs.task.trigger.${run.trigger}`, undefined, run.trigger)}
              </span>
            )}
          </div>
          <p className="mt-2 truncate text-sm text-white/70" title={run.current_item || getTaskMessage(run, t)}>{getTaskMessage(run, t)}</p>
          <p className="mt-1 truncate text-xs text-white/35" title={run.current_item || undefined}>
            {run.phase ? t(`settings.maintenance.taskPhase.${run.phase}`) : '-'}{run.current_item ? ` - ${run.current_item}` : ''}
          </p>
          {timestamp && <p className="mt-1 text-xs text-white/35">{formatRelativeTime(timestamp)} - {formatDateTime(timestamp)}</p>}
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <RunControlButtons run={run} taskActionKey={taskActionKey} onTaskAction={onTaskAction} />
          {/* 每条运行各有一个日志入口：历次运行里点开的必须是**那一次**的日志，
              而不是这个任务最近那一次的。 */}
          {onViewTaskLogs && (
            <button type="button" onClick={() => onViewTaskLogs(run)} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/60 hover:bg-white/10 hover:text-white">
              <FileText className="h-3.5 w-3.5" />
              {t('logs.task.viewLogs')}
            </button>
          )}
          {hasRunDetails(run) && (
            <button type="button" onClick={onToggleExpanded} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/60 hover:bg-white/10 hover:text-white">
              <ChevronDown className={`h-3.5 w-3.5 transition-transform ${expanded ? 'rotate-180' : ''}`} />
              {expanded ? t('common.collapseDetails') : t('common.viewDetails')}
            </button>
          )}
        </div>
      </div>
      <RunProgressBar run={run} />
      {hasRunTelemetry(run) && <RunTelemetry run={run} />}
      {isInterruptedRun(run) && (
        <p className="mt-3 rounded-lg border border-amber-500/20 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">{t('logs.task.interruptedHint')}</p>
      )}
      {run.error && !expanded && (
        <p className="mt-3 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-400">{run.error}</p>
      )}
      {expanded && (
        <div className="mt-3 rounded-xl border border-white/10 bg-black/20 p-3">
          <RunDetailDrawer run={run} />
        </div>
      )}
    </div>
  );
}

/** RunCardList 画一组运行卡片，并自己记住哪一张展开着详情。去重键是**运行标识**——同一个任务键有多条运行。 */
function RunCardList({
  runs,
  taskActionKey,
  onTaskAction,
  onViewTaskLogs,
}: {
  runs: RunStatus[];
  taskActionKey: string | null;
  onTaskAction: (run: RunStatus, action: TaskAction) => void;
  onViewTaskLogs?: (run: RunStatus) => void;
}) {
  const [expandedRunId, setExpandedRunId] = useState<number | null>(null);
  return (
    <div className="space-y-3">
      {runs.map((run) => (
        <RunCard
          key={run.run_id}
          run={run}
          expanded={expandedRunId === run.run_id}
          taskActionKey={taskActionKey}
          onToggleExpanded={() => setExpandedRunId((current) => (current === run.run_id ? null : run.run_id))}
          onTaskAction={onTaskAction}
          onViewTaskLogs={onViewTaskLogs}
        />
      ))}
    </div>
  );
}

/**
 * LiveSection 是上半层**现在**：在跑的与排队中的运行常驻置顶，带槽位占用与全部暂停。
 *
 * 它不受下半层那排筛选影响：这里答的是「我的盘现在在干什么」，而那是个全局问题——
 * 顶部那对全部暂停 / 全部恢复同样作用于全体运行。
 */
function LiveSection({
  live,
  bulkPauseBusy,
  taskActionKey,
  onTaskAction,
  onPauseAll,
  onResumeAll,
  onViewTaskLogs,
}: {
  live: RunLive;
  bulkPauseBusy?: boolean;
  taskActionKey: string | null;
  onTaskAction: (run: RunStatus, action: TaskAction) => void;
  onPauseAll?: () => void;
  onResumeAll?: () => void;
  onViewTaskLogs?: (run: RunStatus) => void;
}) {
  const { t } = useI18n();
  const bulkPause = onPauseAll && onResumeAll;

  return (
    <section className="space-y-3 rounded-xl border border-white/10 bg-gray-950/40 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <h4 className="text-sm font-semibold text-white">{t('logs.taskCenter.liveTitle')}</h4>
          {/* 槽位上限为 0 表示今天没有上限可报，那就只写占用数——「2/2147483647」不是「槽位 2/2」。 */}
          <span className="rounded-full border border-white/10 bg-white/4 px-2.5 py-1 text-xs text-white/60">
            {live.slots > 0
              ? t('logs.taskCenter.slots', { active: live.active, slots: live.slots })
              : t('logs.taskCenter.activeRuns', { active: live.active })}
          </span>
          {/* 排队中今天不会产生（槽位实质关着）：一条都没有时整格不显示，而不是写一个「排队 0」。 */}
          {live.queued > 0 && (
            <span className="rounded-full border border-sky-500/30 bg-sky-500/10 px-2.5 py-1 text-xs text-sky-300">
              {t('logs.taskCenter.queued', { count: live.queued })}
            </span>
          )}
        </div>
        {/* 两个按钮并排，不是一个按状态翻面的开关：一条已暂停、三条还在跑时，翻面的那个只剩
            「全部恢复」，想让盘安静下来的用户得先恢复再暂停——那与他按下去的意图正好相反。 */}
        {bulkPause && (
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={onPauseAll}
              disabled={bulkPauseBusy}
              className="inline-flex items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-500 hover:bg-amber-500/15 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Pause className="h-4 w-4" />
              {t('settings.maintenance.pauseAllRuns')}
            </button>
            {/* 一条都没暂停时「全部恢复」按下去什么也不会发生，因此按实况帧那个全局答案禁用它。 */}
            <button
              type="button"
              onClick={onResumeAll}
              disabled={bulkPauseBusy || !live.paused}
              className="inline-flex items-center gap-2 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-500 hover:bg-emerald-500/15 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Play className="h-4 w-4" />
              {t('settings.maintenance.resumeAllRuns')}
            </button>
          </div>
        )}
      </div>

      {/* 这句话写在按钮旁边而不是只挂在 title 上：理由同 RunControlButtons 里那条说明。 */}
      {bulkPause && <p className="text-xs text-white/40">{t('settings.maintenance.pauseAllHint')}</p>}

      {live.runs.length === 0
        ? <p className="rounded-xl border border-white/10 bg-white/3 p-4 text-sm text-white/50">{t('logs.taskCenter.noLiveRuns')}</p>
        : <RunCardList runs={live.runs} taskActionKey={taskActionKey} onTaskAction={onTaskAction} onViewTaskLogs={onViewTaskLogs} />}
    </section>
  );
}

/**
 * TaskRow 是下半层的一行：一个**任务**，写着上次结果、连败与停发，展开看它的历次运行。
 *
 * 历次运行由页面按需取回（taskRuns 为 undefined 即还没取回来），列表接口一条都不带。
 */
function TaskRow({
  task,
  history,
  taskActionKey,
  onToggle,
  onTaskAction,
  onOpenTaskTarget,
  onViewTaskLogs,
}: {
  task: TaskSummary;
  // history 只在这一行正是展开着的那一行时给出。
  history?: TaskRunHistory;
  taskActionKey: string | null;
  onToggle?: () => void;
  onTaskAction: (run: RunStatus, action: TaskAction) => void;
  onOpenTaskTarget?: (target: TaskTarget) => void;
  onViewTaskLogs?: (run: RunStatus) => void;
}) {
  const { t, formatDateTime, formatRelativeTime } = useI18n();
  const lastRun = task.last_run;
  const expanded = Boolean(history);
  // 停发是**退避**与禁用那一组的对外说法：任一为真就标红。今天没有写入方，因此不会出现。
  const stalled = task.disabled || Boolean(task.backoff_until && new Date(task.backoff_until).getTime() > Date.now());
  const timestamp = lastRun ? runTimestamp(lastRun) : '';

  return (
    <div className={`rounded-xl border bg-gray-950/50 ${stalled ? 'border-red-500/30' : 'border-white/10'}`}>
      <div className="flex flex-wrap items-start justify-between gap-3 p-4">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={expanded}
          className="flex min-w-0 flex-1 flex-wrap items-center gap-2 text-left"
        >
          <ChevronDown className={`h-3.5 w-3.5 shrink-0 text-white/45 transition-transform ${expanded ? 'rotate-180' : '-rotate-90'}`} />
          <span className={`rounded-full border px-2.5 py-1 text-xs ${lastRun ? runBadgeClass(lastRun.status) : runBadgeClass('')}`}>
            {lastRun ? t(`logs.taskStatus.${lastRun.status}`) : t('logs.taskCenter.neverRun')}
          </span>
          <span className="text-sm font-semibold text-white">{getTaskTypeLabel({ type: task.type, params: lastRun?.params }, t)}</span>
          <span className="text-xs text-white/40">{scopeLabel(task, t)}</span>
          {timestamp && <span className="text-xs text-white/35" title={formatDateTime(timestamp)}>{formatRelativeTime(timestamp)}</span>}
          {/* 连败与停发在无数据时整格不显示：画一个「连败 0」只是替一个还没有写入方的字段占位。 */}
          {task.fail_streak > 0 && (
            <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] text-amber-500">
              {t('logs.taskCenter.failStreak', { count: task.fail_streak })}
            </span>
          )}
          {stalled && (
            <span className="rounded-full border border-red-500/30 bg-red-500/10 px-2 py-0.5 text-[11px] text-red-200">
              {t('logs.taskCenter.stalled')}
            </span>
          )}
        </button>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {/* 重试作用在**任务**上：它新起一次运行，被重试的那一条原样留着。因此这个按钮一个任务只有一个，
              而不是每条历次运行各画一个。 */}
          {lastRun?.retryable && !isActiveRunStatus(lastRun.status) && (
            <button type="button" onClick={() => onTaskAction(lastRun, 'retry')} disabled={taskActionKey === `${lastRun.key}:retry`} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/70 hover:bg-white/10 disabled:opacity-50">
              <RotateCcw className={`h-3.5 w-3.5 ${taskActionKey === `${lastRun.key}:retry` ? 'animate-spin' : ''}`} />
              {t('common.retry')}
            </button>
          )}
          {onOpenTaskTarget && (
            <button type="button" onClick={() => onOpenTaskTarget(task)} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/60 hover:bg-white/10 hover:text-white">
              <ExternalLink className="h-3.5 w-3.5" />
              {t('logs.task.openPage')}
            </button>
          )}
        </div>
      </div>
      {lastRun && <p className="px-4 pb-3 text-xs text-white/45">{getTaskMessage(lastRun, t)}</p>}
      <p className="px-4 pb-4 text-xs text-white/35">{getTaskActionHint({ type: task.type, params: lastRun?.params }, t)}</p>
      {expanded && (
        <div className="border-t border-white/10 p-4">
          <p className="mb-3 inline-flex items-center gap-1.5 text-[11px] uppercase tracking-[0.16em] text-white/35">
            <ListTree className="h-3.5 w-3.5" />
            {t('logs.taskCenter.runHistory')}
          </p>
          {history?.loading && <p className="text-sm text-white/50">{t('common.loading')}</p>}
          {!history?.loading && history?.runs?.length === 0 && <p className="text-sm text-white/50">{t('logs.taskCenter.noRunHistory')}</p>}
          {!history?.loading && history?.runs && history.runs.length > 0 && (
            <RunCardList runs={history.runs} taskActionKey={taskActionKey} onTaskAction={onTaskAction} onViewTaskLogs={onViewTaskLogs} />
          )}
        </div>
      )}
    </div>
  );
}

export function TaskCenter({
  live,
  tasks,
  loading,
  taskActionKey,
  history,
  bulkPauseBusy,
  filters,
  typeOptions = [],
  currentFilterCanClear,
  onRefresh,
  onTaskAction,
  onToggleTask,
  onPauseAll,
  onResumeAll,
  onFilterChange,
  onClearTasks,
  onOpenTaskTarget,
  onViewTaskLogs,
}: TaskCenterProps) {
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

      <LiveSection
        live={live}
        bulkPauseBusy={bulkPauseBusy}
        taskActionKey={taskActionKey}
        onTaskAction={onTaskAction}
        onPauseAll={onPauseAll}
        onResumeAll={onResumeAll}
        onViewTaskLogs={onViewTaskLogs}
      />

      <section className="space-y-3 rounded-xl border border-white/10 bg-gray-950/40 p-4">
        <h4 className="text-sm font-semibold text-white">{t('logs.taskCenter.taskListTitle')}</h4>
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
        {tasks.length === 0
          ? <p className="rounded-xl border border-white/10 bg-white/3 p-4 text-sm text-white/50">{t('settings.maintenance.noTasks')}</p>
          : (
            <div className="space-y-3">
              {tasks.map((task) => (
                <TaskRow
                  key={task.task_id}
                  task={task}
                  history={history?.taskId === task.task_id ? history : undefined}
                  taskActionKey={taskActionKey}
                  onToggle={onToggleTask ? () => onToggleTask(task.task_id) : undefined}
                  onTaskAction={onTaskAction}
                  onOpenTaskTarget={onOpenTaskTarget}
                  onViewTaskLogs={onViewTaskLogs}
                />
              ))}
            </div>
          )}
      </section>
    </section>
  );
}
