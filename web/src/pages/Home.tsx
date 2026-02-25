import { useState, useEffect } from 'react';
import axios from 'axios';
import { useParams, Link, useOutletContext } from 'react-router-dom';
import { ImageIcon, ChevronLeft, ChevronRight, Heart } from 'lucide-react';

interface NullString {
    String: string;
    Valid: boolean;
}

interface Series {
    id: number;
    name: string;
    title?: NullString;
    summary?: NullString;
    rating?: { Float64: number, Valid: boolean };
    cover_path?: NullString;
    tags_string?: string | null;
    volume_count: number;
    actual_book_count: number;
    read_count: number;
    total_pages: { Float64: number, Valid: boolean };
    is_favorite: boolean;
}

const PAGE_SIZE = 30;

export default function Home() {
    const { libId } = useParams();
    const { refreshTrigger } = useOutletContext<{ refreshTrigger: number }>() || { refreshTrigger: 0 };
    const [allSeries, setAllSeries] = useState<Series[]>([]);
    const [totalSeries, setTotalSeries] = useState(0);
    const [loading, setLoading] = useState(false);
    const [activeTag, setActiveTag] = useState<string | null>(null);
    const [activeAuthor, setActiveAuthor] = useState<string | null>(null);
    const [activeStatus, setActiveStatus] = useState<string | null>(null);
    const [activeLetter, setActiveLetter] = useState<string | null>(null);
    const [sortBy, setSortBy] = useState<string>('name_asc');
    const [page, setPage] = useState(1);

    const [allTags, setAllTags] = useState<{ name: string }[]>([]);
    const [allAuthors, setAllAuthors] = useState<{ name: string }[]>([]);
    const allStatuses = ['已完结', '连载中', '已放弃', '有生之年'];

    useEffect(() => {
        Promise.all([
            axios.get('/api/tags/all').catch(() => ({ data: [] })),
            axios.get('/api/authors/all').catch(() => ({ data: [] }))
        ]).then(([tRes, aRes]) => {
            // Deduplicate authors by name since we might have Writer, Penciller combinations
            const tNames = tRes.data || [];
            const aList = aRes.data || [];
            const map = new Map();
            aList.forEach((a: any) => map.set(a.name, a));

            setAllTags(tNames);
            setAllAuthors(Array.from(map.values()));
        });
    }, []);

    useEffect(() => {
        if (libId) {
            // 防闪烁：仅当没有数据时才显示大面积 Loading，否则在后台静默获取
            setLoading(allSeries.length === 0);
            const params = new URLSearchParams();
            params.append('libraryId', libId);
            params.append('limit', PAGE_SIZE.toString());
            params.append('page', page.toString());
            if (activeTag) params.append('tags', activeTag);
            if (activeAuthor) params.append('authors', activeAuthor);
            if (activeStatus) params.append('status', activeStatus);
            if (activeLetter) params.append('letter', activeLetter);
            if (sortBy) params.append('sortBy', sortBy);

            axios.get(`/api/series/search?${params.toString()}`)
                .then(res => {
                    setAllSeries(res.data.items || []);
                    setTotalSeries(res.data.total || 0);
                    setLoading(false);
                })
                .catch(err => {
                    console.error("Failed to fetch series:", err);
                    setLoading(false);
                });
        }
    }, [libId, refreshTrigger, activeTag, activeAuthor, activeStatus, activeLetter, page]);

    // 分页逻辑
    const totalPages = Math.max(1, Math.ceil(totalSeries / PAGE_SIZE));

    // 当筛选条件变化时重置到第一页
    useEffect(() => { setPage(1); }, [activeTag, activeAuthor, activeStatus, activeLetter]);

    if (!libId) {
        return (
            <div className="flex-1 flex items-center justify-center p-10 h-full text-gray-500">
                请在左侧选择一个扫描库以开始
            </div>
        );
    }

    return (
        <div className="p-6 lg:p-10">
            {/* 头部信息栏 */}
            <div className="mb-6 flex flex-col sm:flex-row sm:justify-between sm:items-end gap-4 border-b border-gray-800 pb-4">
                <div>
                    <h2 className="text-3xl font-bold text-white tracking-tight mb-1">浏览系列</h2>
                    <p className="text-gray-400 text-sm">
                        资源库返回 {totalSeries} 个结果
                    </p>
                </div>
                <div className="flex items-center gap-3">
                    <span className="text-sm text-gray-400 font-medium">排序方式</span>
                    <select
                        value={sortBy}
                        onChange={(e) => setSortBy(e.target.value)}
                        className="bg-gray-900 border border-gray-700 text-white text-sm rounded-lg focus:ring-komgaPrimary focus:border-komgaPrimary block p-2 outline-none transition-colors cursor-pointer hover:border-gray-500"
                    >
                        <option value="name_asc">按名称排序 (A-Z)</option>
                        <option value="name_desc">按名称排序 (Z-A)</option>
                        <option value="created_desc">最新加入在先</option>
                        <option value="updated_desc">最新更新连载在先</option>
                        <option value="rating_desc">最高评分优先</option>
                        <option value="books_desc">内容最多优先</option>
                        <option value="books_asc">内容最少优先</option>
                        <option value="favorite_desc">我的收藏置顶</option>
                    </select>
                </div>
            </div>

            {/* 标签与作者与状态 聚合导航栏 */}
            <div className="mb-6 grid xl:grid-cols-3 gap-8 divide-y xl:divide-y-0 xl:divide-x divide-gray-800">
                <div className="xl:pr-8">
                    <span className="text-komgaPrimary font-semibold text-sm mb-3 block">连载状态 (Status)</span>
                    <div className="flex flex-wrap gap-2 max-h-32 overflow-y-auto pr-2 custom-scrollbar">
                        <button
                            onClick={() => setActiveStatus(null)}
                            className={`px-3 py-1 text-xs font-medium rounded-full transition-colors border ${activeStatus === null
                                ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                                : 'bg-transparent border-gray-700 text-gray-400 hover:border-gray-500 hover:text-white'
                                }`}
                        >
                            全部状态
                        </button>
                        {allStatuses.map((st) => (
                            <button
                                key={st}
                                onClick={() => setActiveStatus(st === activeStatus ? null : st)}
                                className={`px-3 py-1 text-xs font-medium rounded-full transition-all border flex items-center gap-1.5 ${activeStatus === st
                                    ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                                    : 'bg-transparent border-gray-800 text-gray-400 hover:border-gray-600 hover:text-gray-200 bg-gray-900/40'
                                    }`}
                            >
                                {st}
                            </button>
                        ))}
                    </div>
                </div>
                {allTags.length > 0 && (
                    <div className="pt-6 xl:pt-0 xl:px-8">
                        <span className="text-komgaPrimary font-semibold text-sm mb-3 block">标签分类 (Tags)</span>
                        <div className="flex flex-wrap gap-2 max-h-32 overflow-y-auto pr-2 custom-scrollbar">
                            <button
                                onClick={() => setActiveTag(null)}
                                className={`px-3 py-1 text-xs font-medium rounded-full transition-colors border ${activeTag === null
                                    ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                                    : 'bg-transparent border-gray-700 text-gray-400 hover:border-gray-500 hover:text-white'
                                    }`}
                            >
                                不限
                            </button>
                            {allTags.map((t) => (
                                <button
                                    key={t.name}
                                    onClick={() => setActiveTag(t.name === activeTag ? null : t.name)}
                                    className={`px-3 py-1 text-xs font-medium rounded-full transition-all border flex items-center gap-1.5 ${activeTag === t.name
                                        ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                                        : 'bg-transparent border-gray-800 text-gray-400 hover:border-gray-600 hover:text-gray-200 bg-gray-900/40'
                                        }`}
                                >
                                    {t.name}
                                </button>
                            ))}
                        </div>
                    </div>
                )}
                {allAuthors.length > 0 && (
                    <div className="pt-6 xl:pt-0 xl:pl-8">
                        <span className="text-komgaPrimary font-semibold text-sm mb-3 block">参与人员 (Authors)</span>
                        <div className="flex flex-wrap gap-2 max-h-32 overflow-y-auto pr-2 custom-scrollbar">
                            <button
                                onClick={() => setActiveAuthor(null)}
                                className={`px-3 py-1 text-xs font-medium rounded-full transition-colors border ${activeAuthor === null
                                    ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                                    : 'bg-transparent border-gray-700 text-gray-400 hover:border-gray-500 hover:text-white'
                                    }`}
                            >
                                不限
                            </button>
                            {allAuthors.map((a) => (
                                <button
                                    key={a.name}
                                    onClick={() => setActiveAuthor(a.name === activeAuthor ? null : a.name)}
                                    className={`px-3 py-1 text-xs font-medium rounded-full transition-all border flex items-center gap-1.5 ${activeAuthor === a.name
                                        ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                                        : 'bg-transparent border-gray-800 text-gray-400 hover:border-gray-600 hover:text-gray-200 bg-gray-900/40'
                                        }`}
                                >
                                    {a.name}
                                </button>
                            ))}
                        </div>
                    </div>
                )}
            </div>

            {/* 首字母筛选条 */}
            <div className="mb-8 flex flex-wrap gap-1 items-center justify-center">
                <button
                    onClick={() => setActiveLetter(null)}
                    className={`px-3 py-1.5 text-sm font-medium rounded-md transition-colors ${activeLetter === null ? 'bg-komgaPrimary text-white shadow-md' : 'text-gray-400 hover:bg-gray-800 hover:text-white'}`}
                >
                    全部
                </button>
                {'ABCDEFGHIJKLMNOPQRSTUVWXYZ#'.split('').map(letter => (
                    <button
                        key={letter}
                        onClick={() => setActiveLetter(letter)}
                        className={`w-8 h-8 flex items-center justify-center text-sm font-medium rounded-md transition-colors ${activeLetter === letter ? 'bg-komgaPrimary text-white shadow-md' : 'text-gray-400 hover:bg-gray-800 hover:text-white'}`}
                    >
                        {letter}
                    </button>
                ))}
            </div>

            {loading && allSeries.length === 0 ? (
                <div className="text-center py-20 text-gray-400 animate-pulse">正在加载目录与元数据...</div>
            ) : allSeries.length === 0 ? (
                <div className="text-center py-20 text-gray-500">无匹配的系列</div>
            ) : (
                <>
                    <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-6">
                        {allSeries.map(s => (
                            <Link
                                key={s.id}
                                to={`/series/${s.id}`}
                                className="group relative flex flex-col rounded-xl overflow-hidden bg-komgaSurface border border-gray-800 hover:border-komgaPrimary/50 transition-all duration-300 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10 cursor-pointer"
                            >
                                <div className="aspect-[2/3] w-full bg-gray-900 flex items-center justify-center relative overflow-hidden">
                                    {s.cover_path?.Valid && s.cover_path?.String ? (
                                        <img src={`/api/thumbnails/${s.cover_path.String}`} alt="cover" loading="lazy" className="absolute inset-0 w-full h-full object-cover transition-transform duration-500 group-hover:scale-105" />
                                    ) : (
                                        <ImageIcon className="h-12 w-12 text-gray-700 opacity-50 transition-opacity group-hover:opacity-100 relative z-10" />
                                    )}
                                    <div className="absolute inset-x-0 top-0 p-3 z-20 pointer-events-none flex justify-between items-start">
                                        {s.rating?.Valid && s.rating.Float64 > 0 && (
                                            <span className="flex items-center text-xs font-bold text-yellow-400 bg-black/70 px-1.5 py-0.5 rounded backdrop-blur border border-yellow-400/20 shadow-md">
                                                ★ {s.rating.Float64.toFixed(1)}
                                            </span>
                                        )}
                                        {s.is_favorite && (
                                            <div className="ml-auto bg-black/70 p-1.5 rounded-full backdrop-blur border border-red-500/30 shadow-md">
                                                <Heart className="w-3.5 h-3.5 fill-red-500 text-red-500" />
                                            </div>
                                        )}
                                    </div>
                                    <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/95 via-black/60 to-transparent p-3 pt-8 z-10 pointer-events-none">
                                        <div className="flex justify-between text-[11px] font-medium text-gray-300">
                                            <span>
                                                {s.volume_count > 0 ? `${s.volume_count}卷 · ` : ''}{s.actual_book_count}话
                                            </span>
                                            <span>{s.total_pages?.Valid ? s.total_pages.Float64 : 0} P</span>
                                        </div>
                                        {/* 阅读进度条 */}
                                        {s.total_pages?.Valid && s.total_pages.Float64 > 0 && (
                                            <div className="w-full h-1 bg-gray-700/60 rounded-full mt-1.5 overflow-hidden">
                                                <div
                                                    className={`h-full ${s.read_count >= s.total_pages.Float64 ? 'bg-green-500' : 'bg-komgaPrimary'}`}
                                                    style={{ width: `${Math.min(100, (s.read_count / s.total_pages.Float64) * 100)}%` }}
                                                />
                                            </div>
                                        )}
                                    </div>
                                </div>
                                <div className="p-4 flex-1 flex flex-col justify-between">
                                    <div>
                                        <h4 className="text-sm font-bold text-gray-200 line-clamp-2 leading-snug group-hover:text-komgaPrimary transition-colors">
                                            {s.title?.Valid ? s.title.String : s.name}
                                        </h4>
                                        {s.summary?.Valid && (
                                            <p className="mt-2 text-xs text-gray-500 line-clamp-2">
                                                {s.summary.String}
                                            </p>
                                        )}
                                    </div>
                                    <div className="mt-3 flex items-center justify-between text-xs text-gray-500">
                                        <span>系列</span>
                                        <span className="opacity-0 group-hover:opacity-100 transition-opacity">→</span>
                                    </div>
                                </div>
                            </Link>
                        ))}
                    </div>

                    {/* 分页控件 */}
                    {totalPages > 1 && (
                        <div className="mt-8 flex items-center justify-center gap-2">
                            <button
                                disabled={page <= 1}
                                onClick={() => setPage(p => p - 1)}
                                className="p-2 rounded-lg bg-gray-800 text-gray-400 hover:bg-gray-700 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                            >
                                <ChevronLeft className="w-5 h-5" />
                            </button>
                            {Array.from({ length: totalPages }, (_, i) => i + 1)
                                .filter(p => p === 1 || p === totalPages || Math.abs(p - page) <= 2)
                                .reduce<(number | string)[]>((acc, p, idx, arr) => {
                                    if (idx > 0 && p - (arr[idx - 1] as number) > 1) acc.push('...');
                                    acc.push(p);
                                    return acc;
                                }, [])
                                .map((p, idx) =>
                                    typeof p === 'string' ? (
                                        <span key={`ellipsis-${idx}`} className="px-2 text-gray-600">…</span>
                                    ) : (
                                        <button
                                            key={p}
                                            onClick={() => setPage(p)}
                                            className={`min-w-[36px] h-9 rounded-lg text-sm font-medium transition-colors ${page === p
                                                ? 'bg-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                                                : 'bg-gray-800 text-gray-400 hover:bg-gray-700 hover:text-white'
                                                }`}
                                        >
                                            {p}
                                        </button>
                                    )
                                )}
                            <button
                                disabled={page >= totalPages}
                                onClick={() => setPage(p => p + 1)}
                                className="p-2 rounded-lg bg-gray-800 text-gray-400 hover:bg-gray-700 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                            >
                                <ChevronRight className="w-5 h-5" />
                            </button>
                        </div>
                    )}
                </>
            )}
        </div>
    );
}
