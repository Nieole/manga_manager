import { useState, useEffect, useMemo } from 'react';
import axios from 'axios';
import { useParams, Link, useOutletContext } from 'react-router-dom';
import { ImageIcon, ChevronLeft, ChevronRight, Search } from 'lucide-react';

interface NullString {
    String: string;
    Valid: boolean;
}

interface Series {
    id: string;
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
}

const PAGE_SIZE = 30;
const LETTERS = '#ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('');

function getFirstChar(s: Series): string {
    const name = s.title?.Valid ? s.title.String : s.name;
    const ch = name.charAt(0).toUpperCase();
    return /[A-Z]/.test(ch) ? ch : '#';
}

export default function Home() {
    const { libId } = useParams();
    const { refreshTrigger } = useOutletContext<{ refreshTrigger: number }>() || { refreshTrigger: 0 };
    const [allSeries, setAllSeries] = useState<Series[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchQuery, setSearchQuery] = useState('');
    const [activeLetter, setActiveLetter] = useState<string | null>(null);
    const [activeTag, setActiveTag] = useState<string | null>(null);
    const [page, setPage] = useState(1);

    useEffect(() => {
        if (libId) {
            setLoading(true);
            setSearchQuery('');
            setActiveLetter(null);
            setActiveTag(null);
            setPage(1);
            axios.get(`/api/series/${libId}`)
                .then(res => {
                    setAllSeries(res.data || []);
                    setLoading(false);
                })
                .catch(err => {
                    console.error("Failed to fetch series:", err);
                    setLoading(false);
                });
        }
    }, [libId, refreshTrigger]);

    // 筛选逻辑
    const filtered = useMemo(() => {
        let result = allSeries;

        if (activeLetter) {
            result = result.filter(s => getFirstChar(s) === activeLetter);
        }

        if (activeTag) {
            result = result.filter(s => {
                const tags = s.tags_string ? s.tags_string.split(',') : [];
                return tags.includes(activeTag);
            });
        }

        if (searchQuery.trim()) {
            const q = searchQuery.toLowerCase();
            result = result.filter(s => {
                const name = (s.title?.Valid ? s.title.String : s.name).toLowerCase();
                return name.includes(q);
            });
        }

        return result;
    }, [allSeries, activeLetter, activeTag, searchQuery]);

    // 分页逻辑
    const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
    const paginated = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

    // 当筛选条件变化时重置到第一页
    useEffect(() => { setPage(1); }, [activeLetter, activeTag, searchQuery]);

    // 统计目前资源下的全部去重标签和分布频次
    const allUniqueTags = useMemo(() => {
        const counts = new Map<string, number>();
        allSeries.forEach(s => {
            if (s.tags_string) {
                const parts = s.tags_string.split(',');
                parts.forEach(t => {
                    counts.set(t, (counts.get(t) || 0) + 1);
                });
            }
        });
        const entries = Array.from(counts.entries());
        // 按照名字排序或按照出现次数排序
        entries.sort((a, b) => b[1] - a[1]);
        return entries;
    }, [allSeries]);

    // 统计每个字母有多少系列
    const letterCounts = useMemo(() => {
        const counts: Record<string, number> = {};
        allSeries.forEach(s => {
            const ch = getFirstChar(s);
            counts[ch] = (counts[ch] || 0) + 1;
        });
        return counts;
    }, [allSeries]);

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
                        共 {allSeries.length} 个系列{filtered.length !== allSeries.length ? `，筛选后 ${filtered.length} 项` : ''}
                    </p>
                </div>
                <div className="relative w-full sm:w-72">
                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
                    <input
                        type="text"
                        value={searchQuery}
                        onChange={e => setSearchQuery(e.target.value)}
                        placeholder="快捷搜索系列名称..."
                        className="w-full bg-gray-900 border border-gray-800 rounded-lg pl-9 pr-4 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 focus:border-transparent transition-all placeholder-gray-500"
                    />
                </div>
            </div>

            {/* 首字母导航栏 */}
            <div className="mb-6 flex flex-wrap gap-1">
                <button
                    onClick={() => setActiveLetter(null)}
                    className={`px-2.5 py-1 text-xs font-medium rounded transition-colors ${activeLetter === null
                        ? 'bg-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                        : 'bg-gray-800 text-gray-400 hover:bg-gray-700 hover:text-white'
                        }`}
                >
                    全部
                </button>
                {LETTERS.map(letter => {
                    const count = letterCounts[letter] || 0;
                    return (
                        <button
                            key={letter}
                            disabled={count === 0}
                            onClick={() => setActiveLetter(letter === activeLetter ? null : letter)}
                            className={`px-2 py-1 text-xs font-medium rounded transition-colors ${activeLetter === letter
                                ? 'bg-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                                : count > 0
                                    ? 'bg-gray-800 text-gray-400 hover:bg-gray-700 hover:text-white'
                                    : 'bg-gray-900 text-gray-700 cursor-not-allowed'
                                }`}
                            title={count > 0 ? `${count} 项` : '无匹配'}
                        >
                            {letter}
                        </button>
                    );
                })}
            </div>

            {/* 标签聚合导航栏 (若有) */}
            {allUniqueTags.length > 0 && (
                <div className="mb-6 pb-2 grid grid-cols-[auto_1fr] items-start gap-4">
                    <span className="text-gray-500 text-sm mt-1 whitespace-nowrap">分类标签:</span>
                    <div className="flex flex-wrap gap-2 max-h-24 overflow-y-auto">
                        <button
                            onClick={() => setActiveTag(null)}
                            className={`px-3 py-1 text-xs font-medium rounded-full transition-colors border ${activeTag === null
                                ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                                : 'bg-transparent border-gray-700 text-gray-400 hover:border-gray-500 hover:text-white'
                                }`}
                        >
                            全部题材
                        </button>
                        {allUniqueTags.map(([tname, tcount]) => (
                            <button
                                key={tname}
                                onClick={() => setActiveTag(tname === activeTag ? null : tname)}
                                className={`px-3 py-1 text-xs font-medium rounded-full transition-all border flex items-center gap-1.5 ${activeTag === tname
                                    ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20'
                                    : 'bg-transparent border-gray-800 text-gray-400 hover:border-gray-600 hover:text-gray-200 bg-gray-900/40'
                                    }`}
                            >
                                {tname}
                                <span className={activeTag === tname ? 'text-white/70' : 'text-gray-600'}>{tcount}</span>
                            </button>
                        ))}
                    </div>
                </div>
            )}

            {loading ? (
                <div className="text-center py-20 text-gray-400 animate-pulse">正在加载目录与元数据...</div>
            ) : filtered.length === 0 ? (
                <div className="text-center py-20 text-gray-500">无匹配的系列</div>
            ) : (
                <>
                    <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-6">
                        {paginated.map(s => (
                            <Link
                                key={s.id}
                                to={`/series/${s.id}`}
                                className="group relative flex flex-col rounded-xl overflow-hidden bg-komgaSurface border border-gray-800 hover:border-komgaPrimary/50 transition-all duration-300 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10 cursor-pointer"
                            >
                                <div className="aspect-[2/3] w-full bg-gray-900 flex items-center justify-center relative overflow-hidden">
                                    {s.cover_path?.Valid ? (
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
                                    </div>
                                    <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/95 via-black/60 to-transparent p-3 pt-8 z-10 pointer-events-none">
                                        <div className="flex justify-between text-[11px] font-medium text-gray-300">
                                            <span>
                                                {s.volume_count > 0 ? `${s.volume_count}卷 · ` : ''}{s.actual_book_count}话
                                            </span>
                                            <span>{s.total_pages?.Valid ? s.total_pages.Float64 : 0} P</span>
                                        </div>
                                        {/* 阅读进度条 */}
                                        {s.actual_book_count > 0 && (
                                            <div className="w-full h-1 bg-gray-700/60 rounded-full mt-1.5 overflow-hidden">
                                                <div
                                                    className={`h-full ${s.read_count === s.actual_book_count ? 'bg-green-500' : 'bg-komgaPrimary'}`}
                                                    style={{ width: `${(s.read_count / s.actual_book_count) * 100}%` }}
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
