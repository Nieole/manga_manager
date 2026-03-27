import { useState, useEffect } from 'react';
import axios from 'axios';
import { useParams, Link, useOutletContext } from 'react-router-dom';
import { ImageIcon, Heart, FolderHeart, RefreshCw } from 'lucide-react';
import AddToCollectionModal from '../components/AddToCollectionModal';
import { AIRecommendationsSection } from './home/AIRecommendationsSection';
import { HomeFilters } from './home/HomeFilters';
import { HomeToolbar } from './home/HomeToolbar';
import { RecentSeriesStrip } from './home/RecentSeriesStrip';
import type { AIRecommendation, NamedOption, Series } from './home/types';

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
    const [settingsReady, setSettingsReady] = useState(false);

    const [isSelectionMode, setIsSelectionMode] = useState(false);
    const [selectedSeries, setSelectedSeries] = useState<number[]>([]);
    const [showCollectionModal, setShowCollectionModal] = useState(false);
    const [toastMsg, setToastMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);
    const [rescanningId, setRescanningId] = useState<number | null>(null);

    const showToast = (text: string, type: 'success' | 'error') => {
        setToastMsg({ text, type });
        setTimeout(() => setToastMsg(null), 3000);
    };

    const [allTags, setAllTags] = useState<NamedOption[]>([]);
    const [allAuthors, setAllAuthors] = useState<NamedOption[]>([]);
    const allStatuses = ['已完结', '连载中', '已放弃', '有生之年'];

    const [aiRecommendations, setAiRecommendations] = useState<AIRecommendation[]>([]);
    const [loadingAI, setLoadingAI] = useState(false);
    const [hasFetchedAI, setHasFetchedAI] = useState(false);

    const fetchAIRecommendations = () => {
        if (!libId) return;
        setLoadingAI(true);
        axios.get('/api/recommendations?limit=3')
            .then(res => {
                setAiRecommendations(res.data || []);
                setHasFetchedAI(true);
            })
            .catch(err => {
                console.error("Failed to fetch AI recommendations", err);
                showToast("获取 AI 推荐失败", "error");
            })
            .finally(() => setLoadingAI(false));
    };

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

    const fetchSeriesPage = (pageNumber: number, silent = false) => {
        if (!libId || (loading && !silent)) return;

        if (!silent) setLoading(true);
        const params = new URLSearchParams();
        params.append('libraryId', libId);
        params.append('limit', PAGE_SIZE.toString());
        params.append('page', pageNumber.toString());
        if (activeTag) params.append('tags', activeTag);
        if (activeAuthor) params.append('authors', activeAuthor);
        if (activeStatus) params.append('status', activeStatus);
        if (activeLetter) params.append('letter', activeLetter);
        if (sortByField && sortDir) params.append('sortBy', `${sortByField}_${sortDir}`);

        axios.get(`/api/series/search?${params.toString()}`)
            .then((res: any) => {
                const newItems = res.data.items || [];
                setAllSeries(newItems);
                setTotalSeries(res.data.total || 0);
                if (!silent) {
                    setLoading(false);
                    window.scrollTo({ top: 0, behavior: 'smooth' });
                }
            })
            .catch((err: any) => {
                console.error("Failed to fetch series:", err);
                setLoading(false);
            });
    };

    // 1. 恢复配置 (仅在 libId 变化时执行一次)
    useEffect(() => {
        if (!libId) return;
        const saved = localStorage.getItem(`lib_settings_${libId}`);
        if (saved) {
            try {
                const config = JSON.parse(saved);
                setActiveTag(config.activeTag);
                setActiveAuthor(config.activeAuthor);
                setActiveStatus(config.activeStatus);
                setActiveLetter(config.activeLetter);
                setSortByField(config.sortByField || 'name');
                setSortDir(config.sortDir || 'asc');
                setPage(config.page || 1);
            } catch (e) { }
        } else {
            setActiveTag(null);
            setActiveAuthor(null);
            setActiveStatus(null);
            setActiveLetter(null);
            setSortByField('name');
            setSortDir('asc');
            setPage(1);
        }
        setSettingsReady(true);
        return () => setSettingsReady(false);
    }, [libId]);

    // 2. 状态变化处理：筛选/排序/分页变化时，延迟 300ms 再拉取（防抖），避免快速切换筛选条件时的请求洪峰
    useEffect(() => {
        if (!libId || !settingsReady) return;

        // 保存配置
        const config = { activeTag, activeAuthor, activeStatus, activeLetter, sortByField, sortDir, page };
        localStorage.setItem(`lib_settings_${libId}`, JSON.stringify(config));

        // 防抖：300ms 后执行拉取
        const timer = setTimeout(() => {
            fetchSeriesPage(page);
        }, 300);

        // 筛选变化时自动退出选择模式
        setIsSelectionMode(false);
        setSelectedSeries([]);

        return () => clearTimeout(timer);
    }, [libId, settingsReady, page, activeTag, activeAuthor, activeStatus, activeLetter, sortByField, sortDir]);

    // 3. SSE 专用静默刷新
    useEffect(() => {
        if (libId) {
            fetchSeriesPage(page, true);
        }
    }, [refreshTrigger]);

    const handleBulkFavoriteUpdate = async (isFav: boolean) => {
        try {
            await axios.post('/api/series/bulk-update', {
                series_ids: selectedSeries,
                is_favorite: isFav
            });
            setIsSelectionMode(false);
            setSelectedSeries([]);
            // 由于使用了 useOutletContext，无法直接修改其 state。因此我们可以借助触发重新 fetch 当前列表。
            fetchSeriesPage(page, true);
        } catch (e) {
            console.error("Bulk update failed", e);
            alert("批量更新失败");
        }
    };

    const handleToggleFavorite = async (e: React.MouseEvent, seriesId: number, currentFav: boolean) => {
        e.preventDefault();
        e.stopPropagation();
        try {
            await axios.post('/api/series/bulk-update', {
                series_ids: [seriesId],
                is_favorite: !currentFav
            });
            // 静默刷新列表
            fetchSeriesPage(page, true);
        } catch (e) {
            console.error("Toggle favorite failed", e);
        }
    };

    const handleRescanSeries = async (e: React.MouseEvent, seriesId: number) => {
        e.preventDefault();
        e.stopPropagation();
        setRescanningId(seriesId);
        try {
            await axios.post(`/api/series/${seriesId}/rescan?force=true`);
            showToast('已下发重新扫描指令', 'success');
            setTimeout(() => fetchSeriesPage(page, true), 3000);
        } catch (err: any) {
            showToast('重新扫描失败: ' + (err.response?.data?.error || err.message), 'error');
        } finally {
            setRescanningId(null);
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
            <HomeToolbar
                totalSeries={totalSeries}
                hasSeries={allSeries.length > 0}
                isSelectionMode={isSelectionMode}
                sortByField={sortByField}
                sortDir={sortDir}
                onToggleSelectionMode={() => {
                    setIsSelectionMode(!isSelectionMode);
                    setSelectedSeries([]);
                }}
                onSortFieldChange={(value) => {
                    setSortByField(value);
                    setPage(1);
                }}
                onToggleSortDir={() => {
                    setSortDir(prev => prev === 'asc' ? 'desc' : 'asc');
                    setPage(1);
                }}
            />

            <RecentSeriesStrip recentSeries={recentSeries} />

            <AIRecommendationsSection
                aiRecommendations={aiRecommendations}
                loadingAI={loadingAI}
                hasFetchedAI={hasFetchedAI}
                onRefresh={fetchAIRecommendations}
            />

            <HomeFilters
                allStatuses={allStatuses}
                allTags={allTags}
                allAuthors={allAuthors}
                activeStatus={activeStatus}
                activeTag={activeTag}
                activeAuthor={activeAuthor}
                activeLetter={activeLetter}
                onStatusChange={(value) => {
                    setActiveStatus(value);
                    setPage(1);
                }}
                onTagChange={(value) => {
                    setActiveTag(value);
                    setPage(1);
                }}
                onAuthorChange={(value) => {
                    setActiveAuthor(value);
                    setPage(1);
                }}
                onLetterChange={(value) => {
                    setActiveLetter(value);
                    setPage(1);
                }}
            />

            {loading && allSeries.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-40">
                    <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-komgaPrimary mb-4"></div>
                    <div className="text-gray-400 font-medium">正在拉取资源...</div>
                </div>
            ) : allSeries.length === 0 ? (
                <div className="text-center py-20 text-gray-500">无匹配的系列</div>
            ) : (
                <div className={`relative transition-opacity duration-300 ${loading ? 'opacity-40 pointer-events-none' : 'opacity-100'}`}>
                    <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-4 sm:gap-6 min-h-[600px] items-start">
                        {allSeries.map((s) => {
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
                                    className={`group relative rounded-xl overflow-hidden bg-komgaSurface border ${isSelected ? 'border-komgaPrimary ring-2 ring-komgaPrimary shadow-lg shadow-komgaPrimary/20' : 'border-gray-800 hover:border-komgaPrimary/50 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10'} transition-all duration-300 cursor-pointer block h-fit`}
                                >
                                    <div className="aspect-[1/1.4] w-full bg-gray-900 flex items-center justify-center relative overflow-hidden">
                                        {isSelectionMode && (
                                            <div className="absolute top-2 left-2 z-30">
                                                <div className={`w-5 h-5 rounded-full border-2 flex items-center justify-center transition-colors ${isSelected ? 'bg-komgaPrimary border-komgaPrimary' : 'bg-black/50 border-gray-400'}`}>
                                                    {isSelected && <span className="text-white text-xs font-bold leading-none select-none">✓</span>}
                                                </div>
                                            </div>
                                        )}
                                        {s.cover_path?.Valid && s.cover_path?.String ? (
                                            <img src={`/api/thumbnails/${s.cover_path.String}${s.updated_at ? `?v=${new Date(s.updated_at).getTime()}` : ''}`} alt="cover" loading="lazy" className="absolute inset-0 w-full h-full object-cover transition-transform duration-500 group-hover:scale-105" />
                                        ) : (
                                            <ImageIcon className="h-12 w-12 text-gray-700 opacity-50 transition-opacity group-hover:opacity-100 relative z-10" />
                                        )}
                                        <div className="absolute inset-x-0 top-0 p-3 z-20 flex justify-between items-start">
                                            {s.rating?.Valid && s.rating.Float64 > 0 && (
                                                <span className="flex items-center text-xs font-bold text-yellow-400 bg-black/70 px-1.5 py-0.5 rounded backdrop-blur border border-yellow-400/20 shadow-md pointer-events-none">
                                                    ★ {s.rating.Float64.toFixed(1)}
                                                </span>
                                            )}
                                            {!isSelectionMode && (
                                                <div className="flex gap-1.5">
                                                    <button
                                                        onClick={(e) => handleRescanSeries(e, s.id)}
                                                        disabled={rescanningId === s.id}
                                                        className={`p-1.5 rounded-full backdrop-blur border shadow-md transition-all bg-black/60 border-white/10 text-white/40 hover:text-blue-400 hover:bg-blue-400/20 hover:border-blue-400/40 opacity-0 group-hover:opacity-100 disabled:opacity-100 disabled:cursor-not-allowed`}
                                                        title="重新扫描该系列"
                                                    >
                                                        <RefreshCw className={`w-3.5 h-3.5 ${rescanningId === s.id ? 'animate-spin text-blue-400' : ''}`} />
                                                    </button>
                                                    <button
                                                        onClick={(e) => handleToggleFavorite(e, s.id, s.is_favorite)}
                                                        className={`p-1.5 rounded-full backdrop-blur border shadow-md transition-all ${s.is_favorite
                                                            ? 'bg-red-500/20 border-red-500/40 text-red-500'
                                                            : 'bg-black/60 border-white/10 text-white/40 hover:text-red-400 hover:bg-red-400/20 hover:border-red-400/40 opacity-0 group-hover:opacity-100'
                                                            }`}
                                                    >
                                                        <Heart className={`w-3.5 h-3.5 ${s.is_favorite ? 'fill-current' : ''}`} />
                                                    </button>
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
                                    <div className="p-3">
                                        <div>
                                            <h4 className="text-sm font-bold text-gray-200 line-clamp-1 leading-tight group-hover:text-komgaPrimary transition-colors mb-1.5">
                                                {s.title?.Valid ? s.title.String : s.name}
                                            </h4>
                                            {s.summary?.Valid && (
                                                <p className="text-[11px] text-gray-500 line-clamp-2 leading-tight opacity-70">
                                                    {s.summary.String}
                                                </p>
                                            )}
                                        </div>
                                        {/* 移除底部的“系列”字样，保持清爽 */}
                                    </div>
                                </Link>
                            );
                        })}
                    </div>

                    {/* 分页控制栏 */}
                    <div className="mt-12 mb-8 flex flex-col sm:flex-row items-center justify-between gap-6 border-t border-gray-800 pt-8">
                        <div className="text-gray-500 text-sm">
                            共 <span className="text-gray-300 font-bold">{totalSeries}</span> 个系列，当前第 <span className="text-komgaPrimary font-bold">{page}</span> / {Math.ceil(totalSeries / PAGE_SIZE)} 页
                        </div>
                        <div className="flex items-center gap-2">
                            <button
                                onClick={() => setPage(1)}
                                disabled={page === 1}
                                className="px-3 py-2 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
                            >
                                首页
                            </button>
                            <button
                                onClick={() => setPage(p => Math.max(1, p - 1))}
                                disabled={page === 1}
                                className="px-4 py-2 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
                            >
                                上一页
                            </button>

                            <div className="flex items-center gap-1 mx-2">
                                {[...Array(Math.min(5, Math.ceil(totalSeries / PAGE_SIZE)))].map((_, i) => {
                                    const totalPages = Math.ceil(totalSeries / PAGE_SIZE);
                                    let pNum = page;

                                    if (page <= 3) {
                                        pNum = i + 1;
                                    } else if (page >= totalPages - 2) {
                                        pNum = totalPages - 4 + i;
                                    } else {
                                        pNum = page - 2 + i;
                                    }

                                    if (pNum <= 0 || pNum > totalPages) return null;

                                    return (
                                        <button
                                            key={pNum}
                                            onClick={() => setPage(pNum)}
                                            className={`w-10 h-10 flex items-center justify-center rounded-lg text-sm font-bold transition-all ${page === pNum ? 'bg-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20' : 'bg-gray-900 text-gray-400 border border-gray-800 hover:border-gray-600 hover:text-white'}`}
                                        >
                                            {pNum}
                                        </button>
                                    );
                                })}
                            </div>

                            <button
                                onClick={() => setPage(p => Math.min(Math.ceil(totalSeries / PAGE_SIZE), p + 1))}
                                disabled={page >= Math.ceil(totalSeries / PAGE_SIZE)}
                                className="px-4 py-2 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
                            >
                                下一页
                            </button>
                            <button
                                onClick={() => setPage(Math.ceil(totalSeries / PAGE_SIZE))}
                                disabled={page >= Math.ceil(totalSeries / PAGE_SIZE)}
                                className="px-3 py-2 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
                            >
                                末页
                            </button>
                        </div>
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
                                <button
                                    onClick={() => setShowCollectionModal(true)}
                                    className="bg-komgaPrimary/10 hover:bg-komgaPrimary/20 text-komgaPrimary border border-komgaPrimary/30 px-4 py-2 rounded-lg text-sm font-medium transition-colors flex items-center gap-2"
                                >
                                    <FolderHeart className="w-4 h-4" /> 加入合集
                                </button>
                            </div>
                        </div>
                    )}
                </div>
            )}

            {/* 添加到合集弹窗 */}
            {showCollectionModal && selectedSeries.length > 0 && (
                <AddToCollectionModal
                    seriesIds={selectedSeries}
                    onClose={() => setShowCollectionModal(false)}
                    onSuccess={() => {
                        showToast(`成功将 ${selectedSeries.length} 个系列加入合集`, 'success');
                        setSelectedSeries([]);
                        setIsSelectionMode(false);
                    }}
                />
            )}

            {/* Toast 通知 */}
            {toastMsg && (
                <div className="fixed bottom-6 right-6 z-50 animate-in slide-in-from-bottom-5 fade-in duration-300">
                    <div className={`px-4 py-3 rounded-lg shadow-lg flex items-center gap-3 border ${toastMsg.type === 'success' ? 'bg-green-900 border-green-700 text-green-100' : 'bg-red-900 border-red-700 text-red-100'
                        }`}>
                        <span className="text-sm font-medium">{toastMsg.text}</span>
                        <button onClick={() => setToastMsg(null)} className="ml-2 text-white/50 hover:text-white">✕</button>
                    </div>
                </div>
            )}
        </div>
    );
}
