import { useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { CheckCircle2, ChevronDown, Loader2, X, XCircle } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';

export interface TaskBubbleEntry {
  key: string;
  type: string;
  status: string;
  message: string;
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

function statusIcon(status: string) {
  if (status === 'completed') return <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />;
  if (status === 'failed' || status === 'canceled') return <XCircle className="h-3.5 w-3.5 text-red-400" />;
  return <Loader2 className="h-3.5 w-3.5 text-komgaPrimary animate-spin" />;
}

function progressPercent(task: TaskBubbleEntry) {
  if (task.total > 0) return Math.min(100, Math.round((task.current / task.total) * 100));
  if (task.status === 'completed') return 100;
  return 0;
}

export function SidebarTaskBubble({ tasks, onDismiss, onClearFinished }: TaskBubbleProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);

  const sorted = useMemo(() => {
    return [...tasks].sort((a, b) => {
      const ra = a.status === 'running' || a.status === 'paused' ? 0 : 1;
      const rb = b.status === 'running' || b.status === 'paused' ? 0 : 1;
      if (ra !== rb) return ra - rb;
      return b.updatedAt - a.updatedAt;
    });
  }, [tasks]);

  const runningCount = sorted.filter((t) => t.status === 'running' || t.status === 'paused').length;
  const finishedCount = sorted.length - runningCount;
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
      className="fixed bottom-4 left-4 z-40 w-[300px] sm:w-[340px]"
    >
      {open && (
        <div className="mb-2 max-h-[60vh] overflow-y-auto rounded-2xl border border-gray-700 bg-gray-950/95 shadow-2xl backdrop-blur">
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
              const finished = task.status === 'completed' || task.status === 'failed' || task.status === 'canceled';
              return (
                <li key={task.key} className="flex flex-col gap-1 px-3 py-2 hover:bg-gray-900/50">
                  <div className="flex items-center justify-between gap-2">
                    <Link
                      to={`/ops?tab=tasks&task=${encodeURIComponent(task.key)}`}
                      onClick={() => setOpen(false)}
                      className="flex min-w-0 items-center gap-2 text-xs text-gray-200 hover:text-komgaPrimary transition"
                    >
                      {statusIcon(task.status)}
                      <span className="truncate font-medium">{task.message || task.type}</span>
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
        className="w-full flex items-center gap-3 rounded-2xl border border-gray-700 bg-gray-900/95 px-3 py-2.5 shadow-xl backdrop-blur hover:border-komgaPrimary/40 transition"
      >
        <div className="relative shrink-0">
          {runningCount > 0 ? (
            <Loader2 className="h-4 w-4 text-komgaPrimary animate-spin" />
          ) : (
            <CheckCircle2 className="h-4 w-4 text-emerald-400" />
          )}
          {runningCount > 0 && (
            <span className="absolute -top-1 -right-1 min-w-[14px] h-[14px] rounded-full bg-komgaPrimary text-white text-[9px] font-bold flex items-center justify-center px-1">
              {runningCount}
            </span>
          )}
        </div>
        <div className="flex-1 min-w-0 text-left">
          <p className="text-[11px] font-semibold text-white truncate">
            {runningCount > 0
              ? t('taskBubble.running', { count: runningCount })
              : t('taskBubble.allDone')}
          </p>
          {primary && (
            <p className="text-[10px] text-gray-500 truncate">{primary.message || primary.type}</p>
          )}
        </div>
        <ChevronDown className={`h-3.5 w-3.5 text-gray-500 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
    </div>
  );
}
