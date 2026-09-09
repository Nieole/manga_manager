/**
 * 任务中心的两层结构：上半**实况区**是仍会变化的那些**运行**，常驻置顶、不受筛选影响；
 * 下半**任务清单**一个**任务**一行，写着上次结果，展开才按需取它的历次运行。
 * 本组件不取数——两层各自的数据由页面交进来，展开哪一行也由页面决定（它要据此发请求）。
 */

import { useState } from 'react';
import { Activity, Ban, ChevronDown, ExternalLink, FileText, ListTree, Pause, PauseCircle, Play, RefreshCw, RotateCcw, Search, Terminal, Trash2, XCircle } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { getTaskActionHint, getTaskMessage, getTaskTypeLabel } from '../../i18n/task';
import { runCardId } from '../../utils/runCard';
import { isActiveRunStatus, isLiveRunStatus } from '../../utils/runStatus';

// TaskLimits / RunStatus / RunLive / TaskSummary 由 cmd/tsgen 从 Go 后端响应结构体生成
// （单一事实源，见 api/generated.ts），此处再导出以保持既有 import 路径不变。
export type { TaskLimits, RunStatus, RunLive, TaskSummary, RunEvent, RunPhaseSpan, RunEventsResponse, RunSample, RunSamplesResponse } from '../../api/generated';
import type { RunEvent, RunEventsResponse, RunLive, RunPhaseSpan, RunSample, RunSamplesResponse, RunStatus, TaskSummary } from '../../api/generated';

// 运行上的动作作用在**运行**上，重试作用在**任务**上（它重新发起一次，不改动被重试的那一条）。
export type TaskAction = 'pause' | 'resume' | 'cancel' | 'retry';

// **停发**的三条原因各画各的，因为用户要做的事不同：连败到阈值是「去修那块盘」，标红；
// 人工禁用是他自己按下的，画中性色即可；退避会自己走完，只写「还要等到什么时候」。
// 红色因此只对应票据里那一条「连败 ≥6 次停发并标红」，不至于贬值成「这一行有点什么」。
const STALL_FAIL_LIMIT = 'fail_limit';
const STALL_DISABLED = 'disabled';
const STALL_BACKOFF = 'backoff';

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

/**
 * RunDetailView 是**打开着详情面板的那一张运行卡片**与它按需拉回来的两份东西：
 * **运行事件**与**采样**。
 *
 * 两份收在一份视图里而不是两个各自可空的 prop：它们一起打开、一起关掉，只给其中一个不是一种
 * 有意义的状态。各自仍可缺席——取失败的那一份为 undefined，另一份照画。
 *
 * 认的是卡片而不是运行：同一条运行会同时出现在实况区与展开着的历次运行里，按运行认的话
 * 两张卡片下面各画一份，用户会以为那是两条运行各自的失败。cardId 由 runCardId 拼出。
 *
 * 一次只留一张：两份都按需拉（它们不进推送通道），留着上一张的话再点开会先闪一眼别人的东西。
 */
export interface RunDetailView {
  cardId: string;
  events?: RunEventsResponse;
  samples?: RunSamplesResponse;
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
  // 「查看日志」打开的是**这次运行自己的详情面板**（吞吐曲线与事件流）；「原始日志」才是按运行
  // 过滤的那份全局日志。两个入口并排：详情答「这次出了什么事、卡在哪」，原始日志答「那一刻还
  // 发生了什么」。
  onViewRunDetail?: (run: RunStatus, cardId: string) => void;
  onViewRawLogs?: (run: RunStatus) => void;
  // detail 是当前打开着详情面板的那一张卡片；不给即一张都没打开。
  detail?: RunDetailView;
  // 人工禁用那条开关作用在**任务**上，因此它收的是任务而不是运行。不给即不画那个按钮。
  onToggleTaskAuto?: (task: TaskSummary) => void;
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
  'failed_covers',
  'remaining_covers',
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
        <button type="button" onClick={() => onTaskAction(run, 'pause')} disabled={taskActionKey === `${run.run_id}:pause`} className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 px-3 py-2 text-xs text-amber-500 hover:bg-amber-500/10 disabled:opacity-50">
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
        <button type="button" onClick={() => onTaskAction(run, 'resume')} disabled={taskActionKey === `${run.run_id}:resume`} className="inline-flex items-center gap-1.5 rounded-lg border border-emerald-500/30 px-3 py-2 text-xs text-emerald-500 hover:bg-emerald-500/10 disabled:opacity-50">
          <Play className="h-3.5 w-3.5" />
          {t('settings.maintenance.resumeTask')}
        </button>
      )}
      {/* 判据是「还会不会变」而不是「是不是活动态」：**排队中**的运行也取消得掉——它从未开跑，
          按下去当场进已取消，用户不必等它开跑再停它。按活动态判的话那张卡片上一个按钮都没有。 */}
      {run.can_cancel && isLiveRunStatus(run.status) && (
        <button type="button" onClick={() => onTaskAction(run, 'cancel')} disabled={taskActionKey === `${run.run_id}:cancel` || run.status === 'cancelling'} className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/30 px-3 py-2 text-xs text-red-200 hover:bg-red-500/10 disabled:opacity-50">
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

/** formatMillis 把一段耗时说成人话。它服务阶段时间线，因此秒以下也要说得出——有的段就是很短。 */
function formatMillis(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) return '0s';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return formatDuration(ms / 1000);
}

/**
 * RunPhaseTimeline 画「慢在哪一段」：每个**阶段**一行，写着它跑了多久。
 *
 * 段长是后端由相邻两条阶段事件相减算出来的，前端不再自己减一遍——减出来的第二份一旦与
 * 事件流里的时刻错开，界面就在自己打自己的脸。
 */
function RunPhaseTimeline({ phases }: { phases: RunPhaseSpan[] }) {
  const { t } = useI18n();
  const total = phases.reduce((sum, span) => sum + Math.max(0, span.duration_ms), 0);
  return (
    <div className="space-y-1.5">
      <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.phaseTimeline')}</p>
      {phases.map((span, index) => (
        <div key={`${span.phase}-${span.started_at}-${index}`} className="rounded-lg border border-white/10 bg-white/3 px-3 py-2">
          <div className="flex flex-wrap items-baseline justify-between gap-2 text-xs">
            <span className="text-white/70">
              {t(`settings.maintenance.taskPhase.${span.phase}`, undefined, span.phase)}
              {span.current && <span className="ml-2 text-emerald-400">{t('logs.task.phaseCurrent')}</span>}
            </span>
            <span className="text-white/50">{formatMillis(span.duration_ms)}</span>
          </div>
          {/* 一条按占比拉长的条子：读「40 分钟」要算，读一根长条不用算。 */}
          <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-white/5">
            <div className="h-full rounded-full bg-komgaPrimary/70" style={{ width: `${total > 0 ? (span.duration_ms / total) * 100 : 0}%` }} />
          </div>
        </div>
      ))}
    </div>
  );
}

/**
 * RunFailureList 画「哪些文件失败了、为什么」。
 *
 * 上限之外的那些只有一个计数（后端每次运行最多留一批明细），因此列完之后要说出还有多少条——
 * 不说的话，用户会以为失败就这么几个。
 */
function RunFailureList({ failures, omitted }: { failures: RunEvent[]; omitted: number }) {
  const { t } = useI18n();
  return (
    <div className="space-y-1.5">
      <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.failedItems', { count: failures.length })}</p>
      <div className="max-h-64 space-y-1.5 overflow-auto">
        {failures.map((failure, index) => (
          <div key={`${failure.item}-${index}`} className="rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2">
            <p className="wrap-break-word text-xs text-white/70">{failure.item}</p>
            {failure.reason && <p className="mt-1 wrap-break-word text-[11px] text-red-300/80">{failure.reason}</p>}
          </div>
        ))}
      </div>
      {omitted > 0 && (
        <p className="rounded-lg border border-amber-500/20 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
          {t('logs.task.failedItemsOmitted', { count: omitted })}
        </p>
      )}
    </div>
  );
}

/** runEventLabel 是事件流里一行的说法：四类各念各的。 */
function runEventLabel(event: RunEvent, t: Translate) {
  switch (event.kind) {
    case 'phase':
      return t('logs.task.event.phase', { phase: t(`settings.maintenance.taskPhase.${event.phase}`, undefined, event.phase || '') });
    case 'item':
      return `${event.item}${event.reason ? ` - ${event.reason}` : ''}`;
    case 'control':
      return t(`logs.task.event.control.${event.action}`, undefined, event.action || '');
    default:
      return `${t(`logs.task.event.warn.${event.code}`, { count: event.count || 0 }, event.code || '')}${event.detail ? ` - ${event.detail}` : ''}`;
  }
}

function runEventClass(kind: string) {
  if (kind === 'item') return 'text-red-300/80';
  if (kind === 'warn') return 'text-amber-400/90';
  if (kind === 'control') return 'text-sky-300/80';
  return 'text-white/60';
}

// 曲线画布的坐标系。它只是一套内部坐标：SVG 按 viewBox 缩放到容器宽度，
// 因此这两个数不是像素，改它们不改变界面上的大小。
const CURVE_WIDTH = 600;
const CURVE_HEIGHT = 120;

// 相邻两点隔了超过这么多个「正常间距」，就当中间漏了点，曲线在那里断开。
//
// 二倍而不是一倍：取点的节拍与它的水位各读一次时钟，正常节奏下也会偶尔隔出一个多间隔，
// 按一倍判会把一条连续的曲线切成一串碎段。
const SAMPLE_GAP_FACTOR = 2;

/**
 * typicalGapMs 是这一条曲线**自己的**取点间距：全部相邻间距取中位数。
 *
 * 不读「此刻设置里的间隔」：取点间隔是可配的，而这些点是**当时**那个节奏留下的。
 * 拿现在的间隔去量过去的点，把 60 秒改成 10 秒之后，每一条老曲线都会被判成处处漏点、
 * 碎成一串圆点。中位数而不是最小值：一次时钟抖动就能挤出一个极小的间距。
 *
 * 少于两个点时没有间距可量，交回 0——那种情况下也无从断开。
 */
function typicalGapMs(samples: RunSample[]) {
  if (samples.length < 2) return 0;
  const gaps = samples
    .slice(1)
    .map((sample, index) => new Date(sample.at).getTime() - new Date(samples[index].at).getTime())
    .sort((a, b) => a - b);
  return gaps[Math.floor(gaps.length / 2)];
}

/**
 * curveSegments 把采样点切成若干段连续的点：**有点就连线，没点就断开**。
 *
 * 断口只留给「这一段我们一个观测都没有」——丢点、过保留期、以及重启前后。连过去就是画一段
 * 根本没发生过的数据，而这条曲线存在的理由恰恰是不让用户猜。
 *
 * **暂停不断**：暂停是**活动态**，采样照取，那几个点的吞吐是真实的零，曲线在那里是平的。
 * 平的说的是「它没在产出」，断的说的是「这一段我们不知道」，两件事不能画成一个样子。
 */
function curveSegments(samples: RunSample[]) {
  const typical = typicalGapMs(samples);
  if (typical <= 0) return samples.length > 0 ? [samples] : [];
  const maxGapMs = typical * SAMPLE_GAP_FACTOR;
  const segments: RunSample[][] = [];
  let segment: RunSample[] = [];
  for (const sample of samples) {
    const previous = segment[segment.length - 1];
    if (previous && new Date(sample.at).getTime() - new Date(previous.at).getTime() > maxGapMs) {
      segments.push(segment);
      segment = [];
    }
    segment.push(sample);
  }
  if (segment.length > 0) segments.push(segment);
  return segments;
}

/**
 * RunThroughputCurve 画这一次运行的吞吐曲线：横轴是时刻，纵轴是每分钟处理了多少条。
 * 它答的是用户故事 12 那一句——「它是不是卡住了」不用靠猜，卡住的那一段自己贴着零走。
 *
 * **一段连续的点画成一条 <polyline>，不是一个点一个元素。** 一次跑一夜的运行有几千个点，
 * 逐点画等于把它们全塞进 DOM；折线的点集是一个属性，浏览器画一次。零长的那一段（只剩一个点）
 * 靠圆头线帽画成一个点，因此不必为它另开一条渲染路径。
 *
 * 纵轴按这一次运行自己的峰值归一，不设固定刻度：不同任务的量级差着几个数量级，
 * 而这条曲线要回答的是「现在比刚才慢了多少」，不是「它比别的运行快不快」。
 */
function RunThroughputCurve({ data }: { data: RunSamplesResponse }) {
  const { t, formatDateTime } = useI18n();
  const samples = data.samples || [];
  const retentionDays = data.retention_days;

  if (samples.length === 0) {
    // 一个点都没有分两种情况，说的话完全不同：过了保留期是「曲线没了」（运行还在，这是设计），
    // 没过就是「还没攒够第一个点」。含糊过去、或者画一条空轴，都是在让用户猜。
    return (
      <p className="text-xs text-white/40">
        {data.expired && retentionDays > 0
          ? t('logs.task.samplesGone', { days: retentionDays })
          : t('logs.task.noSamples')}
      </p>
    );
  }

  const from = new Date(samples[0].at).getTime();
  const to = new Date(samples[samples.length - 1].at).getTime();
  // 单点（或同一毫秒里的几个点）跨度为零：夹到 1 只是别让除法炸掉，画出来仍是最左边一个点。
  const span = Math.max(1, to - from);
  const peak = samples.reduce((max, sample) => Math.max(max, sample.throughput_per_minute), 0);
  const pointsOf = (segment: RunSample[]) => segment
    .map((sample) => {
      const x = ((new Date(sample.at).getTime() - from) / span) * CURVE_WIDTH;
      const y = CURVE_HEIGHT - (peak > 0 ? (sample.throughput_per_minute / peak) * CURVE_HEIGHT : 0);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');

  return (
    <div className="space-y-1.5" data-testid="run-throughput-curve">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.throughput')}</p>
        <span className="text-[11px] text-white/40">{t('logs.task.throughputPeak', { rate: formatRate(peak) })}</span>
      </div>
      <svg
        viewBox={`0 0 ${CURVE_WIDTH} ${CURVE_HEIGHT}`}
        preserveAspectRatio="none"
        className="h-28 w-full rounded-lg border border-white/10 bg-black/30"
        role="img"
        aria-label={t('logs.task.throughput')}
      >
        {curveSegments(samples).map((segment, index) => (
          <polyline
            key={`${segment[0].at}-${index}`}
            points={pointsOf(segment)}
            fill="none"
            className="stroke-komgaPrimary"
            strokeWidth={1.5}
            strokeLinecap="round"
            strokeLinejoin="round"
            vectorEffect="non-scaling-stroke"
          />
        ))}
      </svg>
      <div className="flex flex-wrap items-baseline justify-between gap-2 text-[11px] text-white/35">
        <span>{formatDateTime(samples[0].at)}</span>
        <span>{t('logs.task.throughputPoints', { count: samples.length })}</span>
        <span>{formatDateTime(samples[samples.length - 1].at)}</span>
      </div>
      {/* 两句都是「这条曲线不完整」，但缺的那一段各有出处，因此各说各的。 */}
      {data.expired && retentionDays > 0 && (
        <p className="text-[11px] text-amber-500">{t('logs.task.samplesExpired', { days: retentionDays })}</p>
      )}
      {data.truncated && <p className="text-[11px] text-amber-500">{t('logs.task.samplesTruncated')}</p>}
    </div>
  );
}

/**
 * RunEventStream 是「查看日志」打开的第二块：这**一次运行**自己的事件流，
 * 而不是在全局日志里 grep 一个子串。
 *
 * 三块自上而下：阶段时间线答「慢在哪一段」，失败明细答「哪些文件、为什么」，
 * 整条事件流答「中间还发生过什么」。第一块是它上面那条吞吐曲线，答「它是不是卡住了」。
 */
function RunEventStream({ data }: { data: RunEventsResponse }) {
  const { t, formatDateTime } = useI18n();
  const { events, phases, truncated } = data;
  const failures = events.filter((event) => event.kind === 'item');
  // 「还有多少条没列出」由后端那一格回答，不在事件流里自己找：运行还在跑时那条告警根本还没落，
  // 而事件多到被截断时，最后落下的恰好就是它。
  const omitted = data.omitted_failures || 0;

  if (events.length === 0) {
    return <p className="text-xs text-white/40">{t('logs.task.noEvents')}</p>;
  }
  return (
    <div className="space-y-3">
      {phases.length > 0 && <RunPhaseTimeline phases={phases} />}
      {(failures.length > 0 || omitted > 0) && <RunFailureList failures={failures} omitted={omitted} />}
      <div className="space-y-1">
        <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{t('logs.task.eventStream')}</p>
        <div className="max-h-64 space-y-1 overflow-auto rounded-lg border border-white/10 bg-black/30 p-2">
          {events.map((event, index) => (
            <p key={`${event.at}-${index}`} className="flex gap-2 text-[11px]">
              <span className="shrink-0 text-white/30">{formatDateTime(event.at)}</span>
              <span className={`wrap-break-word ${runEventClass(event.kind)}`}>{runEventLabel(event, t)}</span>
            </p>
          ))}
        </div>
        {truncated && <p className="text-[11px] text-amber-500">{t('logs.task.eventsTruncated')}</p>}
      </div>
    </div>
  );
}

/**
 * RunDetailPanel 是「查看日志」打开的整块：上面是吞吐曲线（**它是不是卡住了**），
 * 下面是事件流（**这次出了什么事**）。
 *
 * 两份各自可能缺席（取失败的那一份没交进来），缺的那份整块不画——其余照旧。
 * 加载态判在这里而不是判在两块里各一次：它们一起开、一起关，各判一次只会闪两回。
 */
function RunDetailPanel({ view }: { view: RunDetailView }) {
  const { t } = useI18n();
  if (view.loading) {
    return <p className="text-xs text-white/40">{t('common.loading')}</p>;
  }
  return (
    <div className="space-y-3">
      {view.samples && <RunThroughputCurve data={view.samples} />}
      {view.events && <RunEventStream data={view.events} />}
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
  cardId,
  expanded,
  taskActionKey,
  detail,
  onToggleExpanded,
  onTaskAction,
  onViewRunDetail,
  onViewRawLogs,
}: {
  run: RunStatus;
  // cardId 是这张卡片在这一屏里的身份，见 runCardId：同一条运行可以同时出现在两区。
  cardId: string;
  expanded: boolean;
  taskActionKey: string | null;
  detail?: RunDetailView;
  onToggleExpanded: () => void;
  onTaskAction: (run: RunStatus, action: TaskAction) => void;
  onViewRunDetail?: (run: RunStatus, cardId: string) => void;
  onViewRawLogs?: (run: RunStatus) => void;
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
            {/* **合并**计数：这条排队代表了几次发起。没合并过就整格不显示，而不是写一个「已合并 0 次」。 */}
            {Boolean(run.coalesced_count) && (
              <span className="rounded-full border border-sky-500/30 bg-sky-500/10 px-2 py-0.5 text-[11px] text-sky-300">
                {t('logs.task.coalesced', { count: run.coalesced_count || 0 })}
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
          {/* 每条运行各有自己的两个入口：历次运行里点开的必须是**那一次**的事件流与日志，
              而不是这个任务最近那一次的。 */}
          {onViewRunDetail && (
            <button type="button" onClick={() => onViewRunDetail(run, cardId)} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/60 hover:bg-white/10 hover:text-white">
              <FileText className="h-3.5 w-3.5" />
              {t('logs.task.viewLogs')}
            </button>
          )}
          {/* 原始日志留在详情面板旁边：事件是「用户该知道的」，日志是「排障要的」，同一件事不写两处。 */}
          {onViewRawLogs && (
            <button type="button" onClick={() => onViewRawLogs(run)} className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/45 hover:bg-white/10 hover:text-white">
              <Terminal className="h-3.5 w-3.5" />
              {t('logs.task.rawLogs')}
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
      {detail?.cardId === cardId && (
        <div className="mt-3 rounded-xl border border-white/10 bg-black/20 p-3">
          <RunDetailPanel view={detail} />
        </div>
      )}
    </div>
  );
}

/** RunCardList 画一组运行卡片，并自己记住哪一张展开着详情。去重键是**运行标识**——同一个任务键有多条运行。 */
function RunCardList({
  runs,
  origin,
  taskActionKey,
  detail,
  onTaskAction,
  onViewRunDetail,
  onViewRawLogs,
}: {
  runs: RunStatus[];
  // origin 是这一组卡片画在哪一区（实况区 / 展开着的历次运行），与运行标识一起拼出卡片身份。
  origin: string;
  taskActionKey: string | null;
  detail?: RunDetailView;
  onTaskAction: (run: RunStatus, action: TaskAction) => void;
  onViewRunDetail?: (run: RunStatus, cardId: string) => void;
  onViewRawLogs?: (run: RunStatus) => void;
}) {
  const [expandedRunId, setExpandedRunId] = useState<number | null>(null);
  return (
    <div className="space-y-3">
      {runs.map((run) => (
        <RunCard
          key={run.run_id}
          run={run}
          cardId={runCardId(origin, run.run_id)}
          expanded={expandedRunId === run.run_id}
          taskActionKey={taskActionKey}
          detail={detail}
          onToggleExpanded={() => setExpandedRunId((current) => (current === run.run_id ? null : run.run_id))}
          onTaskAction={onTaskAction}
          onViewRunDetail={onViewRunDetail}
          onViewRawLogs={onViewRawLogs}
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
  detail,
  onViewRunDetail,
  onViewRawLogs,
}: {
  live: RunLive;
  bulkPauseBusy?: boolean;
  taskActionKey: string | null;
  onTaskAction: (run: RunStatus, action: TaskAction) => void;
  onPauseAll?: () => void;
  onResumeAll?: () => void;
  detail?: RunDetailView;
  onViewRunDetail?: (run: RunStatus, cardId: string) => void;
  onViewRawLogs?: (run: RunStatus) => void;
}) {
  const { t } = useI18n();
  const bulkPause = onPauseAll && onResumeAll;

  return (
    <section className="space-y-3 rounded-xl border border-white/10 bg-gray-950/40 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <h4 className="text-sm font-semibold text-white">{t('logs.taskCenter.liveTitle')}</h4>
          {/* 后端恒发一个真的会被撞上的上限；报 0 只可能是它没答上来，那就退回只写占用数。 */}
          <span className="rounded-full border border-white/10 bg-white/4 px-2.5 py-1 text-xs text-white/60">
            {live.slots > 0
              ? t('logs.taskCenter.slots', { active: live.active, slots: live.slots })
              : t('logs.taskCenter.activeRuns', { active: live.active })}
          </span>
          {/* 排队中一条都没有时整格不显示，而不是写一个「排队 0」。 */}
          {live.queued > 0 && (
            <span className="rounded-full border border-sky-500/30 bg-sky-500/10 px-2.5 py-1 text-xs text-sky-300">
              {t('logs.taskCenter.queued', { count: live.queued })}
            </span>
          )}
          {/* 全部暂停的闸门关着时说出来：否则用户看到的是一条排队一动不动，而队列正被这个闸门拦着。 */}
          {live.paused_all && (
            <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 text-xs text-amber-500">
              {t('logs.taskCenter.pausedAll')}
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
            {/* 一条都没暂停、闸门也开着时「全部恢复」按下去什么也不会发生，因此按实况帧那两个
                全局答案禁用它。**两个都要看**：被暂停的那几条被取消或跑完之后 paused 就回到 false，
                而闸门仍关着拦住队列——只看 paused 的话，这个按钮会灰在唯一能重新放开队列的位置上。 */}
            <button
              type="button"
              onClick={onResumeAll}
              disabled={bulkPauseBusy || (!live.paused && !live.paused_all)}
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
        : <RunCardList runs={live.runs} origin="live" taskActionKey={taskActionKey} detail={detail} onTaskAction={onTaskAction} onViewRunDetail={onViewRunDetail} onViewRawLogs={onViewRawLogs} />}
    </section>
  );
}

// stallColorClass 把**停发**原因翻成那一行说明文字的颜色，与上面三个徽章同一套口径。
function stallColorClass(reason: string) {
  if (reason === STALL_FAIL_LIMIT) return 'text-red-200/80';
  if (reason === STALL_BACKOFF) return 'text-amber-500/80';
  return 'text-white/50';
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
  detail,
  onViewRunDetail,
  onViewRawLogs,
  onToggleTaskAuto,
}: {
  task: TaskSummary;
  // history 只在这一行正是展开着的那一行时给出。
  history?: TaskRunHistory;
  taskActionKey: string | null;
  onToggle?: () => void;
  onTaskAction: (run: RunStatus, action: TaskAction) => void;
  onOpenTaskTarget?: (target: TaskTarget) => void;
  // 「查看日志」打开的是**这次运行自己的详情面板**（吞吐曲线与事件流）；「原始日志」才是按运行
  // 过滤的那份全局日志。两个入口并排：详情答「这次出了什么事、卡在哪」，原始日志答「那一刻还
  // 发生了什么」。前者带上卡片身份：同一条运行可以同时出现在两区，页面据此只让被点的那张画。
  onViewRunDetail?: (run: RunStatus, cardId: string) => void;
  onViewRawLogs?: (run: RunStatus) => void;
  // detail 是当前打开着详情面板的那一张卡片；不给即一张都没打开。
  detail?: RunDetailView;
  onToggleTaskAuto?: (task: TaskSummary) => void;
}) {
  const { t, formatDateTime, formatRelativeTime } = useI18n();
  const lastRun = task.last_run;
  const expanded = Boolean(history);
  // **停发的判据只有后端一处**：它要三个阈值、三个字段与此刻的时间，而阈值在设置里可改——
  // 在这里拿 disabled / backoff_until 自己再推一遍，改完设置两边立刻对不上。
  const stallReason = task.stall_reason || '';
  const stalled = stallReason === STALL_FAIL_LIMIT;
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
          {stallReason === STALL_DISABLED && (
            <span className="rounded-full border border-white/15 bg-white/5 px-2 py-0.5 text-[11px] text-white/60">
              {t('logs.taskCenter.autoDisabled')}
            </span>
          )}
          {stallReason === STALL_BACKOFF && (
            <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] text-amber-500">
              {t('logs.taskCenter.backingOff')}
            </span>
          )}
        </button>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {/* 禁用作用在**任务**上，与重试并排：它关掉的是自动发起，手动发起（重试）照旧可用。 */}
          {onToggleTaskAuto && (
            <button
              type="button"
              onClick={() => onToggleTaskAuto(task)}
              disabled={taskActionKey === `${task.task_id}:auto`}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/70 hover:bg-white/10 disabled:opacity-50"
            >
              {task.disabled ? <Play className="h-3.5 w-3.5" /> : <Ban className="h-3.5 w-3.5" />}
              {t(task.disabled ? 'logs.taskCenter.enableAuto' : 'logs.taskCenter.disableAuto')}
            </button>
          )}
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
      {/* 「写明原因」那半句：光标红答不出「我该去修什么」，因此连败几次、还要等多久、
          以及最后一次的错误都写在这里。 */}
      {stallReason && (
        <p className={`px-4 pb-3 text-xs ${stallColorClass(stallReason)}`}>
          {t(`logs.taskCenter.stallReason.${stallReason}`, {
            count: task.fail_streak,
            until: task.backoff_until ? formatDateTime(task.backoff_until) : '',
          })}
          {lastRun?.error && ` · ${t('logs.taskCenter.lastError', { error: lastRun.error })}`}
        </p>
      )}
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
            <RunCardList runs={history.runs} origin="history" taskActionKey={taskActionKey} detail={detail} onTaskAction={onTaskAction} onViewRunDetail={onViewRunDetail} onViewRawLogs={onViewRawLogs} />
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
  detail,
  onViewRunDetail,
  onViewRawLogs,
  onToggleTaskAuto,
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
        detail={detail}
        onViewRunDetail={onViewRunDetail}
        onViewRawLogs={onViewRawLogs}
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
                  detail={detail}
                  onViewRunDetail={onViewRunDetail}
                  onViewRawLogs={onViewRawLogs}
                  onToggleTaskAuto={onToggleTaskAuto}
                />
              ))}
            </div>
          )}
      </section>
    </section>
  );
}
