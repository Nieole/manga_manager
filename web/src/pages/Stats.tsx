/**
 * 业务说明：本文件是独立的「统计」页（第 6 项），按当前用户展示深度阅读统计：
 * 连续阅读天数、累计阅读时长、年度/月度回顾（页数/活跃天数/时长/读完本数/最多阅读系列），以及每本阅读时长排行。
 * 数据来自每用户统计端点（/api/stats/streak|reading-time|period），未登录时后端返回空值。
 * 维护要点：与仪表盘概览互补——仪表盘留结构性概览与热力图，本页聚焦个人回顾。
 */

import { useEffect, useMemo, useState } from 'react';
import { Flame, Clock, CalendarRange, BookCheck, Trophy, Loader2, BarChart3 } from 'lucide-react';
import { apiClient, getApiErrorMessage } from '../api/client';
import { useI18n } from '../i18n/LocaleProvider';
import { useToast } from '../components/ToastProvider';

interface StreakResponse { current: number; longest: number }
interface BookReadingTime { book_id: number; book_name: string; book_title: string; series_id: number; series_name: string; total_seconds: number }
interface ReadingTimeResponse { total_seconds: number; top: BookReadingTime[] }
interface PeriodTopSeries { series_id: number; series_name: string; pages: number }
interface PeriodResponse { pages: number; read_seconds: number; active_days: number; books_touched: number; books_completed: number; top_series: PeriodTopSeries[] }

const cardClass = 'bg-komgaSurface border border-gray-800 rounded-2xl p-5';

export default function Stats() {
  const { t, formatNumber } = useI18n();
  const { showToast } = useToast();

  const [streak, setStreak] = useState<StreakResponse>({ current: 0, longest: 0 });
  const [readingTime, setReadingTime] = useState<ReadingTimeResponse>({ total_seconds: 0, top: [] });
  const [loading, setLoading] = useState(true);

  const currentYear = new Date().getFullYear();
  const [year, setYear] = useState(currentYear);
  const [month, setMonth] = useState(0); // 0 = 全年
  const [period, setPeriod] = useState<PeriodResponse | null>(null);
  const [periodLoading, setPeriodLoading] = useState(true);

  const formatDuration = (seconds: number): string => {
    // 先算总分钟再拆时/分，避免分钟分量四舍五入到 60（否则会显示 "1h 60m"）。
    const totalMinutes = Math.max(0, Math.round((seconds || 0) / 60));
    if (totalMinutes < 60) return t('stats.duration.minutes', { value: totalMinutes });
    return t('stats.duration.hoursMinutes', { hours: Math.floor(totalMinutes / 60), minutes: totalMinutes % 60 });
  };

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    Promise.all([
      apiClient.get<StreakResponse>('/api/stats/streak').then((r) => r.data).catch(() => ({ current: 0, longest: 0 })),
      apiClient.get<ReadingTimeResponse>('/api/stats/reading-time?limit=10').then((r) => r.data).catch(() => ({ total_seconds: 0, top: [] })),
    ]).then(([s, rt]) => {
      if (cancelled) return;
      setStreak(s);
      setReadingTime(rt);
    }).catch((err) => {
      if (!cancelled) showToast(getApiErrorMessage(err, t('stats.loadFailed')), 'error');
    }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [t, showToast]);

  useEffect(() => {
    let cancelled = false;
    setPeriodLoading(true);
    const q = month > 0 ? `year=${year}&month=${month}` : `year=${year}`;
    apiClient.get<PeriodResponse>(`/api/stats/period?${q}`)
      .then((r) => { if (!cancelled) setPeriod(r.data); })
      .catch((err) => { if (!cancelled) showToast(getApiErrorMessage(err, t('stats.loadFailed')), 'error'); })
      .finally(() => { if (!cancelled) setPeriodLoading(false); });
    return () => { cancelled = true; };
  }, [year, month, t, showToast]);

  const years = useMemo(() => Array.from({ length: 6 }, (_, i) => currentYear - i), [currentYear]);
  const maxTop = useMemo(() => Math.max(1, ...readingTime.top.map((b) => b.total_seconds)), [readingTime.top]);

  if (loading) {
    return (
      <div className="flex min-h-[50vh] items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-komgaPrimary" />
      </div>
    );
  }

  return (
    <div className="p-4 sm:p-8 max-w-5xl mx-auto space-y-8">
      <div className="flex items-center gap-2">
        <BarChart3 className="h-6 w-6 text-komgaPrimary" />
        <h1 className="text-2xl font-bold text-white">{t('stats.title')}</h1>
      </div>

      {/* 概览卡：连续天数 + 累计时长 */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className={cardClass}>
          <div className="flex items-center gap-2 text-orange-400"><Flame className="h-4 w-4" /><span className="text-sm text-gray-400">{t('stats.streak.current')}</span></div>
          <div className="mt-2 text-3xl font-bold text-white">{t('stats.streak.days', { count: streak.current })}</div>
          <div className="mt-1 text-xs text-gray-500">{t('stats.streak.longest', { count: streak.longest })}</div>
        </div>
        <div className={cardClass}>
          <div className="flex items-center gap-2 text-sky-400"><Clock className="h-4 w-4" /><span className="text-sm text-gray-400">{t('stats.readingTime.total')}</span></div>
          <div className="mt-2 text-3xl font-bold text-white">{formatDuration(readingTime.total_seconds)}</div>
          <div className="mt-1 text-xs text-gray-500">{t('stats.readingTime.hint')}</div>
        </div>
        <div className={cardClass}>
          <div className="flex items-center gap-2 text-emerald-400"><BookCheck className="h-4 w-4" /><span className="text-sm text-gray-400">{t('stats.period.booksCompleted')}</span></div>
          <div className="mt-2 text-3xl font-bold text-white">{period ? formatNumber(period.books_completed) : '—'}</div>
          <div className="mt-1 text-xs text-gray-500">{month > 0 ? t('stats.period.thisMonth') : t('stats.period.thisYear')}</div>
        </div>
      </div>

      {/* 年度/月度回顾 */}
      <div className={cardClass}>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <CalendarRange className="h-5 w-5 text-komgaPrimary" />
            <h2 className="text-lg font-semibold text-white">{t('stats.review.title')}</h2>
          </div>
          <div className="flex items-center gap-2">
            <select value={year} onChange={(e) => setYear(Number(e.target.value))} className="rounded-lg border border-gray-800 bg-gray-900 px-3 py-1.5 text-sm text-white">
              {years.map((y) => <option key={y} value={y}>{t('stats.review.year', { year: y })}</option>)}
            </select>
            <select value={month} onChange={(e) => setMonth(Number(e.target.value))} className="rounded-lg border border-gray-800 bg-gray-900 px-3 py-1.5 text-sm text-white">
              <option value={0}>{t('stats.review.wholeYear')}</option>
              {Array.from({ length: 12 }, (_, i) => i + 1).map((m) => <option key={m} value={m}>{t('stats.review.month', { month: m })}</option>)}
            </select>
          </div>
        </div>

        {periodLoading || !period ? (
          <div className="flex justify-center py-8"><Loader2 className="h-5 w-5 animate-spin text-komgaPrimary" /></div>
        ) : (
          <>
            <div className="mt-4 grid grid-cols-2 sm:grid-cols-4 gap-3">
              <ReviewStat label={t('stats.review.pages')} value={formatNumber(period.pages)} />
              <ReviewStat label={t('stats.review.readingTime')} value={formatDuration(period.read_seconds)} />
              <ReviewStat label={t('stats.review.activeDays')} value={formatNumber(period.active_days)} />
              <ReviewStat label={t('stats.review.booksTouched')} value={formatNumber(period.books_touched)} />
            </div>
            {period.top_series.length > 0 && (
              <div className="mt-5">
                <div className="text-sm font-medium text-gray-300 mb-2">{t('stats.review.topSeries')}</div>
                <ol className="space-y-1.5">
                  {period.top_series.map((s, i) => (
                    <li key={s.series_id} className="flex items-center gap-2 text-sm">
                      <span className="w-5 text-gray-500">{i + 1}.</span>
                      <span className="flex-1 truncate text-gray-200">{s.series_name}</span>
                      <span className="text-gray-500">{t('stats.review.pagesCount', { count: s.pages })}</span>
                    </li>
                  ))}
                </ol>
              </div>
            )}
            {period.pages === 0 && period.read_seconds === 0 && (
              <p className="mt-4 text-center text-sm text-gray-500">{t('stats.review.empty')}</p>
            )}
          </>
        )}
      </div>

      {/* 每本阅读时长排行 */}
      <div className={cardClass}>
        <div className="flex items-center gap-2 mb-3">
          <Trophy className="h-5 w-5 text-amber-400" />
          <h2 className="text-lg font-semibold text-white">{t('stats.readingTime.topTitle')}</h2>
        </div>
        {readingTime.top.length === 0 ? (
          <p className="py-6 text-center text-sm text-gray-500">{t('stats.readingTime.empty')}</p>
        ) : (
          <ul className="space-y-2.5">
            {readingTime.top.map((b) => (
              <li key={b.book_id}>
                <div className="flex items-center justify-between gap-3 text-sm">
                  <span className="truncate text-gray-200">
                    <span className="text-gray-500">{b.series_name} · </span>{b.book_title || b.book_name}
                  </span>
                  <span className="shrink-0 text-gray-400">{formatDuration(b.total_seconds)}</span>
                </div>
                <div className="mt-1 h-1.5 rounded-full bg-gray-800 overflow-hidden">
                  <div className="h-full rounded-full bg-komgaPrimary" style={{ width: `${Math.max(4, (b.total_seconds / maxTop) * 100)}%` }} />
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function ReviewStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-gray-800 bg-gray-900/40 p-3">
      <div className="text-xs text-gray-500">{label}</div>
      <div className="mt-1 text-xl font-semibold text-white">{value}</div>
    </div>
  );
}
