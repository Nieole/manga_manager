import { useState, useEffect } from 'react';
import axios from 'axios';
import { useParams, Link, useOutletContext } from 'react-router-dom';
import { ImageIcon, Heart, ArrowUp, ArrowDown } from 'lucide-react';
import { VirtuosoGrid } from 'react-virtuoso';

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
    recent_book_id?: number;
    last_read_at?: { Time: string; Valid: boolean };
    last_read_page?: { Int64: number; Valid: boolean };
}

const PAGE_SIZE = 30;

export default function Home() {
    const { libId } = useParams();
    const { refreshTrigger } = useOutletContext<{ refreshTrigger: number }>() || { refreshTrigger: 0 };
    const [allSeries, setAllSeries] = useState<Series[]>([]);
    const [recentSeries, setRecentSeries] = useState<Series[]>([]);
    const [totalSeries, setTotalSeries] = useState(0);
    const [loading, setLoading] = useState(false);
    const [activeTag, setActiveTag] = useState<string | null>(null);
    const [activeAuthor, setActiveAuthor] = useState<string | null>(null);
    const [activeStatus, setActiveStatus] = useState<string | null>(null);
    const [activeLetter, setActiveLetter] = useState<string | null>(null);
    const [sortByField, setSortByField] = useState<string>('name');
    const [sortDir, setSortDir] = useState<string>('asc');
    const [page, setPage] = useState(1);

    const [isSelectionMode, setIsSelectionMode] = useState(false);
    const [selectedSeries, setSelectedSeries] = useState<number[]>([]);

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
            axios.get(`/api/series/recent-read?libraryId=${libId}&limit=10`)
                .then(res => {
                    setRecentSeries(res.data.items || []);
                })
                .catch(err => console.error("Failed to fetch recent series:", err));
        }
    }, [libId, refreshTrigger]);

    useEffect(() => {
        if (libId) {
            // 当筛选条件、排序或基础库变化时，重置所有状态
            setAllSeries([]);
            setPage(1);
            setTotalSeries(0);
            setLoading(true);
        }
    }, [libId, refreshTrigger, activeTag, activeAuthor, activeStatus, activeLetter, sortByField, sortDir]);

    const loadMore = () => {
        if (!libId || loading || (allSeries.length >= totalSeries && totalSeries > 0)) return;

        setLoading(true);
        const params = new URLSearchParams();
        params.append('libraryId', libId);
        params.append('limit', PAGE_SIZE.toString());
        params.append('page', page.toString());
        if (activeTag) params.append('tags', activeTag);
        if (activeAuthor) params.append('authors', activeAuthor);
        if (activeStatus) params.append('status', activeStatus);
        if (activeLetter) params.append('letter', activeLetter);
        if (sortByField && sortDir) params.append('sortBy', `${sortByField}_${sortDir}`);

        axios.get(`/api/series/search?${params.toString()}`)
            .then((res: any) => {
                const newItems = res.data.items || [];
                setAllSeries((prev: Series[]) => page === 1 ? newItems : [...prev, ...newItems]);
                setTotalSeries(res.data.total || 0);
                setPage((prev: number) => prev + 1);
                setLoading(false);
            })
            .catch((err: any) => {
                console.error("Failed to fetch series:", err);
                setLoading(false);
            });
    };

    // 初始加载及参数变化后的首次加载
    useEffect(() => {
        if (libId && page === 1) {
            loadMore();
        }
    }, [libId, page]);

    // 移除旧的分页逻辑
    // const totalPages = Math.max(1, Math.ceil(totalSeries / PAGE_SIZE));

    // 当筛选条件变化时重置到第一页
    useEffect(() => { setPage(1); setIsSelectionMode(false); setSelectedSeries([]); }, [activeTag, activeAuthor, activeStatus, activeLetter, sortByField, sortDir]);

    const handleBulkFavoriteUpdate = async (isFav: boolean) => {
        try {
            await axios.post('/api/series/bulk-update', {
                series_ids: selectedSeries,
                is_favorite: isFav
            });
            setIsSelectionMode(false);
            setSelectedSeries([]);
            // 由于使用了 useOutletContext，无法直接修改其 state。因此我们可以借助触发重新 fetch 当前列表。
            const params = new URLSearchParams();
            params.append('libraryId', libId!);
            params.append('limit', PAGE_SIZE.toString());
            params.append('page', page.toString());
            if (activeTag) params.append('tags', activeTag);
            if (activeAuthor) params.append('authors', activeAuthor);
            if (activeStatus) params.append('status', activeStatus);
            if (activeLetter) params.append('letter', activeLetter);
            if (sortByField && sortDir) params.append('sortBy', `${sortByField}_${sortDir}`);
            const res = await axios.get(`/api/series/search?${params.toString()}`);
            setAllSeries(res.data.items || []);
        } catch (e) {
            console.error("Bulk update failed", e);
            alert("批量更新失败");
        }
    };

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
                    <h2 className="text-2xl sm:text-3xl font-bold text-white tracking-tight mb-1">浏览系列</h2>
                    <p className="text-gray-400 text-xs sm:text-sm">
                        资源库返回 {totalSeries} 个结果
                    </p>
                </div>
                <div className="flex flex-wrap items-center gap-2 sm:gap-3 mt-4 sm:mt-0 w-full sm:w-auto justify-between sm:justify-end">
                    {allSeries.length > 0 && (
                        <button
                            onClick={() => {
                                setIsSelectionMode(!isSelectionMode);
                                setSelectedSeries([]);
                            }}
                            className={`px-3 py-1.5 text-xs sm:text-sm font-medium rounded-lg transition-colors border focus:outline-none flex-shrink-0 ${isSelectionMode ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-md' : 'bg-transparent border-gray-700 text-gray-400 hover:border-gray-500 hover:text-white'}`}
                        >
                            {isSelectionMode ? '取消选择' : '批量操作'}
                        </button>
                    )}
                    <span className="text-xs sm:text-sm text-gray-400 font-medium ml-auto sm:ml-0">排序方式</span>
                    <select
                        value={sortByField}
                        onChange={(e) => setSortByField(e.target.value)}
                        className="bg-gray-900 border border-gray-700 text-white text-sm rounded-lg focus:ring-komgaPrimary focus:border-komgaPrimary block p-2 outline-none transition-colors cursor-pointer hover:border-gray-500"
                    >
                        <option value="name">名称</option>
                        <option value="created">入库时间</option>
                        <option value="updated">最新更新</option>
                        <option value="rating">评分</option>
                        <option value="books">册数量</option>
                        <option value="favorite">收藏状态</option>
                    </select>
                    <button
                        onClick={() => setSortDir(prev => prev === 'asc' ? 'desc' : 'asc')}
                        className="p-2 bg-gray-900 border border-gray-700 hover:border-gray-500 rounded-lg text-gray-400 hover:text-white transition-colors flex items-center justify-center shadow-sm hover:shadow"
                        title={sortDir === 'asc' ? '当前正序 (点击切换倒序)' : '当前倒序 (点击切换正序)'}
                    >
                        {sortDir === 'asc' ? <ArrowUp className="w-5 h-5 text-komgaPrimary" /> : <ArrowDown className="w-5 h-5 text-komgaPrimary" />}
                    </button>
                </div>
            </div>

            {/* 继续阅读 */}
            {recentSeries.length > 0 && (
                <div className="mb-10">
                    <h3 className="text-xl font-bold text-white mb-4 pl-1 border-l-4 border-komgaPrimary">继续阅读</h3>
                    <div className="flex gap-3 sm:gap-4 overflow-x-auto pb-4 custom-scrollbar snap-x">
                        {recentSeries.map(s => (
                            <Link
                                key={s.id}
                                to={s.recent_book_id ? `/reader/${s.recent_book_id}` : `/series/${s.id}`}
                                className="group shrink-0 w-32 sm:w-44 md:w-52 flex flex-col rounded-xl overflow-hidden bg-komgaSurface border border-gray-800 hover:border-komgaPrimary transition-all duration-300 hover:shadow-lg snap-start"
                            >
                                <div className="aspect-[2/3] w-full bg-gray-900 flex items-center justify-center relative overflow-hidden">
                                    {s.cover_path?.Valid && s.cover_path?.String ? (
                                        <img
                                            src={`/api/thumbnails/${s.cover_path.String}`}
                                            alt={s.name}
                                            className="w-full h-full object-cover transition-transform duration-500 group-hover:scale-105"
                                            loading="lazy"
                                        />
                                    ) : (
                                        <ImageIcon className="w-10 h-10 text-gray-700" />
                                    )}
                                    <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-komgaBackground to-transparent h-2/3 opacity-80" />
                                    <div className="absolute inset-0 ring-1 ring-inset ring-white/10 rounded-t-xl" />
                                    <div className="absolute bottom-3 right-3 bg-komgaPrimary/90 text-white text-[10px] font-bold px-2 py-1 rounded backdrop-blur">
                                        接着读
                                    </div>
                                    {s.last_read_page?.Valid && s.last_read_page?.Int64 > 1 && (
                                        <div className="absolute top-2 right-2 bg-black/70 px-2 py-0.5 rounded text-[10px] text-gray-300 backdrop-blur">
                                            P.{s.last_read_page.Int64}
                                        </div>
                                    )}
                                </div>
                                <div className="p-3">
                                    <h3 className="text-sm font-semibold text-gray-100 truncate group-hover:text-komgaPrimary transition-colors" title={s.title?.Valid ? s.title.String : s.name}>
                                        {s.title?.Valid ? s.title.String : s.name}
                                    </h3>
                                </div>
                            </Link>
                        ))}
                    </div>
                </div>
            )}

            {/* 标签与作者与状态 聚合导航栏 */}
            <div className="mb-6 grid xl:grid-cols-3 gap-6 xl:gap-8 divide-y xl:divide-y-0 xl:divide-x divide-gray-800">
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
                    <div className="min-h-[600px] w-full relative">
                        <VirtuosoGrid
                            useWindowScroll
                            data={allSeries}
                            totalCount={totalSeries}
                            endReached={loadMore}
                            overscan={400}
                            listClassName="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4 sm:gap-6"
                            itemClassName="p-1"
                            itemContent={(_, s: Series) => {
                                const isSelected = selectedSeries.includes(s.id);

                                const handleCardClick = (e: React.MouseEvent) => {
                                    if (isSelectionMode) {
                                        e.preventDefault();
                                        setSelectedSeries((prev: number[]) => prev.includes(s.id) ? prev.filter((id: number) => id !== s.id) : [...prev, s.id]);
                                    }
                                };

                                return (
                                    <Link
                                        key={s.id}
                                        to={`/series/${s.id}`}
                                        onClick={handleCardClick}
                                        className={`group relative flex flex-col h-full rounded-xl overflow-hidden bg-komgaSurface border ${isSelected ? 'border-komgaPrimary ring-2 ring-komgaPrimary shadow-lg shadow-komgaPrimary/20' : 'border-gray-800 hover:border-komgaPrimary/50 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10'} transition-all duration-300 cursor-pointer`}
                                    >
                                        <div className="aspect-[2/3] w-full bg-gray-900 flex items-center justify-center relative overflow-hidden">
                                            {isSelectionMode && (
                                                <div className="absolute top-2 left-2 z-30">
                                                    <div className={`w-5 h-5 rounded-full border-2 flex items-center justify-center transition-colors ${isSelected ? 'bg-komgaPrimary border-komgaPrimary' : 'bg-black/50 border-gray-400'}`}>
                                                        {isSelected && <span className="text-white text-xs font-bold leading-none select-none">✓</span>}
                                                    </div>
                                                </div>
                                            )}
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
                                );
                            }}
                            components={{
                                Footer: () => (
                                    loading ? (
                                        <div className="w-full py-10 flex flex-col items-center justify-center text-gray-500">
                                            <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-komgaPrimary mb-2"></div>
                                            <span className="text-xs">加载更多系列中...</span>
                                        </div>
                                    ) : allSeries.length >= totalSeries && totalSeries > 0 ? (
                                        <div className="w-full py-10 text-center text-gray-600 text-xs italic">
                                            — 到底啦，共 {totalSeries} 个系列 —
                                        </div>
                                    ) : null
                                )
                            }}
                        />
                    </div>


                    {/* 悬浮多选操作栏 */}
                    {isSelectionMode && selectedSeries.length > 0 && (
                        <div className="fixed bottom-8 left-1/2 -translate-x-1/2 bg-gray-900 border border-gray-700 shadow-[0_20px_50px_-12px_rgba(0,0,0,0.8)] rounded-2xl px-6 py-4 flex items-center gap-6 z-50 animate-in slide-in-from-bottom-5">
                            <span className="text-white font-medium text-sm">已选择 {selectedSeries.length} 项</span>
                            <div className="flex items-center gap-3">
                                <button
                                    onClick={() => handleBulkFavoriteUpdate(true)}
                                    className="bg-red-500/10 hover:bg-red-500/20 text-red-500 border border-red-500/30 px-4 py-2 rounded-lg text-sm font-medium transition-colors flex items-center gap-2"
                                >
                                    <Heart className="w-4 h-4 fill-current" /> 标记收藏
                                </button>
                                <button
                                    onClick={() => handleBulkFavoriteUpdate(false)}
                                    className="bg-gray-800 hover:bg-gray-700 text-gray-300 border border-gray-700 px-4 py-2 rounded-lg text-sm font-medium transition-colors"
                                >
                                    移除收藏
                                </button>
                            </div>
                        </div>
                    )}
                </>
            )}
        </div>
    );
}
