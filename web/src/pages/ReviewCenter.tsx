import { useState, useEffect, lazy, Suspense } from 'react';
import { useSearchParams } from 'react-router-dom';
import { GitCompareArrows, Layers3, Loader2, ShieldCheck } from 'lucide-react';
import { PageShell, PageHeader } from '../components/PageShell';
import { useI18n } from '../i18n/LocaleProvider';

const MetadataReviews = lazy(() => import('./MetadataReviews'));
const AIGroupingReviews = lazy(() => import('./AIGroupingReviews'));

type TabKey = 'metadata' | 'ai-grouping';

export default function ReviewCenter() {
  const { t } = useI18n();
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = (searchParams.get('tab') as TabKey) || 'metadata';
  const [metadataCount, setMetadataCount] = useState<number>(0);
  const [aiGroupingCount, setAiGroupingCount] = useState<number>(0);

  const setTab = (tab: TabKey) => {
    setSearchParams({ tab }, { replace: true });
  };

  // Fetch pending counts
  useEffect(() => {
    fetch('/api/metadata/reviews?limit=1&page=1')
      .then((res) => res.json())
      .then((data) => setMetadataCount(data.total ?? 0))
      .catch(() => setMetadataCount(0));

    fetch('/api/ai-grouping/reviews?status=pending&limit=1&page=1')
      .then((res) => res.json())
      .then((data) => setAiGroupingCount(data.total ?? 0))
      .catch(() => setAiGroupingCount(0));
  }, []);

  const tabs: { key: TabKey; label: string; icon: typeof GitCompareArrows; count: number }[] = [
    { key: 'metadata', label: t('reviewCenter.tab.metadata'), icon: GitCompareArrows, count: metadataCount },
    { key: 'ai-grouping', label: t('reviewCenter.tab.aiGrouping'), icon: Layers3, count: aiGroupingCount },
  ];

  return (
    <PageShell maxWidth="full">
      <PageHeader
        badge={{ icon: <ShieldCheck className="h-3.5 w-3.5" />, label: t('reviewCenter.badge') }}
        title={t('reviewCenter.title')}
        description={t('reviewCenter.description')}
      />

      {/* Tab bar */}
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
              {tab.count > 0 && (
                <span
                  className={`rounded-full px-2 py-0.5 text-[11px] font-semibold ${
                    isActive
                      ? 'bg-white/20 text-white'
                      : 'bg-gray-800 text-gray-300'
                  }`}
                >
                  {tab.count}
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Tab content */}
      <Suspense
        fallback={
          <div className="flex min-h-[40vh] items-center justify-center">
            <Loader2 className="h-8 w-8 animate-spin text-komgaPrimary" />
          </div>
        }
      >
        {activeTab === 'metadata' ? <MetadataReviews embedded /> : <AIGroupingReviews embedded />}
      </Suspense>
    </PageShell>
  );
}
