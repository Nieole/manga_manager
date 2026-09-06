import { Suspense, lazy, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Activity, Loader2, Terminal } from 'lucide-react';
import { PageShell, PageHeader } from '../components/PageShell';
import { useI18n } from '../i18n/LocaleProvider';
import type { RunStatus } from '../components/tasks/TaskCenter';

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
  // 原始日志按**运行**过滤，不再按**任务键**：同一个库连着扫三次共用一个键，按键过滤把三次
  // 混在一起，而排障要的恰恰是其中一次。
  const [runID, setRunID] = useState<string>(() => searchParams.get('run_id') || '');

  useEffect(() => {
    const fromUrl = searchParams.get('tab');
    if (fromUrl && VALID_TABS.includes(fromUrl as TabKey) && fromUrl !== activeTab) {
      setActiveTab(fromUrl as TabKey);
    }
    const runIDFromUrl = searchParams.get('run_id') || '';
    if (runIDFromUrl !== runID) {
      setRunID(runIDFromUrl);
    }
  }, [searchParams, activeTab, runID]);

  const setTab = (tab: TabKey) => {
    setActiveTab(tab);
    const next = new URLSearchParams(searchParams);
    next.set('tab', tab);
    setSearchParams(next, { replace: true });
  };

  const viewRawLogs = (run: RunStatus) => {
    const next = new URLSearchParams(searchParams);
    next.set('tab', 'logs');
    next.set('run_id', String(run.run_id));
    setActiveTab('logs');
    setRunID(String(run.run_id));
    setSearchParams(next, { replace: true });
  };

  const clearRunID = () => {
    setRunID('');
    const next = new URLSearchParams(searchParams);
    next.delete('run_id');
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
          ? <BackgroundTasks embedded onViewRawLogs={viewRawLogs} />
          : <Logs embedded runID={runID || undefined} onClearRunID={runID ? clearRunID : undefined} />}
      </Suspense>
    </PageShell>
  );
}
