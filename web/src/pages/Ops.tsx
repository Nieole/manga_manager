import { Suspense, lazy, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Activity, Loader2, Terminal } from 'lucide-react';
import { PageShell, PageHeader } from '../components/PageShell';
import { useI18n } from '../i18n/LocaleProvider';
import type { TaskStatus } from '../components/tasks/TaskCenter';

const BackgroundTasks = lazy(() => import('./BackgroundTasks'));
const Logs = lazy(() => import('./Logs'));

type TabKey = 'tasks' | 'logs';

const VALID_TABS: TabKey[] = ['tasks', 'logs'];

export default function Ops() {
  const { t } = useI18n();
  const [searchParams, setSearchParams] = useSearchParams();
  const initialTab = searchParams.get('tab');
  const [activeTab, setActiveTab] = useState<TabKey>(
    VALID_TABS.includes(initialTab as TabKey) ? (initialTab as TabKey) : 'tasks',
  );
  const [taskKey, setTaskKey] = useState<string>(() => searchParams.get('task_key') || '');

  useEffect(() => {
    const fromUrl = searchParams.get('tab');
    if (fromUrl && VALID_TABS.includes(fromUrl as TabKey) && fromUrl !== activeTab) {
      setActiveTab(fromUrl as TabKey);
    }
    const taskKeyFromUrl = searchParams.get('task_key') || '';
    if (taskKeyFromUrl !== taskKey) {
      setTaskKey(taskKeyFromUrl);
    }
  }, [searchParams, activeTab, taskKey]);

  const setTab = (tab: TabKey) => {
    setActiveTab(tab);
    const next = new URLSearchParams(searchParams);
    next.set('tab', tab);
    setSearchParams(next, { replace: true });
  };

  const viewTaskLogs = (task: TaskStatus) => {
    const next = new URLSearchParams(searchParams);
    next.set('tab', 'logs');
    next.set('task_key', task.key);
    setActiveTab('logs');
    setTaskKey(task.key);
    setSearchParams(next, { replace: true });
  };

  const clearTaskKey = () => {
    setTaskKey('');
    const next = new URLSearchParams(searchParams);
    next.delete('task_key');
    setSearchParams(next, { replace: true });
  };

  const tabs: { key: TabKey; label: string; icon: typeof Activity }[] = [
    { key: 'tasks', label: t('ops.tab.tasks'), icon: Activity },
    { key: 'logs', label: t('ops.tab.logs'), icon: Terminal },
  ];

  return (
    <PageShell maxWidth="full">
      <PageHeader
        badge={{ icon: <Activity className="h-3.5 w-3.5" />, label: t('ops.badge') }}
        title={t('ops.title')}
        description={t('ops.description')}
      />

      <div className="flex gap-1 rounded-xl border border-gray-800 bg-gray-950/60 p-1">
        {tabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.key;
          return (
            <button
              key={tab.key}
              onClick={() => setTab(tab.key)}
              className={`flex items-center gap-2 rounded-lg px-4 py-2.5 text-sm font-medium transition-all ${
                isActive
                  ? 'bg-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                  : 'text-gray-400 hover:bg-gray-800/50 hover:text-white'
              }`}
            >
              <Icon className="h-4 w-4" />
              {tab.label}
            </button>
          );
        })}
      </div>

      <Suspense
        fallback={
          <div className="flex min-h-[40vh] items-center justify-center">
            <Loader2 className="h-8 w-8 animate-spin text-komgaPrimary" />
          </div>
        }
      >
        {activeTab === 'tasks'
          ? <BackgroundTasks embedded onViewTaskLogs={viewTaskLogs} />
          : <Logs embedded taskKey={taskKey || undefined} onClearTaskKey={taskKey ? clearTaskKey : undefined} />}
      </Suspense>
    </PageShell>
  );
}
