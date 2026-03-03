import { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import axios from 'axios';
import { BookOpen, Library, Eye, FileText, TrendingUp, ChevronLeft, ChevronRight } from 'lucide-react';

interface DashboardStats {
    total_series: number;
    total_books: number;
    read_books: number;
    total_pages: number;
    active_days_7: number;
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

export default function Dashboard() {
    const [stats, setStats] = useState<DashboardStats | null>(null);
    const [recentReads, setRecentReads] = useState<RecentReadItem[]>([]);
    const [loading, setLoading] = useState(true);
    const scrollRef = useRef<HTMLDivElement>(null);
    const navigate = useNavigate();

    useEffect(() => {
        Promise.all([
            axios.get('/api/stats/dashboard'),
            axios.get('/api/stats/recent-read?limit=20').catch(() => ({ data: [] }))
        ]).then(([statsRes, recentRes]) => {
            setStats(statsRes.data);
            setRecentReads(Array.isArray(recentRes.data) ? recentRes.data : []);
        }).catch(console.error).finally(() => setLoading(false));
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

            {/* 阅读进度环形图 + 7日活跃 */}
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

                    {/* 7日活跃概览 */}
                    <div className="bg-komgaSurface border border-gray-800 rounded-2xl p-6">
                        <h3 className="text-lg font-semibold text-white mb-3">近七日活跃</h3>
                        <div className="flex items-end gap-2 h-20">
                            {Array.from({ length: 7 }, (_, i) => {
                                const dayActive = i < stats.active_days_7;
                                const height = dayActive ? 60 + Math.random() * 40 : 15;
                                const labels = ['一', '二', '三', '四', '五', '六', '日'];
                                return (
                                    <div key={i} className="flex-1 flex flex-col items-center gap-1">
                                        <div
                                            className={`w-full rounded-t-md transition-all duration-700 ease-out ${dayActive ? 'bg-gradient-to-t from-komgaPrimary to-purple-400' : 'bg-gray-800'}`}
                                            style={{ height: `${height}%` }}
                                        />
                                        <span className="text-[10px] text-gray-500">{labels[i]}</span>
                                    </div>
                                );
                            })}
                        </div>
                        <p className="text-xs text-gray-500 mt-3">
                            过去 7 天中有 <span className="text-komgaPrimary font-medium">{stats.active_days_7}</span> 天保持了阅读习惯
                        </p>
                    </div>
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
