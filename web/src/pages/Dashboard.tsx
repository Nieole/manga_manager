/**
 * 业务说明：本文件是业务实现，属于项目源码的一部分，负责支撑漫画管理器在资料库、阅读器、扫描、元数据或系统设置中的具体业务能力。
 * 它与相邻模块共同组成前后端业务链路，修改时需要结合调用方理解数据流和用户可见行为。
 * 维护时应关注输入输出契约、错误处理、状态同步和与既有业务语义的一致性。
 */

import { useState, useEffect, useRef, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import axios from 'axios';
import { BookOpen, Library, Eye, FileText, TrendingUp, ChevronLeft, ChevronRight, Sparkles, RefreshCcw, FolderPlus, Settings as SettingsIcon, ClipboardCheck, ArrowRight } from 'lucide-react';
import { useI18n } from '../i18n/LocaleProvider';

interface LibraryOverview {
    id: number;
    name: string;
}

interface LibrarySize {
    library_id: number;
    library_name: string;
    total_size: number;
}

interface DashboardStats {
    total_series: number;
    total_books: number;
    read_books: number;
    total_pages: number;
    active_days_7: number;
    library_sizes: LibrarySize[];
}

interface ActivityDay {
    date: string;
    page_count: number;
}

interface RecentReadItem {
    series_id: number;
    series_name: string;
    book_id: number;
    book_name: string;
    book_title: { String: string; Valid: boolean } | null;
    cover_path: string;
    last_read_page: { Int64: number; Valid: boolean } | null;
    last_read_at: { Time: string; Valid: boolean } | null;
    page_count: number;
    is_sequel_suggestion?: boolean;
    relation_type?: string;
    source_series_name?: string;
}

interface RecommendedItem {
    series_id: number;
    title: string;
    cover_path: string;
    reason: string;
}

export default function Dashboard() {
    const { t, formatNumber } = useI18n();
    const [stats, setStats] = useState<DashboardStats | null>(null);
    const [libraries, setLibraries] = useState<LibraryOverview[]>([]);
    const [recentReads, setRecentReads] = useState<RecentReadItem[]>([]);
    const [recommendations, setRecommendations] = useState<RecommendedItem[]>([]);
    const [heatmapData, setHeatmapData] = useState<ActivityDay[]>([]);
    const [reviewPending, setReviewPending] = useState(0);
    const [loading, setLoading] = useState(true);
    const [recommendationsRequested, setRecommendationsRequested] = useState(false);
    const [recsLoading, setRecsLoading] = useState(false);
    const [heatmapWeeks, setHeatmapWeeks] = useState(52);
    const scrollRef = useRef<HTMLDivElement>(null);
    const recommendationsRef = useRef<HTMLDivElement>(null);
    const navigate = useNavigate();

    useEffect(() => {
        let active = true;
        Promise.all([
            // 每个请求各自兜底，避免任一失败 reject 整个 Promise.all：
            // 否则 dashboard 统计接口一失败，libraries 就不会被设置，UI 会误显示“没有资源库”的新手引导页。
            axios.get('/api/stats/dashboard').catch(() => ({ data: null })),
            axios.get('/api/libraries').catch(() => ({ data: [] })),
            axios.get('/api/stats/recent-read?limit=20').catch(() => ({ data: [] })),
            axios.get('/api/reviews/inbox/summary').catch(() => ({ data: { counts: { total: 0 } } })),
        ]).then(([statsRes, librariesRes, recentRes, reviewSummaryRes]) => {
            if (!active) return;
            if (statsRes.data) setStats(statsRes.data);
            setLibraries(Array.isArray(librariesRes.data) ? librariesRes.data : []);
            setRecentReads(Array.isArray(recentRes.data) ? recentRes.data : []);
            setReviewPending(reviewSummaryRes?.data?.counts?.total ?? 0);
        }).catch(console.error).finally(() => {
            if (active) setLoading(false);
        });
        return () => {
            active = false;
        };
    }, []);

    useEffect(() => {
        let active = true;
        axios.get(`/api/stats/activity-heatmap?weeks=${heatmapWeeks}`)
            .then(res => {
                if (!active) return;
                setHeatmapData(Array.isArray(res.data) ? res.data : []);
            })
            .catch(console.error);
        return () => {
            active = false;
        };
    }, [heatmapWeeks]);

    const loadRecommendations = useCallback((forceRefresh = false) => {
        if (recsLoading) return;
        setRecommendationsRequested(true);
        setRecsLoading(true);
        const url = forceRefresh ? '/api/stats/recommendations?limit=10&refresh=true' : '/api/stats/recommendations?limit=10';
        axios.get(url)
            .then(res => setRecommendations(Array.isArray(res.data) ? res.data : []))
            .catch(console.error)
            .finally(() => setRecsLoading(false));
    }, [recsLoading]);

    useEffect(() => {
        if (loading || recommendationsRequested) return;
        const target = recommendationsRef.current;
        if (!target) return;
        const observer = new IntersectionObserver((entries) => {
            if (entries.some((entry) => entry.isIntersecting)) {
                loadRecommendations(false);
            }
        }, { rootMargin: '320px 0px' });
        observer.observe(target);
        return () => observer.disconnect();
    }, [loadRecommendations, loading, recommendationsRequested]);

    const scrollCarousel = (dir: 'left' | 'right') => {
        if (!scrollRef.current) return;
        const amount = 300;
        scrollRef.current.scrollBy({ left: dir === 'left' ? -amount : amount, behavior: 'smooth' });
    };

    const readPercent = stats ? (stats.total_books > 0 ? Math.round((stats.read_books / stats.total_books) * 100) : 0) : 0;

    if (loading) {
        return (
            <div className="flex items-center justify-center h-full min-h-[60vh]">
                <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-komgaPrimary"></div>
            </div>
        );
    }

    if (libraries.length === 0) {
        return (
            <div className="p-4 sm:p-8 max-w-5xl mx-auto">
                <div className="rounded-[28px] border border-gray-800 bg-linear-to-br from-komgaSurface to-gray-950 p-8 sm:p-10 shadow-2xl">
                    <div className="max-w-3xl space-y-6">
                        <div className="inline-flex items-center gap-2 rounded-full border border-komgaPrimary/30 bg-komgaPrimary/10 px-3 py-1 text-sm text-komgaPrimary">
                            <Sparkles className="w-4 h-4" />
                            {t('dashboard.onboarding.badge')}
                        </div>
                        <div>
                            <h1 className="text-3xl sm:text-4xl font-bold text-white tracking-tight">{t('dashboard.onboarding.title')}</h1>
                            <p className="mt-3 text-gray-400 leading-7">
                                {t('dashboard.onboarding.description')}
                            </p>
                        </div>

                        <div className="grid gap-4 md:grid-cols-3">
                            <OnboardingCard
                                title={t('dashboard.onboarding.step1.title')}
                                description={t('dashboard.onboarding.step1.description')}
                                actionLabel={t('dashboard.onboarding.step1.action')}
                                onClick={() => window.dispatchEvent(new Event('manga-manager:open-add-library'))}
                                icon={<FolderPlus className="w-5 h-5 text-komgaPrimary" />}
                            />
                            <OnboardingCard
                                title={t('dashboard.onboarding.step2.title')}
                                description={t('dashboard.onboarding.step2.description')}
                                actionLabel={t('dashboard.onboarding.step2.action')}
                                onClick={() => navigate('/settings')}
                                icon={<SettingsIcon className="w-5 h-5 text-amber-400" />}
                            />
                            <OnboardingCard
                                title={t('dashboard.onboarding.step3.title')}
                                description={t('dashboard.onboarding.step3.description')}
                                actionLabel={t('dashboard.onboarding.step3.action')}
                                onClick={() => navigate('/settings')}
                                icon={<Library className="w-5 h-5 text-blue-400" />}
                            />
                        </div>
                    </div>
                </div>
            </div>
        );
    }

    return (
        <div className="p-4 sm:p-8 max-w-6xl mx-auto space-y-8">
            {/* 标题 */}
            <div className="flex items-center gap-3">
                <TrendingUp className="w-7 h-7 text-komgaPrimary" />
                <h1 className="text-2xl font-bold text-white tracking-tight">{t('dashboard.title')}</h1>
            </div>

            {/* 待审核提示行（仅有挂起项时显示） */}
            {reviewPending > 0 && (
                <button
                    type="button"
                    onClick={() => navigate('/reviews')}
                    className="flex items-center gap-3 w-full sm:w-auto px-4 py-3 rounded-xl border border-amber-500/30 bg-amber-500/10 text-amber-200 hover:bg-amber-500/15 transition-colors"
                >
                    <ClipboardCheck className="w-4 h-4" />
                    <span className="text-sm">{t('dashboard.reviewPending.label', { count: reviewPending })}</span>
                    <ArrowRight className="w-3.5 h-3.5 ml-auto" />
                </button>
            )}

            {/* 继续阅读 Hero —— 提至首屏 */}
            {recentReads.length > 0 && (
                <div>
                    <div className="flex items-center justify-between mb-4">
                        <h2 className="text-lg font-semibold text-white flex items-center gap-2">
                            <BookOpen className="w-5 h-5 text-komgaPrimary" />
                            {t('dashboard.continueReading.title')}
                        </h2>
                        <div className="flex gap-2">
                            <button onClick={() => scrollCarousel('left')} className="p-1.5 rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-400 hover:text-white transition-colors">
                                <ChevronLeft className="w-4 h-4" />
                            </button>
                            <button onClick={() => scrollCarousel('right')} className="p-1.5 rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-400 hover:text-white transition-colors">
                                <ChevronRight className="w-4 h-4" />
                            </button>
                        </div>
                    </div>

                    <div
                        ref={scrollRef}
                        className="flex gap-4 overflow-x-auto scroll-smooth pb-2 snap-x snap-mandatory"
                        style={{ scrollbarWidth: 'none' }}
                    >
                        {recentReads.map((item) => {
                            const isSequel = item.is_sequel_suggestion;
                            const progress = !isSequel && item.page_count > 0 && item.last_read_page?.Valid
                                ? Math.round((item.last_read_page.Int64 / item.page_count) * 100) : 0;
                            const coverUrl = item.cover_path ? `/api/thumbnails/${item.cover_path}` : '';
                            const keyStr = isSequel ? `sequel-${item.series_id}` : `${item.series_id}-${item.book_id}`;

                            return (
                                <div
                                    key={keyStr}
                                    onClick={() => navigate(isSequel ? `/series/${item.series_id}` : `/reader/${item.book_id}`)}
                                    className="group shrink-0 w-40 snap-start cursor-pointer"
                                >
                                    <div className={`relative aspect-2/3 rounded-xl overflow-hidden bg-gray-900 border border-gray-800 transition-all duration-300 shadow-lg ${isSequel ? 'group-hover:border-purple-500/50 group-hover:shadow-purple-500/10' : 'group-hover:border-komgaPrimary/50 group-hover:shadow-komgaPrimary/10'}`}>
                                        {coverUrl ? (
                                            <img
                                                src={coverUrl}
                                                alt={item.series_name}
                                                className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500"
                                            />
                                        ) : (
                                            <div className="w-full h-full flex items-center justify-center text-gray-700">
                                                <BookOpen className="w-10 h-10" />
                                            </div>
                                        )}

                                        {isSequel && (
                                            <div className="absolute top-2 right-2 bg-purple-600 text-white text-[10px] font-bold px-1.5 py-0.5 rounded shadow-sm uppercase tracking-wide">
                                                {t(`series.relations.type.${item.relation_type}`, undefined, item.relation_type)}
                                            </div>
                                        )}

                                        {!isSequel && (
                                            <div className="absolute bottom-0 inset-x-0 h-1 bg-gray-900/80">
                                                <div className="h-full bg-komgaPrimary transition-all" style={{ width: `${progress}%` }} />
                                            </div>
                                        )}

                                        <div className="absolute inset-0 bg-linear-to-t from-black/80 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300 flex items-end p-3">
                                            <span className="text-xs text-white font-medium">
                                                {isSequel 
                                                    ? t('series.continue.start', { book: item.series_name })
                                                    : t('dashboard.continueReading.resumeToPage', { page: item.last_read_page?.Valid ? item.last_read_page.Int64 : 1 })
                                                }
                                            </span>
                                        </div>
                                    </div>

                                    <div className="mt-2 px-1">
                                        <p className="text-sm font-medium text-gray-200 truncate group-hover:text-komgaPrimary transition-colors">{item.series_name}</p>
                                        <p className="text-xs text-gray-500 truncate mt-0.5">
                                            {isSequel 
                                                ? t('series.franchise.description')
                                                : (item.book_title?.Valid ? item.book_title.String : item.book_name)
                                            }
                                        </p>
                                        {!isSequel && <p className="text-[10px] text-gray-600 mt-1">{t('dashboard.continueReading.readPercent', { percent: progress })}</p>}
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                </div>
            )}

            {/* 统计卡片网格 */}
            {stats && (
                <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
                    <StatCard
                        icon={<Library className="w-5 h-5" />}
                        label={t('dashboard.stats.series')}
                        value={stats.total_series}
                        color="from-purple-500/20 to-purple-600/5"
                        borderColor="border-purple-500/30"
                        iconColor="text-purple-400"
                    />
                    <StatCard
                        icon={<BookOpen className="w-5 h-5" />}
                        label={t('dashboard.stats.books')}
                        value={stats.total_books}
                        color="from-blue-500/20 to-blue-600/5"
                        borderColor="border-blue-500/30"
                        iconColor="text-blue-400"
                    />
                    <StatCard
                        icon={<Eye className="w-5 h-5" />}
                        label={t('dashboard.stats.read')}
                        value={t('dashboard.stats.booksReadValue', { count: stats.read_books })}
                        subtitle={t('dashboard.stats.readRate', { percent: readPercent })}
                        color="from-emerald-500/20 to-emerald-600/5"
                        borderColor="border-emerald-500/30"
                        iconColor="text-emerald-400"
                    />
                    <StatCard
                        icon={<FileText className="w-5 h-5" />}
                        label={t('dashboard.stats.pages')}
                        value={formatNumber(stats.total_pages)}
                        subtitle={t('dashboard.stats.activeDays7', { count: stats.active_days_7 })}
                        color="from-amber-500/20 to-amber-600/5"
                        borderColor="border-amber-500/30"
                        iconColor="text-amber-400"
                    />
                </div>
            )}



            {/* 阅读进度、存储空间与热力图 */}
            {stats && (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                    {/* 完读进度环 */}
                    <div className="bg-komgaSurface border border-gray-800 rounded-2xl p-6 flex items-center gap-6 h-full">
                        <div className="relative w-28 h-28 shrink-0">
                            <svg viewBox="0 0 36 36" className="w-full h-full -rotate-90 drop-shadow-xl">
                                <circle cx="18" cy="18" r="15.5" fill="none" stroke="currentColor" strokeWidth="2.5" className="text-gray-800" />
                                <circle cx="18" cy="18" r="15.5" fill="none" stroke="currentColor" strokeWidth="2.5"
                                    strokeDasharray={`${readPercent} ${100 - readPercent}`}
                                    strokeLinecap="round"
                                    className="text-komgaPrimary transition-all duration-1000 ease-out"
                                />
                            </svg>
                            <div className="absolute inset-0 flex items-center justify-center">
                                <span className="text-2xl font-bold text-white">{readPercent}%</span>
                            </div>
                        </div>
                        <div>
                            <h3 className="text-lg font-semibold text-white mb-1">{t('dashboard.readingProgress.title')}</h3>
                            <p className="text-sm text-gray-400">
                                {t('dashboard.readingProgress.summary', { read: stats.read_books, total: stats.total_books })}
                            </p>
                            <p className="text-xs text-gray-500 mt-2">
                                {t('dashboard.readingProgress.pages', { pages: formatNumber(stats.total_pages) })}
                            </p>
                        </div>
                    </div>

                    {/* 物理存储占用图 */}
                    <StoragePieChart librarySizes={stats.library_sizes} />

                    {/* GitHub 风格活跃热力图 */}
                    <div className="lg:col-span-2">
                        <ActivityHeatmap data={heatmapData} activeDays7={stats.active_days_7} weeks={heatmapWeeks} onChangeWeeks={setHeatmapWeeks} />
                    </div>
                </div>
            )}

            {/* 继续阅读横向轮播：原位置内容已移动到 Hero 区 */}

            {/* 猜你喜欢 - AI 推荐 */}
            <div ref={recommendationsRef}>
            {(recsLoading || recommendations.length > 0 || !recommendationsRequested) && (
                <div>
                    <h2 className="text-lg font-semibold text-white flex items-center gap-2 mb-4">
                        <Sparkles className="w-5 h-5 text-amber-400" />
                        {t('dashboard.recommendations.title')}
                        <span className="text-xs text-gray-500 font-normal ml-1">{t('dashboard.recommendations.subtitle')}</span>

                        <div className="flex-1" />

                        <button
                            onClick={() => loadRecommendations(true)}
                            disabled={recsLoading}
                            className={`flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-md transition-all ${recsLoading
                                    ? 'bg-komgaSurface border border-gray-800 text-gray-500 cursor-not-allowed'
                                    : 'bg-gray-800 hover:bg-gray-700 text-gray-300 hover:text-white border border-gray-700'
                                }`}
                            title={t('dashboard.recommendations.refresh')}
                        >
                            <RefreshCcw className={`w-3.5 h-3.5 ${recsLoading ? 'animate-spin' : ''}`} />
                            {t('dashboard.recommendations.refresh')}
                        </button>
                    </h2>
                    {recsLoading ? (
                        <div className="grid grid-cols-3 sm:grid-cols-4 md:grid-cols-5 gap-3">
                            {Array.from({ length: 5 }, (_, i) => (
                                <div key={i} className="animate-pulse">
                                    <div className="aspect-2/3 rounded-xl bg-gray-800" />
                                    <div className="mt-2 h-3 w-3/4 bg-gray-800 rounded-sm" />
                                    <div className="mt-1 h-2 w-1/2 bg-gray-800/50 rounded-sm" />
                                </div>
                            ))}
                        </div>
                    ) : !recommendationsRequested ? (
                        <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-5 text-sm text-gray-500">
                            {t('dashboard.recommendations.deferred')}
                        </div>
                    ) : (
                        <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
                            {recommendations.map(item => {
                                const coverUrl = item.cover_path ? `/api/thumbnails/${item.cover_path}` : '';
                                return (
                                    <div key={item.series_id} onClick={() => navigate(`/series/${item.series_id}`)} className="group cursor-pointer flex bg-gray-900 border border-gray-800 rounded-xl overflow-hidden hover:border-amber-500/40 transition-all shadow-lg">
                                        <div className="w-24 shrink-0 aspect-2/3 relative bg-black">
                                            {coverUrl ? (
                                                <img src={coverUrl} alt={item.title} className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500" />
                                            ) : (
                                                <div className="w-full h-full flex items-center justify-center text-gray-700"><BookOpen className="w-8 h-8" /></div>
                                            )}
                                            <div className="absolute top-0 right-0 p-1">
                                                <Sparkles className="w-4 h-4 text-amber-400 drop-shadow-md" />
                                            </div>
                                        </div>
                                        <div className="p-3 flex flex-col min-w-0">
                                            <p className="text-sm font-medium text-gray-200 truncate group-hover:text-amber-400 transition-colors">{item.title}</p>
                                            <p className="text-xs text-gray-400 mt-2 line-clamp-3 leading-relaxed">{item.reason}</p>
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                    )}
                </div>
            )}
            </div>
        </div>
    );
}

// 统计卡片组件
function StatCard({ icon, label, value, subtitle, color, borderColor, iconColor }: {
    icon: React.ReactNode;
    label: string;
    value: string | number;
    subtitle?: string;
    color: string;
    borderColor: string;
    iconColor: string;
}) {
    return (
        <div className={`bg-linear-to-br ${color} border ${borderColor} rounded-2xl p-5 relative overflow-hidden group hover:scale-[1.02] transition-transform duration-300`}>
            <div className={`${iconColor} mb-3 opacity-80`}>{icon}</div>
            <p className="text-2xl font-bold text-white mb-1">{value}</p>
            <p className="text-sm text-gray-400">{label}</p>
            {subtitle && <p className="text-xs text-gray-500 mt-1">{subtitle}</p>}
            {/* 装饰光斑 */}
            <div className={`absolute -top-8 -right-8 w-24 h-24 rounded-full bg-linear-to-br ${color} opacity-30 blur-2xl group-hover:opacity-50 transition-opacity`} />
        </div>
    );
}





// GitHub 风格活跃热力图组件
function ActivityHeatmap({ data, activeDays7, weeks, onChangeWeeks }: { data: ActivityDay[]; activeDays7: number; weeks: number; onChangeWeeks: (w: number) => void }) {
    const { locale, t, formatNumber } = useI18n();
    const TOTAL_DAYS = weeks * 7;

    // 构建日期 → 页数的映射
    const activityMap = new Map<string, number>();
    data.forEach(d => activityMap.set(d.date, d.page_count));

    // 计算颜色等级的阈值
    const maxPages = Math.max(...data.map(d => d.page_count), 1);
    const getLevel = (count: number): number => {
        if (count === 0) return 0;
        if (count <= maxPages * 0.25) return 1;
        if (count <= maxPages * 0.5) return 2;
        if (count <= maxPages * 0.75) return 3;
        return 4;
    };

    const levelColors = [
        'bg-gray-800/60',           // 0: 无活动
        'bg-purple-900/80',         // 1: 少
        'bg-purple-700/80',         // 2: 中
        'bg-purple-500',            // 3: 多
        'bg-komgaPrimary',          // 4: 极多
    ];

    // 生成从今天往前 TOTAL_DAYS 天的日期网格
    const today = new Date();
    const cells: { date: string; count: number; dayOfWeek: number }[] = [];

    // 找到起始日期：从 TOTAL_DAYS 前开始，对齐到周一
    const startDate = new Date(today);
    startDate.setDate(startDate.getDate() - TOTAL_DAYS + 1);
    // 调整到最近的周一
    const startDow = startDate.getDay();
    const adjustToMonday = startDow === 0 ? -6 : 1 - startDow;
    startDate.setDate(startDate.getDate() + adjustToMonday);

    const endDate = new Date(today);
    const current = new Date(startDate);
    while (current <= endDate) {
        const dateStr = current.toISOString().slice(0, 10);
        const count = activityMap.get(dateStr) || 0;
        cells.push({ date: dateStr, count, dayOfWeek: current.getDay() });
        current.setDate(current.getDate() + 1);
    }

    // 按周分组（每列一周，每行一天）
    const weekColumns: typeof cells[] = [];
    let weekBuf: typeof cells = [];
    for (const cell of cells) {
        weekBuf.push(cell);
        if (cell.dayOfWeek === 0) { // 周日结束一周
            weekColumns.push(weekBuf);
            weekBuf = [];
        }
    }
    if (weekBuf.length > 0) weekColumns.push(weekBuf);

    // 月份标签
    const monthLabels: { label: string; colIndex: number }[] = [];
    let lastMonth = -1;
    weekColumns.forEach((week, colIdx) => {
        const firstDay = week[0];
        if (firstDay) {
            const monthDate = new Date(firstDay.date);
            const month = monthDate.getMonth();
            if (month !== lastMonth) {
                monthLabels.push({
                    label: new Intl.DateTimeFormat(locale, { month: 'short' }).format(monthDate),
                    colIndex: colIdx,
                });
                lastMonth = month;
            }
        }
    });

    const dayLabels = ['', t('dashboard.activity.day.mon'), '', t('dashboard.activity.day.wed'), '', t('dashboard.activity.day.fri'), ''];

    const [tooltip, setTooltip] = useState<{ text: string; x: number; y: number } | null>(null);

    return (
        <div className="bg-komgaSurface border border-gray-800 rounded-2xl p-6 relative">
            <div className="flex items-center justify-between mb-4">
                <div>
                    <h3 className="text-lg font-semibold text-white">{t('dashboard.activity.title')}</h3>
                    <p className="text-xs text-gray-500 mt-1">
                        {t('dashboard.activity.summary', { count: formatNumber(activeDays7) })}
                    </p>
                </div>
                <div className="flex items-center">
                    <select
                        value={weeks}
                        onChange={(e) => onChangeWeeks(Number(e.target.value))}
                        className="bg-gray-900 border border-gray-700 text-gray-300 text-xs rounded-lg focus:ring-komgaPrimary focus:border-komgaPrimary block px-2.5 py-1.5 transition-colors"
                    >
                        <option value={13}>{t('dashboard.activity.range.13weeks')}</option>
                        <option value={26}>{t('dashboard.activity.range.26weeks')}</option>
                        <option value={52}>{t('dashboard.activity.range.52weeks')}</option>
                    </select>
                </div>
            </div>

            <div className="overflow-x-auto">
                <div className="inline-flex flex-col gap-0.5 min-w-fit">
                    {/* 月份标签行 */}
                    <div className="flex ml-8 mb-1">
                        {weekColumns.map((_, colIdx) => {
                            const ml = monthLabels.find(m => m.colIndex === colIdx);
                            return (
                                <div key={colIdx} className="w-[13px] mx-[1.5px] shrink-0">
                                    {ml && <span className="text-[10px] text-gray-500 whitespace-nowrap">{ml.label}</span>}
                                </div>
                            );
                        })}
                    </div>

                    {/* 热力图网格：7 行 × N 列 */}
                    {Array.from({ length: 7 }, (_, rowIdx) => (
                        <div key={rowIdx} className="flex items-center gap-0">
                            <div className="w-7 text-right pr-1.5 shrink-0">
                                <span className="text-[10px] text-gray-600 leading-none">{dayLabels[rowIdx]}</span>
                            </div>
                            <div className="flex gap-[3px]">
                                {weekColumns.map((week, colIdx) => {
                                    // 行索引对应周一=0, 周二=1, ..., 周日=6
                                    const mappedDow = rowIdx === 6 ? 0 : rowIdx + 1;
                                    const cell = week.find(c => c.dayOfWeek === mappedDow);
                                    if (!cell) {
                                        return <div key={colIdx} className="w-[13px] h-[13px] rounded-xs" />;
                                    }
                                    const level = getLevel(cell.count);
                                    return (
                                        <div
                                            key={colIdx}
                                            className={`w-[13px] h-[13px] rounded-xs ${levelColors[level]} transition-all duration-200 hover:ring-1 hover:ring-white/30 cursor-pointer`}
                                            onMouseEnter={(e) => {
                                                const rect = e.currentTarget.getBoundingClientRect();
                                                setTooltip({
                                                    text: cell.count > 0
                                                        ? `${cell.date}: ${t('dashboard.activity.pagesRead', { count: formatNumber(cell.count) })}`
                                                        : `${cell.date}: ${t('dashboard.activity.noActivity')}`,
                                                    x: rect.left + rect.width / 2,
                                                    y: rect.top - 8
                                                });
                                            }}
                                            onMouseLeave={() => setTooltip(null)}
                                        />
                                    );
                                })}
                            </div>
                        </div>
                    ))}
                </div>
            </div>

            {/* 图例 */}
            <div className="flex items-center justify-end gap-1.5 mt-3">
                <span className="text-[10px] text-gray-500 mr-1">{t('dashboard.activity.legendLess')}</span>
                {levelColors.map((color, idx) => (
                    <div key={idx} className={`w-[11px] h-[11px] rounded-xs ${color}`} />
                ))}
                <span className="text-[10px] text-gray-500 ml-1">{t('dashboard.activity.legendMore')}</span>
            </div>

            {/* Tooltip */}
            {tooltip && (
                <div
                    className="fixed z-50 px-2.5 py-1.5 bg-gray-900 border border-gray-700 rounded-lg text-xs text-white shadow-xl pointer-events-none whitespace-nowrap"
                    style={{ left: tooltip.x, top: tooltip.y, transform: 'translate(-50%, -100%)' }}
                >
                    {tooltip.text}
                </div>
            )}
        </div>
    );
}

function OnboardingCard({
    title,
    description,
    actionLabel,
    onClick,
    icon,
}: {
    title: string;
    description: string;
    actionLabel: string;
    onClick: () => void;
    icon: React.ReactNode;
}) {
    return (
        <div className="rounded-2xl border border-gray-800 bg-black/20 p-5">
            <div className="flex items-center gap-2 mb-3">
                {icon}
                <h2 className="text-base font-semibold text-white">{title}</h2>
            </div>
            <p className="text-sm text-gray-400 leading-6 min-h-[72px]">{description}</p>
            <button
                onClick={onClick}
                className="mt-4 inline-flex items-center gap-2 rounded-lg bg-gray-900 px-3 py-2 text-sm text-white hover:bg-gray-800"
            >
                {actionLabel}
            </button>
        </div>
    );
}

// 格式化字节数
function formatBytes(bytes: number, decimals = 2) {
    if (!+bytes) return '0 B';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`;
}

// 存储空间环形图组件
function StoragePieChart({ librarySizes }: { librarySizes: LibrarySize[] }) {
    const { t } = useI18n();
    const [tooltip, setTooltip] = useState<{ text: string; x: number; y: number } | null>(null);

    if (!librarySizes || librarySizes.length === 0) {
        return (
            <div className="bg-komgaSurface border border-gray-800 rounded-2xl p-6 flex flex-col items-center justify-center h-full min-h-[160px]">
                <p className="text-gray-500 text-sm">{t('dashboard.storage.empty')}</p>
            </div>
        );
    }

    const totalSize = librarySizes.reduce((sum, ls) => sum + ls.total_size, 0);

    // 科技感配色方案
    const colors = [
        '#8b5cf6', // purple-500
        '#3b82f6', // blue-500
        '#10b981', // emerald-500
        '#f59e0b', // amber-500
        '#ef4444', // red-500
        '#06b6d4', // cyan-500
        '#ec4899', // pink-500
    ];

    // 计算 SVG 环形的分段
    const segments = librarySizes.map((ls, index) => {
        const percentage = totalSize > 0 ? (ls.total_size / totalSize) : 0;
        const length = percentage * 100;
        const gap = 100 - length;
        const strokeDasharray = `${length} ${gap}`;

        // 使用纯函数方式计算 index 之前的所有百分比之和，规避 immutability 重新赋值报错
        const previousSum = librarySizes.slice(0, index).reduce((sum, item) => {
            const pct = totalSize > 0 ? (item.total_size / totalSize) : 0;
            return sum + pct * 100;
        }, 0);
        const strokeDashoffset = -previousSum;

        return {
            ...ls,
            color: colors[index % colors.length],
            percentage,
            strokeDasharray,
            strokeDashoffset,
        };
    });

    return (
        <div className="bg-komgaSurface border border-gray-800 rounded-2xl p-6 relative h-full">
            <h3 className="text-lg font-semibold text-white mb-4">{t('dashboard.storage.title')}</h3>

            <div className="flex flex-col sm:flex-row items-center gap-6">
                {/* 环形图 */}
                <div className="relative w-28 h-28 shrink-0">
                    <svg viewBox="0 0 42 42" className="w-full h-full -rotate-90 drop-shadow-xl overflow-visible">
                        {segments.map((segment) => (
                            <circle
                                key={segment.library_id}
                                cx="21" cy="21" r="15.91549430918954"
                                fill="none"
                                stroke={segment.color}
                                strokeWidth="4.5"
                                strokeDasharray={segment.strokeDasharray}
                                strokeDashoffset={segment.strokeDashoffset}
                                className="transition-all duration-1000 ease-out cursor-pointer hover:stroke-[5.5px]"
                                onMouseEnter={(e) => {
                                    const rect = e.currentTarget.getBoundingClientRect();
                                    setTooltip({
                                        text: `${segment.library_name}: ${formatBytes(segment.total_size)} (${(segment.percentage * 100).toFixed(1)}%)`,
                                        x: rect.left + rect.width / 2,
                                        y: rect.top - 10
                                    });
                                }}
                                onMouseLeave={() => setTooltip(null)}
                            />
                        ))}
                    </svg>
                    {/* 中心总容量 */}
                    <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
                        <span className="text-[9px] text-gray-400 tracking-wide uppercase">{t('dashboard.storage.total')}</span>
                        <span className="text-xs font-bold text-white tracking-tight mt-0.5">{formatBytes(totalSize, 1)}</span>
                    </div>
                </div>

                {/* 图例列表 */}
                <div className="flex-1 w-full space-y-2.5">
                    {segments.slice(0, 5).map((segment) => (
                        <div key={segment.library_id} className="flex items-center justify-between group">
                            <div className="flex items-center gap-2.5 min-w-0">
                                <div className="w-2.5 h-2.5 rounded-full shrink-0 shadow-xs" style={{ backgroundColor: segment.color }} />
                                <span className="text-xs text-gray-300 truncate group-hover:text-white transition-colors">{segment.library_name}</span>
                            </div>
                            <span className="text-xs font-medium text-gray-400 shrink-0 ml-3 group-hover:text-white transition-colors">{formatBytes(segment.total_size)}</span>
                        </div>
                    ))}
                    {segments.length > 5 && (
                        <div className="text-xs text-gray-500 mt-2 italic">
                            + {t('dashboard.storage.others', { count: segments.length - 5 })} {t('dashboard.storage.more')}
                        </div>
                    )}
                </div>
            </div>

            {/* Tooltip */}
            {tooltip && (
                <div
                    className="fixed z-50 px-3 py-2 bg-gray-900/95 border border-gray-700 rounded-lg text-xs text-white shadow-2xl pointer-events-none whitespace-nowrap backdrop-blur-xs"
                    style={{ left: tooltip.x, top: tooltip.y, transform: 'translate(-50%, -100%)' }}
                >
                    {tooltip.text}
                </div>
            )}
        </div>
    );
}
