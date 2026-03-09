import { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import axios from 'axios';
import { BookOpen, Library, Eye, FileText, TrendingUp, ChevronLeft, ChevronRight, Sparkles, RefreshCcw } from 'lucide-react';

interface DashboardStats {
    total_series: number;
    total_books: number;
    read_books: number;
    total_pages: number;
    active_days_7: number;
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
    book_title: { String: string; Valid: boolean };
    cover_path: { String: string; Valid: boolean };
    last_read_page: { Int64: number; Valid: boolean };
    last_read_at: { Time: string; Valid: boolean };
    page_count: number;
}

interface RecommendedItem {
    series_id: number;
    title: string;
    cover_path: string;
    reason: string;
}

export default function Dashboard() {
    const [stats, setStats] = useState<DashboardStats | null>(null);
    const [recentReads, setRecentReads] = useState<RecentReadItem[]>([]);
    const [recommendations, setRecommendations] = useState<RecommendedItem[]>([]);
    const [heatmapData, setHeatmapData] = useState<ActivityDay[]>([]);
    const [loading, setLoading] = useState(true);
    const scrollRef = useRef<HTMLDivElement>(null);
    const navigate = useNavigate();

    useEffect(() => {
        Promise.all([
            axios.get('/api/stats/dashboard'),
            axios.get('/api/stats/recent-read?limit=20').catch(() => ({ data: [] })),
            axios.get('/api/stats/activity-heatmap?weeks=16').catch(() => ({ data: [] }))
        ]).then(([statsRes, recentRes, heatmapRes]) => {
            setStats(statsRes.data);
            setRecentReads(Array.isArray(recentRes.data) ? recentRes.data : []);
            setHeatmapData(Array.isArray(heatmapRes.data) ? heatmapRes.data : []);
        }).catch(console.error).finally(() => setLoading(false));
    }, []);

    // AI 推荐独立加载，不阻塞页面主体渲染
    const [recsLoading, setRecsLoading] = useState(true);

    const loadRecommendations = (forceRefresh = false) => {
        setRecsLoading(true);
        const url = forceRefresh ? '/api/stats/recommendations?limit=10&refresh=true' : '/api/stats/recommendations?limit=10';
        axios.get(url)
            .then(res => setRecommendations(Array.isArray(res.data) ? res.data : []))
            .catch(console.error)
            .finally(() => setRecsLoading(false));
    };

    useEffect(() => {
        loadRecommendations(false);
    }, []);

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

    return (
        <div className="p-4 sm:p-8 max-w-6xl mx-auto space-y-8">
            {/* 标题 */}
            <div className="flex items-center gap-3">
                <TrendingUp className="w-7 h-7 text-komgaPrimary" />
                <h1 className="text-2xl font-bold text-white tracking-tight">仪表板</h1>
            </div>

            {/* 统计卡片网格 */}
            {stats && (
                <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
                    <StatCard
                        icon={<Library className="w-5 h-5" />}
                        label="漫画系列"
                        value={stats.total_series}
                        color="from-purple-500/20 to-purple-600/5"
                        borderColor="border-purple-500/30"
                        iconColor="text-purple-400"
                    />
                    <StatCard
                        icon={<BookOpen className="w-5 h-5" />}
                        label="书籍总册数"
                        value={stats.total_books}
                        color="from-blue-500/20 to-blue-600/5"
                        borderColor="border-blue-500/30"
                        iconColor="text-blue-400"
                    />
                    <StatCard
                        icon={<Eye className="w-5 h-5" />}
                        label="已阅读"
                        value={`${stats.read_books} 册`}
                        subtitle={`${readPercent}% 完读率`}
                        color="from-emerald-500/20 to-emerald-600/5"
                        borderColor="border-emerald-500/30"
                        iconColor="text-emerald-400"
                    />
                    <StatCard
                        icon={<FileText className="w-5 h-5" />}
                        label="馆藏总页数"
                        value={stats.total_pages.toLocaleString()}
                        subtitle={`近7日活跃 ${stats.active_days_7} 天`}
                        color="from-amber-500/20 to-amber-600/5"
                        borderColor="border-amber-500/30"
                        iconColor="text-amber-400"
                    />
                </div>
            )}

            {/* 阅读进度环形图 + GitHub 风格活跃热力图 */}
            {stats && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    {/* 完读进度环 */}
                    <div className="bg-komgaSurface border border-gray-800 rounded-2xl p-6 flex items-center gap-6">
                        <div className="relative w-28 h-28 shrink-0">
                            <svg viewBox="0 0 36 36" className="w-full h-full -rotate-90">
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
                            <h3 className="text-lg font-semibold text-white mb-1">阅读完成度</h3>
                            <p className="text-sm text-gray-400">
                                已阅读 <span className="text-komgaPrimary font-medium">{stats.read_books}</span> / {stats.total_books} 册
                            </p>
                            <p className="text-xs text-gray-500 mt-2">
                                共计 {stats.total_pages.toLocaleString()} 页漫画内容
                            </p>
                        </div>
                    </div>

                    {/* GitHub 风格活跃热力图 */}
                    <ActivityHeatmap data={heatmapData} activeDays7={stats.active_days_7} />
                </div>
            )}

            {/* 继续阅读横向轮播 */}
            {recentReads.length > 0 && (
                <div>
                    <div className="flex items-center justify-between mb-4">
                        <h2 className="text-lg font-semibold text-white flex items-center gap-2">
                            <BookOpen className="w-5 h-5 text-komgaPrimary" />
                            继续阅读
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
                            const progress = item.page_count > 0 && item.last_read_page?.Valid
                                ? Math.round((item.last_read_page.Int64 / item.page_count) * 100) : 0;
                            const coverUrl = item.cover_path?.Valid ? `/api/thumbnails/${item.cover_path.String}` : '';

                            return (
                                <div
                                    key={`${item.series_id}-${item.book_id}`}
                                    onClick={() => navigate(`/reader/${item.book_id}`)}
                                    className="group flex-shrink-0 w-40 snap-start cursor-pointer"
                                >
                                    <div className="relative aspect-[2/3] rounded-xl overflow-hidden bg-gray-900 border border-gray-800 group-hover:border-komgaPrimary/50 transition-all duration-300 shadow-lg group-hover:shadow-komgaPrimary/10">
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

                                        {/* 进度条覆盖 */}
                                        <div className="absolute bottom-0 inset-x-0 h-1 bg-gray-900/80">
                                            <div className="h-full bg-komgaPrimary transition-all" style={{ width: `${progress}%` }} />
                                        </div>

                                        {/* 悬停覆盖层 */}
                                        <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300 flex items-end p-3">
                                            <span className="text-xs text-white font-medium">
                                                继续 → 第 {item.last_read_page?.Valid ? item.last_read_page.Int64 : 1} 页
                                            </span>
                                        </div>
                                    </div>

                                    <div className="mt-2 px-1">
                                        <p className="text-sm font-medium text-gray-200 truncate group-hover:text-komgaPrimary transition-colors">{item.series_name}</p>
                                        <p className="text-xs text-gray-500 truncate mt-0.5">
                                            {item.book_title?.Valid ? item.book_title.String : item.book_name}
                                        </p>
                                        <p className="text-[10px] text-gray-600 mt-1">{progress}% 已读</p>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                </div>
            )}

            {/* 猜你喜欢 - AI 推荐 */}
            {(recsLoading || recommendations.length > 0) && (
                <div>
                    <h2 className="text-lg font-semibold text-white flex items-center gap-2 mb-4">
                        <Sparkles className="w-5 h-5 text-amber-400" />
                        猜你喜欢
                        <span className="text-xs text-gray-500 font-normal ml-1">基于你的收藏和阅读偏好</span>

                        <div className="flex-1" />

                        <button
                            onClick={() => loadRecommendations(true)}
                            disabled={recsLoading}
                            className={`flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-md transition-all ${recsLoading
                                    ? 'bg-komgaSurface border border-gray-800 text-gray-500 cursor-not-allowed'
                                    : 'bg-gray-800 hover:bg-gray-700 text-gray-300 hover:text-white border border-gray-700'
                                }`}
                            title="换一批推荐"
                        >
                            <RefreshCcw className={`w-3.5 h-3.5 ${recsLoading ? 'animate-spin' : ''}`} />
                            换一批
                        </button>
                    </h2>
                    {recsLoading ? (
                        <div className="grid grid-cols-3 sm:grid-cols-4 md:grid-cols-5 gap-3">
                            {Array.from({ length: 5 }, (_, i) => (
                                <div key={i} className="animate-pulse">
                                    <div className="aspect-[2/3] rounded-xl bg-gray-800" />
                                    <div className="mt-2 h-3 w-3/4 bg-gray-800 rounded" />
                                    <div className="mt-1 h-2 w-1/2 bg-gray-800/50 rounded" />
                                </div>
                            ))}
                        </div>
                    ) : (
                        <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
                            {recommendations.map(item => {
                                const coverUrl = item.cover_path ? `/api/thumbnails/${item.cover_path}` : '';
                                return (
                                    <div key={item.series_id} onClick={() => navigate(`/series/${item.series_id}`)} className="group cursor-pointer flex bg-gray-900 border border-gray-800 rounded-xl overflow-hidden hover:border-amber-500/40 transition-all shadow-lg">
                                        <div className="w-24 shrink-0 aspect-[2/3] relative bg-black">
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
        <div className={`bg-gradient-to-br ${color} border ${borderColor} rounded-2xl p-5 relative overflow-hidden group hover:scale-[1.02] transition-transform duration-300`}>
            <div className={`${iconColor} mb-3 opacity-80`}>{icon}</div>
            <p className="text-2xl font-bold text-white mb-1">{value}</p>
            <p className="text-sm text-gray-400">{label}</p>
            {subtitle && <p className="text-xs text-gray-500 mt-1">{subtitle}</p>}
            {/* 装饰光斑 */}
            <div className={`absolute -top-8 -right-8 w-24 h-24 rounded-full bg-gradient-to-br ${color} opacity-30 blur-2xl group-hover:opacity-50 transition-opacity`} />
        </div>
    );
}

// GitHub 风格活跃热力图组件
function ActivityHeatmap({ data, activeDays7 }: { data: ActivityDay[]; activeDays7: number }) {
    const WEEKS = 16;
    const TOTAL_DAYS = WEEKS * 7;

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
    const weeks: typeof cells[] = [];
    let weekBuf: typeof cells = [];
    for (const cell of cells) {
        weekBuf.push(cell);
        if (cell.dayOfWeek === 0) { // 周日结束一周
            weeks.push(weekBuf);
            weekBuf = [];
        }
    }
    if (weekBuf.length > 0) weeks.push(weekBuf);

    // 月份标签
    const monthLabels: { label: string; colIndex: number }[] = [];
    const months = ['1月', '2月', '3月', '4月', '5月', '6月', '7月', '8月', '9月', '10月', '11月', '12月'];
    let lastMonth = -1;
    weeks.forEach((week, colIdx) => {
        const firstDay = week[0];
        if (firstDay) {
            const month = new Date(firstDay.date).getMonth();
            if (month !== lastMonth) {
                monthLabels.push({ label: months[month], colIndex: colIdx });
                lastMonth = month;
            }
        }
    });

    const dayLabels = ['', '一', '', '三', '', '五', ''];

    const [tooltip, setTooltip] = useState<{ text: string; x: number; y: number } | null>(null);

    return (
        <div className="bg-komgaSurface border border-gray-800 rounded-2xl p-6 relative">
            <div className="flex items-center justify-between mb-4">
                <h3 className="text-lg font-semibold text-white">阅读活跃度</h3>
                <p className="text-xs text-gray-500">
                    近 7 天活跃 <span className="text-komgaPrimary font-medium">{activeDays7}</span> 天
                </p>
            </div>

            <div className="overflow-x-auto">
                <div className="inline-flex flex-col gap-0.5 min-w-fit">
                    {/* 月份标签行 */}
                    <div className="flex ml-8 mb-1">
                        {weeks.map((_, colIdx) => {
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
                                {weeks.map((week, colIdx) => {
                                    // 行索引对应周一=0, 周二=1, ..., 周日=6
                                    const mappedDow = rowIdx === 6 ? 0 : rowIdx + 1;
                                    const cell = week.find(c => c.dayOfWeek === mappedDow);
                                    if (!cell) {
                                        return <div key={colIdx} className="w-[13px] h-[13px] rounded-sm" />;
                                    }
                                    const level = getLevel(cell.count);
                                    return (
                                        <div
                                            key={colIdx}
                                            className={`w-[13px] h-[13px] rounded-sm ${levelColors[level]} transition-all duration-200 hover:ring-1 hover:ring-white/30 cursor-pointer`}
                                            onMouseEnter={(e) => {
                                                const rect = e.currentTarget.getBoundingClientRect();
                                                setTooltip({
                                                    text: cell.count > 0 ? `${cell.date}: ${cell.count} 页` : `${cell.date}: 无活动`,
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
                <span className="text-[10px] text-gray-500 mr-1">少</span>
                {levelColors.map((color, idx) => (
                    <div key={idx} className={`w-[11px] h-[11px] rounded-sm ${color}`} />
                ))}
                <span className="text-[10px] text-gray-500 ml-1">多</span>
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
