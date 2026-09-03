/**
 * 业务流程：共享组件承接跨页面交互模式，例如弹窗、选择条、任务入口、目录选择和全局搜索。
 * 数据边界：组件只应通过 props 表达业务意图，不直接假设当前页面或后端资源路径。
 * 维护风险：调整共享组件会同时影响资料库、系列详情、设置和阅读器，需要保留加载态、禁用态和可访问性。
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { CheckCircle2, ChevronDown, CircleAlert, Loader2, PauseCircle, X, XCircle } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';
import { getTaskMessage } from '../i18n/task';
import { isActiveTaskStatus, isTerminalTaskStatus } from '../utils/taskStatus';

export interface TaskBubbleEntry {
  key: string;
  type: string;
  status: string;
  message: string;
  message_code?: string;
  message_params?: Record<string, string>;
  error?: string;
  current: number;
  total: number;
  scope_name?: string;
  updatedAt: number;
}

interface TaskBubbleProps {
  tasks: TaskBubbleEntry[];
  onDismiss: (key: string) => void;
  onClearFinished: () => void;
}

/**
 * statusIcon 把任务状态画成一眼可辨的收尾方式：转圈只留给还在推进的任务，
 * 已暂停改画暂停符（它占着槽位但没在动），中断另给警示色——它不是失败，可重试。
 */
function statusIcon(status: string, size = 'h-3.5 w-3.5') {
  if (status === 'completed') return <CheckCircle2 className={`${size} text-emerald-400`} />;
  if (status === 'interrupted') return <CircleAlert className={`${size} text-amber-400`} />;
  if (isTerminalTaskStatus(status)) return <XCircle className={`${size} text-red-400`} />;
  if (status === 'paused') return <PauseCircle className={`${size} text-amber-400`} />;
  return <Loader2 className={`${size} text-komgaPrimary animate-spin`} />;
}

/**
 * 终态收口的严重度次序：失败最该被看见，「完成」排最后——它只是四种终态之一，
 * 混着失败时用它作结论等于把出错藏起来。
 */
const TERMINAL_SUMMARY_ORDER = ['failed', 'interrupted', 'cancelled', 'completed'] as const;

/**
 * summarizeStatus 给折叠条选一个代表状态：还有活动态任务就代表活动态（它们占着运行槽位，
 * 是用户在等的），否则按严重度从剩下的终态里挑一个；活动态里没有推进中的（全被暂停）时画暂停符。
 */
function summarizeStatus(tasks: TaskBubbleEntry[]) {
  const active = tasks.filter((task) => isActiveTaskStatus(task.status));
  if (active.length > 0) {
    const advancing = active.some((task) => task.status !== 'paused');
    return { kind: 'active' as const, status: advancing ? 'running' : 'paused', count: active.length };
  }
  for (const status of TERMINAL_SUMMARY_ORDER) {
    const count = tasks.filter((task) => task.status === status).length;
    if (count > 0) {
      return { kind: 'terminal' as const, status, count };
    }
  }
  return null;
}

/**
 * 业务注释：progressPercent 是前端共享组件层，负责复用跨页面交互、反馈、布局和选择状态的页面、组件或工具入口，负责把领域状态转换为用户可操作的界面行为。
 * 调整时应同时检查加载态、空态、错误态、主题适配和调用方传入的业务语义。
 */
function progressPercent(task: TaskBubbleEntry) {
  if (task.total > 0) return Math.min(100, Math.round((task.current / task.total) * 100));
  if (task.status === 'completed') return 100;
  return 0;
}

/**
 * 业务注释：SidebarTaskBubble 是前端共享组件层，负责复用跨页面交互、反馈、布局和选择状态的页面、组件或工具入口，负责把领域状态转换为用户可操作的界面行为。
 * 调整时应同时检查加载态、空态、错误态、主题适配和调用方传入的业务语义。
 */
export function SidebarTaskBubble({ tasks, onDismiss, onClearFinished }: TaskBubbleProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);

  const sorted = useMemo(() => {
    return [...tasks].sort((a, b) => {
      const ra = isActiveTaskStatus(a.status) ? 0 : 1;
      const rb = isActiveTaskStatus(b.status) ? 0 : 1;
      if (ra !== rb) return ra - rb;
      return b.updatedAt - a.updatedAt;
    });
  }, [tasks]);

  const summary = useMemo(() => summarizeStatus(sorted), [sorted]);
  const activeCount = summary?.kind === 'active' ? summary.count : 0;
  const finishedCount = sorted.length - sorted.filter((t) => isActiveTaskStatus(t.status)).length;
  const primary = sorted[0];

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (!(e.target instanceof Node)) return;
      if (containerRef.current && !containerRef.current.contains(e.target)) {
        setOpen(false);
      }
    };
    window.addEventListener('mousedown', handler);
    return () => window.removeEventListener('mousedown', handler);
  }, [open]);

  if (sorted.length === 0) return null;

  return (
    <div
      ref={containerRef}
      className="fixed bottom-4 left-4 z-[60] w-[300px] sm:w-[340px]"
    >
      {open && (
        <div className="mb-2 max-h-[60vh] overflow-y-auto rounded-2xl border border-gray-700 bg-gray-950/95 shadow-2xl backdrop-blur-sm">
          <header className="flex items-center justify-between border-b border-gray-800 px-3 py-2">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-gray-300">
              {t('taskBubble.title')}
            </h3>
            {finishedCount > 0 && (
              <button
                type="button"
                onClick={onClearFinished}
                className="text-[11px] text-gray-500 hover:text-white transition"
              >
                {t('taskBubble.clearFinished')}
              </button>
            )}
          </header>
          <ul className="divide-y divide-gray-800">
            {sorted.map((task) => {
              const percent = progressPercent(task);
              const finished = isTerminalTaskStatus(task.status);
              return (
                <li key={task.key} className="flex flex-col gap-1 px-3 py-2 hover:bg-gray-900/50">
                  <div className="flex items-center justify-between gap-2">
                    <Link
                      to={`/ops?tab=tasks&task=${encodeURIComponent(task.key)}`}
                      onClick={() => setOpen(false)}
                      className="flex min-w-0 items-center gap-2 text-xs text-gray-200 hover:text-komgaPrimary transition"
                    >
                      {statusIcon(task.status)}
                      <span className="truncate font-medium">{getTaskMessage(task, t)}</span>
                    </Link>
                    {finished && (
                      <button
                        type="button"
                        onClick={() => onDismiss(task.key)}
                        className="text-gray-600 hover:text-white transition"
                        aria-label={t('common.close')}
                      >
                        <X className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                  {task.scope_name && (
                    <p className="pl-5 text-[10px] text-gray-500 truncate">{task.scope_name}</p>
                  )}
                  <div className="pl-5 flex items-center gap-2">
                    <div className="flex-1 h-1 overflow-hidden rounded-full bg-gray-800">
                      <div
                        className={`h-full rounded-full transition-all duration-500 ease-out ${
                          task.status === 'failed'
                            ? 'bg-red-500'
                            : task.status === 'completed'
                            ? 'bg-emerald-500'
                            : 'bg-komgaPrimary'
                        }`}
                        style={{ width: `${percent}%` }}
                      />
                    </div>
                    <span className="text-[10px] font-mono text-gray-500 whitespace-nowrap">
                      {task.total > 0 ? `${task.current}/${task.total}` : t(`taskBubble.status.${task.status}`)}
                    </span>
                  </div>
                  {task.error && (
                    <p className="pl-5 text-[10px] text-red-300/90 line-clamp-2">{task.error}</p>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      )}

      <button
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        className="w-full flex items-center gap-3 rounded-2xl border border-gray-700 bg-gray-900/95 px-3 py-2.5 shadow-xl backdrop-blur-sm hover:border-komgaPrimary/40 transition"
      >
        <div className="relative shrink-0">
          {summary && statusIcon(summary.status, 'h-4 w-4')}
          {activeCount > 0 && (
            <span className="absolute -top-1 -right-1 min-w-[14px] h-[14px] rounded-full bg-komgaPrimary text-white text-[9px] font-bold flex items-center justify-center px-1">
              {activeCount}
            </span>
          )}
        </div>
        <div className="flex-1 min-w-0 text-left">
          <p className="text-[11px] font-semibold text-white truncate">
            {summary &&
              (summary.kind === 'active'
                ? t('taskBubble.summary.active', { count: summary.count })
                : t(`taskBubble.summary.${summary.status}`, { count: summary.count }))}
          </p>
          {primary && (
            <p className="text-[10px] text-gray-500 truncate">{getTaskMessage(primary, t)}</p>
          )}
        </div>
        <ChevronDown className={`h-3.5 w-3.5 text-gray-500 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
    </div>
  );
}
